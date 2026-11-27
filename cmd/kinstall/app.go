package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kinstall/kinstall/internal/apiproxy"
	"github.com/kinstall/kinstall/internal/app"
	"github.com/kinstall/kinstall/internal/artifact/catalog"
	"github.com/kinstall/kinstall/internal/artifact/source"
	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/cli"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/engine"
	"github.com/kinstall/kinstall/internal/events"
	"github.com/kinstall/kinstall/internal/executor/sshx"
	"github.com/kinstall/kinstall/internal/i18n"
	"github.com/kinstall/kinstall/internal/inventory"
	"github.com/kinstall/kinstall/internal/lock"
	"github.com/kinstall/kinstall/internal/multicluster"
	"github.com/kinstall/kinstall/internal/notify"
	"github.com/kinstall/kinstall/internal/obs"
	"github.com/kinstall/kinstall/internal/osadapter"
	"github.com/kinstall/kinstall/internal/pki"
	"github.com/kinstall/kinstall/internal/ports"
	"github.com/kinstall/kinstall/internal/registry"
	"github.com/kinstall/kinstall/internal/secrets"
	"github.com/kinstall/kinstall/internal/server"
	"github.com/kinstall/kinstall/internal/store"
	"github.com/kinstall/kinstall/internal/template"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Application is the sole composition root wiring all adapters, plugins, workers, and HTTP/SSE server (G1 §7.3).
type Application struct {
	Store   *store.DBStore
	SSHPool *sshx.Pool
	Events  *events.Hub
	Worker  *engine.Worker
	AuthSvc *authz.Service
	Facade  *app.Facade
	I18n    *i18n.Catalog
	Metrics *obs.Metrics
	Logger  *slog.Logger
}

