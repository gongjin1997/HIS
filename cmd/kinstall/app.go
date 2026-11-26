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
	"github.com/kinstall/kinstall/internal/artifact/source"
	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/cli"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/engine"
	"github.com/kinstall/kinstall/internal/events"
	"github.com/kinstall/kinstall/internal/executor/sshx"
	"github.com/kinstall/kinstall/internal/i18n"
	"github.com/kinstall/kinstall/internal/lock"
	"github.com/kinstall/kinstall/internal/multicluster"
	"github.com/kinstall/kinstall/internal/obs"
	"github.com/kinstall/kinstall/internal/osadapter"
	"github.com/kinstall/kinstall/internal/pki"
	"github.com/kinstall/kinstall/internal/registry"
	"github.com/kinstall/kinstall/internal/secrets"
	"github.com/kinstall/kinstall/internal/server"
	"github.com/kinstall/kinstall/internal/store"
	"github.com/kinstall/kinstall/internal/template"
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
	enableGM := strings.EqualFold(os.Getenv("KINSTALL_GM_ENABLED"), "true") || os.Getenv("KINSTALL_GM_ENABLED") == "1"
	alg := secrets.AlgAES256GCM
	if enableGM {
		alg = secrets.AlgSM4GCM
	}
	keyPath := filepath.Join(dataDir, "master.key")
	// #nosec G703 -- keyPath derived from cleaned operator KINSTALL_DATA_DIR
	if _, statErr := os.Stat(keyPath); errors.Is(statErr, os.ErrNotExist) {
		if genErr := secrets.GenerateKeyringFile(keyPath); genErr != nil {
			_ = st.Close()
			return nil, fmt.Errorf("generate master keyring: %w", genErr)
		}
	}
	kek, err := secrets.NewFileKEKProvider("local-file-kek", keyPath)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	secStore := secrets.NewEnvelopeStore(kek, "file", alg)

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
	mediaRes := source.NewPolicyMediaResolver(filepath.Join(dataDir, "cache"))
	pkiIssuer := pki.NewIssuer()
	kcMgr := multicluster.NewKubeconfigManager(st.Kubeconfigs(), pkiIssuer, audRec)
	tplEngine := template.New(nil)
	locker := lock.NewManager(st.Locks())
	worker := engine.NewWorker(st, locker, execChain, secStore, mediaRes, pkiIssuer, tplEngine, tplEngine, evHub, audRec, osEng, logger)

	facade := app.NewFacade(st, reg, execChain, secStore, mediaRes, evHub, audRec, authSvc, worker, osEng, kcMgr, enableGM, "file", alg)

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