func BuildApplication(ctx context.Context) (*Application, error) {
	logger := obs.NewLogger(os.Getenv("KINSTALL_LOG_FORMAT"), os.Getenv("KINSTALL_LOG_LEVEL"))

	dataDir := strings.TrimSpace(os.Getenv("KINSTALL_DATA_DIR"))
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		if home == "" {
			home = "."
		}
		dataDir = filepath.Join(home, ".kinstall")
	}
	dataDir = filepath.Clean(dataDir)
	// #nosec G703 -- operator-configured KINSTALL_DATA_DIR cleaned via filepath.Clean
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	driver := strings.TrimSpace(os.Getenv("KINSTALL_DB_DRIVER"))
	if driver == "" {
		driver = "sqlite"
	}
	dsn := strings.TrimSpace(os.Getenv("KINSTALL_DB_DSN"))
	if dsn == "" && driver == "sqlite" {
		dsn = filepath.Join(dataDir, "kinstall.db")
	}

	st, err := store.OpenAndMigrate(ctx, store.Config{
		Driver:    driver,
		DSN:       dsn,
		BackupDir: filepath.Join(dataDir, "backups"),
	}, logger)
	if err != nil {
		return nil, err
	}

	// KEK & Envelope encryption (AES-256-GCM default; SM4-GCM when KINSTALL_GM_ENABLED=true)
	// Auto-select KEK provider: OpenBao Transit -> Env KINSTALL_MASTER_KEY -> Local 0600 keyring file (D1 §3.2)
	enableGM := strings.EqualFold(os.Getenv("KINSTALL_GM_ENABLED"), "true") || os.Getenv("KINSTALL_GM_ENABLED") == "1"
	alg := secrets.AlgAES256GCM
	if enableGM {
		alg = secrets.AlgSM4GCM
	}
	kek, kekKind, err := initKEKProvider(ctx, dataDir, logger)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	secStore := secrets.NewEnvelopeStore(kek, kekKind, alg)

	osEng, err := osadapter.LoadWithOverride(filepath.Join(dataDir, "spec-override"))
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	cat, err := i18n.New()
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	reg := registry.New()
	if err := RegisterBuiltinPlugins(reg); err != nil {
		_ = st.Close()
		return nil, err
	}

	sshPool := sshx.NewPool(sshx.PoolConfig{
		MaxSessionsPerHost: 8,
		MaxDialsPerBastion: 6,
		KeepAliveInterval:  30 * time.Second,
	})
	execChain := sshx.BuildExecutor(sshPool, logger)

	directPGDSN := ""
	if driver == "postgres" || driver == "postgresql" {
		directPGDSN = strings.TrimSpace(os.Getenv("KINSTALL_PG_DIRECT_DSN"))
		if directPGDSN == "" {
			directPGDSN = dsn
		}
	}
	evHub := events.NewHub(st.OperationEvents(), directPGDSN, logger)
	audRec := audit.NewRecorder(st.Audit(), enableGM)
	authSvc := authz.NewService(st.Auth(), audRec)
	if issuer := strings.TrimSpace(os.Getenv("KINSTALL_OIDC_ISSUER")); issuer != "" {
		oidcProv, oidcErr := authz.NewOIDCProvider(ctx, authz.OIDCConfig{
			IssuerURL:    issuer,
			ClientID:     strings.TrimSpace(os.Getenv("KINSTALL_OIDC_CLIENT_ID")),
			ClientSecret: strings.TrimSpace(os.Getenv("KINSTALL_OIDC_CLIENT_SECRET")),
			RedirectURL:  strings.TrimSpace(os.Getenv("KINSTALL_OIDC_REDIRECT_URL")),
			AdminGroup:   strings.TrimSpace(os.Getenv("KINSTALL_OIDC_ADMIN_GROUP")),
		}, nil)
		if oidcErr != nil {
			logger.Warn("failed to initialize OIDC provider; falling back to local auth", slog.String("error", oidcErr.Error()))
		} else {
			authSvc.ConfigureOIDC(oidcProv)
			logger.Info("enabled OIDC PKCE SSO provider", slog.String("issuer", issuer))
		}
	}
	credDecrypt := func(dctx context.Context, credID string) ([]byte, error) {
		c, cerr := st.Credentials().Get(dctx, credID)
		if cerr != nil {
			return nil, cerr
		}
		return secStore.Decrypt(dctx, "credentials", c.ID, c.KeyVersion, c.EnvelopeJSON)
	}
	artStore := catalog.NewStore(filepath.Join(dataDir, "artifacts"), "/var/lib/kinstall/artifacts")
	mediaRes := source.NewPolicyMediaResolver(filepath.Join(dataDir, "cache")).
		WithCredentialDecryptor(credDecrypt).
		WithCatalog(artStore)
	pkiIssuer := pki.NewIssuer()
	kcMgr := multicluster.NewKubeconfigManager(st.Kubeconfigs(), pkiIssuer, audRec)
	tplEngine := template.New(func(sctx context.Context, credRef string) (string, error) {
		raw, derr := credDecrypt(sctx, credRef)
		if derr != nil {
			return "", derr
		}
		val := string(raw)
		clear(raw)
		return val, nil
	})
	locker := lock.NewManager(st.Locks())
	notifier := notify.NewDispatcher(st.Notifications(), 5*time.Minute).WithSecretResolver(credDecrypt)
	worker := engine.NewWorker(
		st, locker, execChain, secStore, mediaRes, pkiIssuer, tplEngine, tplEngine, evHub, audRec, osEng, logger,
		engine.WithHostResolver(func(rctx context.Context, op *domain.Operation, sel sdk.HostSelector) ([]engine.HostExecBinding, error) {
			return app.ResolveOperationHosts(rctx, st, secStore, op, sel)
		}),
		engine.WithNotifier(func(nctx context.Context, projectID, eventType, severity, dedupKey, summary string) {
			_, _, _ = notifier.Dispatch(nctx, notify.EventMessage{
				EventType:  eventType,
				ProjectID:  projectID,
				Severity:   severity,
				DedupKey:   dedupKey,
				Summary:    summary,
				OccurredAt: time.Now().UTC(),
			})
		}),
	)

	facade := app.NewFacade(st, reg, execChain, secStore, mediaRes, evHub, audRec, authSvc, worker, osEng, kcMgr, enableGM, kekKind, alg).
		WithArtifactStore(artStore).
		WithSSHPool(sshPool)

	// Bootstrap initial local admin account when database is fresh
	ensureBootstrapAdmin(ctx, authSvc, st, logger)

	return &Application{
		Store:   st,
		SSHPool: sshPool,
		Events:  evHub,
		Worker:  worker,
		AuthSvc: authSvc,
		Facade:  facade,
		I18n:    cat,
		Metrics: obs.NewMetrics(),
		Logger:  logger,
	}, nil
}

func initKEKProvider(ctx context.Context, dataDir string, logger *slog.Logger) (ports.KEKProvider, string, error) {
	// 1. OpenBao 2.7.x Transit KEK (D1 §3.2) when KINSTALL_OPENBAO_ADDR is configured
	if addr := strings.TrimSpace(os.Getenv("KINSTALL_OPENBAO_ADDR")); addr != "" {
		keyName := strings.TrimSpace(os.Getenv("KINSTALL_OPENBAO_KEY"))
		if keyName == "" {
			keyName = "kinstall-kek"
		}
		mount := strings.TrimSpace(os.Getenv("KINSTALL_OPENBAO_MOUNT"))
		if mount == "" {
			mount = "transit"
		}
		devInsec := strings.EqualFold(os.Getenv("KINSTALL_OPENBAO_DEV_INSECURE"), "true") || os.Getenv("KINSTALL_OPENBAO_DEV_INSECURE") == "1"
		bao, err := secrets.NewOpenBaoKEKProvider(ctx, secrets.OpenBaoConfig{
			Address:     addr,
			Token:       strings.TrimSpace(os.Getenv("KINSTALL_OPENBAO_TOKEN")),
			Mount:       mount,
			KeyName:     keyName,
			DevInsecure: devInsec,
		})
		if err != nil {
			return nil, "", fmt.Errorf("init openbao transit KEK: %w", err)
		}
		logger.Info("initialized OpenBao Transit KEK provider", slog.String("address", addr), slog.String("key", keyName))
		return bao, "openbao", nil
	}

	// 2. Environment variable KEK (05 §5.5) — immediately unsets KINSTALL_MASTER_KEY after load
	if strings.TrimSpace(os.Getenv("KINSTALL_MASTER_KEY")) != "" {
		envKEK, err := secrets.NewEnvKEKProvider("KINSTALL_MASTER_KEY")
		if err != nil {
			return nil, "", fmt.Errorf("init env KEK: %w", err)
		}
		logger.Info("initialized environment variable KEK provider")
		return envKEK, "env", nil
	}

	// 3. Local 0600 keyring file (default single-node / offline setup)
	keyPath := filepath.Join(dataDir, "master.key")
	// #nosec G703 -- keyPath derived from cleaned operator KINSTALL_DATA_DIR
	if _, statErr := os.Stat(keyPath); errors.Is(statErr, os.ErrNotExist) {
		if genErr := secrets.GenerateKeyringFile(keyPath); genErr != nil {
			return nil, "", fmt.Errorf("generate master keyring: %w", genErr)
		}
	}
	fileKEK, err := secrets.NewFileKEKProvider("local-file-kek", keyPath)
	if err != nil {
		return nil, "", err
	}
	return fileKEK, "file", nil
}

func ensureBootstrapAdmin(ctx context.Context, authSvc *authz.Service, st *store.DBStore, logger *slog.Logger) {
	if _, err := st.Auth().GetUserByUsername(ctx, "admin"); err == nil {
		return
	}
	pass := strings.TrimSpace(os.Getenv("KINSTALL_ADMIN_PASSWORD"))
	if pass == "" {
		pass = "Kinstall#2026!"
	}
	_, err := authSvc.CreateLocalUser(ctx, "admin", "Platform Administrator", pass, []domain.RoleBinding{
		{ProjectID: "*", Role: domain.RoleAdmin},
	})
	if err == nil {
		logger.Info("bootstrapped initial admin user", slog.String("username", "admin"))
	}
}

// RunServer starts the HTTP/SSE server, background worker loop, and PG LISTEN loop with reverse-order graceful shutdown (G1 §7.3).
func (a *Application) RunServer(parentCtx context.Context, listenAddr string, devNoAuth bool) error {
	ctx, stop := signal.NotifyContext(parentCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	handler := server.NewRouter(ctx, a.Facade, a.AuthSvc, a.I18n, a.Logger, server.Options{
		DevBypassAuth: devNoAuth,
		Metrics:       a.Metrics,
	})
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      60 * time.Second, // cleared per-SSE stream via http.ResponseController (G1 §3.2)
		IdleTimeout:       120 * time.Second,
	}

	// Start background gocron/v2 periodic Facts/Baseline scheduler (T-M1-04)
	cronInterval := 30 * time.Minute
	if rawInt := strings.TrimSpace(os.Getenv("KINSTALL_FACTS_CRON_INTERVAL")); rawInt != "" {
		if d, perr := time.ParseDuration(rawInt); perr == nil && d >= time.Minute {
			cronInterval = d
		}
	}
	factsSched, _ := inventory.NewFactsScheduler(cronInterval, a.Logger, func(sctx context.Context) {
		n := a.Facade.RefreshAllHostsBaseline(sctx)
		if n > 0 {
			a.Logger.Info("periodic host baseline check finished", slog.Int("hostsChecked", n))
		}
	})
	factsSched.Start()
	defer func() { _ = factsSched.Shutdown() }()

	// Start background PG LISTEN & Worker loops
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		_ = a.Events.RunListenLoop(ctx)
	}()

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		_ = a.Worker.RunClaimLoop(ctx)
	}()

	errCh := make(chan error, 1)
	go func() {
		a.Logger.Info("kinstall server listening", slog.String("addr", listenAddr), slog.Bool("devNoAuth", devNoAuth))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		a.Logger.Info("shutdown signal received; starting reverse-order drain (G1 §7.3)")
		// 1. Mark worker draining so /readyz returns 503 immediately
		a.Worker.SetDraining(true)
		// 2. Gracefully shut down HTTP server (closes idle conns & finishes in-flight APIs)
		shutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		// 3. Wait for Worker & EventHub loops to stop
		<-workerDone
		<-listenDone
		return nil
	}
}

// Close releases SSH pools and database handles in reverse initialization order.
func (a *Application) Close() {
	if a.SSHPool != nil {
		_ = a.SSHPool.Close()
	}
	if a.Store != nil {
		_ = a.Store.Close()
	}
}

func (a *Application) NewRootCommand() *cli.Deps {
	return &cli.Deps{
		Facade:      a.Facade,
		StartServer: a.RunServer,
		RunAPIProxy: a.RunAPIProxy,
		Out:         os.Stdout,
	}
}

// RunAPIProxy launches the node-local K8s APIServer L4 proxy with SIGHUP zero-downtime hot reload (T-M1-20, P1 §3.1).
func (a *Application) RunAPIProxy(parentCtx context.Context, listenAddr, configPath string) error {
	ctx, stop := signal.NotifyContext(parentCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	p := apiproxy.New(apiproxy.Options{ListenAddr: listenAddr, Logger: a.Logger})
	if err := p.ReloadConfigFile(configPath); err != nil {
		return err
	}
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				if err := p.ReloadConfigFile(configPath); err != nil {
					a.Logger.Error("apiserver-proxy SIGHUP reload failed", slog.String("error", err.Error()))
				} else {
					a.Logger.Info("apiserver-proxy reloaded backends via SIGHUP")
				}
			}
		}
	}()
	return p.Start(ctx)
}
