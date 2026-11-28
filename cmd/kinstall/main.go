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
	"syscall"
	"time"

	"github.com/kinstall/kinstall/internal/catalog"
	"github.com/kinstall/kinstall/internal/cmdb"
	"github.com/kinstall/kinstall/internal/config"
	"github.com/kinstall/kinstall/internal/delivery"
	"github.com/kinstall/kinstall/internal/execplane"
	"github.com/kinstall/kinstall/internal/iam"
	"github.com/kinstall/kinstall/internal/server"
	"github.com/kinstall/kinstall/migrations"
	"github.com/kinstall/kinstall/pkg/platform/audit"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/eventbus"
	"github.com/spf13/cobra"
)

const AppVersion = "2.0.0"

func main() {
	logger := slog.New(audit.NewRedactingHandler(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.SetDefault(logger)

	root := &cobra.Command{
		Use:   "kinstall",
		Short: "Kinstall v2 — 统一动态元模型 CMDB 资产平台与声明式交付运维引擎",
	}

	root.AddCommand(newVersionCmd(), newServerCmd(logger), newAgentCmd(logger))
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "显示 Kinstall 版本信息",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("kinstall v%s (CGO_ENABLED=0 unified CMDB + IaC Delivery + Zero-Trust Agent)\n", AppVersion)
		},
	}
}

func defaultDataDir() string {
	if v := os.Getenv("KINSTALL_DATA_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".kinstall"
	}
	return filepath.Join(home, ".kinstall")
}

func newServerCmd(logger *slog.Logger) *cobra.Command {
	var (
		listen  string
		driver  string
		dsn     string
		dataDir string
	)
	cmd := &cobra.Command{
		Use:   "server",
		Short: "控制面服务端管理",
	}
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "启动 Kinstall v2 控制面 HTTP/SSE/WebSocket 服务",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dataDir == "" {
				dataDir = defaultDataDir()
			}
			if dsn == "" && driver == "sqlite" {
				dsn = filepath.Join(dataDir, "kinstall.db")
			}
			return runServer(cmd.Context(), logger, listen, driver, dsn, dataDir)
		},
	}
	startCmd.Flags().StringVar(&listen, "listen", "0.0.0.0:8080", "HTTP 监听地址")
	startCmd.Flags().StringVar(&driver, "db-driver", "sqlite", "数据库驱动 (sqlite | postgres)")
	startCmd.Flags().StringVar(&dsn, "db-dsn", "", "数据库连接串 (SQLite 文件路径或 PostgreSQL URL)")
	startCmd.Flags().StringVar(&dataDir, "data-dir", "", "数据目录 (默认 ~/.kinstall)")

	cmd.AddCommand(startCmd)
	return cmd
}

func newAgentCmd(logger *slog.Logger) *cobra.Command {
	var cfg execplane.EdgeAgentConfig
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "启动零信任单向出站纯 Go 执行面 Agent（零数据库依赖）",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.Version = AppVersion
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return execplane.RunEdgeAgent(ctx, cfg, logger)
		},
	}
	cmd.Flags().StringVar(&cfg.ServerURL, "server", "http://127.0.0.1:8080", "控制面地址")
	cmd.Flags().StringVar(&cfg.EnrollToken, "token", "", "一次性纳管令牌 (首次纳管必填)")
	cmd.Flags().StringVar(&cfg.Zone, "zone", "prod-core", "所属安全域/网络区域")
	cmd.Flags().StringVar(&cfg.IdentityFile, "identity-file", "", "Agent 身份凭据持久化文件路径 (0600)")
	cmd.Flags().StringVar(&cfg.Hostname, "hostname", "", "覆盖上报主机名")
	return cmd
}

func runServer(parentCtx context.Context, logger *slog.Logger, listen, driver, dsn, dataDir string) error {
	ctx, stop := signal.NotifyContext(parentCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	codec, err := crypto.LoadOrCreateKeyring(filepath.Join(dataDir, "keyring.v2.json"))
	if err != nil {
		return fmt.Errorf("init keyring: %w", err)
	}

	store, err := db.Open(ctx, db.Config{Driver: driver, DSN: dsn})
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = store.Close() }()

	mfs, err := migrations.ForDialect(store.Dialect())
	if err != nil {
		return err
	}
	if err := store.Migrate(ctx, mfs); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	authMgr := auth.NewManager(codec.SigningKey("session-jwt"), 24*time.Hour)
	broker := eventbus.NewLogBroker(2000)

	iamSvc := iam.NewService(store, codec, authMgr)
	defaultWSID, err := iamSvc.Bootstrap(ctx)
	if err != nil {
		return fmt.Errorf("iam bootstrap: %w", err)
	}

	cmdbSvc := cmdb.NewService(store)
	if err := cmdbSvc.EnsureSeeded(ctx, defaultWSID); err != nil {
		return fmt.Errorf("seed cmdb: %w", err)
	}

	cfgSvc := config.NewService(store, codec, iamSvc)
	if err := cfgSvc.EnsureSeeded(ctx, defaultWSID); err != nil {
		return fmt.Errorf("seed config: %w", err)
	}

	catalogSvc := catalog.NewService(store, codec, iamSvc, dataDir)
	if err := catalogSvc.EnsureSeeded(ctx, defaultWSID); err != nil {
		return fmt.Errorf("seed catalog: %w", err)
	}

	execSvc := execplane.NewService(store, cmdbSvc, iamSvc, catalogSvc, broker)
	deliverySvc := delivery.NewService(store, cmdbSvc, execSvc, broker)
	if err := deliverySvc.EnsureSeededPlugins(ctx, defaultWSID); err != nil {
		return fmt.Errorf("seed plugins: %w", err)
	}

	srv := server.New(server.Config{
		Version:     AppVersion,
		DefaultWSID: defaultWSID,
		Logger:      logger,
		AuthMgr:     authMgr,
		Broker:      broker,
		IAM:         iamSvc,
		CMDB:        cmdbSvc,
		ConfigSvc:   cfgSvc,
		Catalog:     catalogSvc,
		Exec:        execSvc,
		Delivery:    deliverySvc,
		PingDB:      store.Ping,
	})

	httpSrv := &http.Server{
		Addr:              listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("Kinstall v2 server listening", "addr", listen, "driver", driver, "dataDir", dataDir, "workspaceId", defaultWSID)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down Kinstall v2 server gracefully")
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shCtx)
	case err := <-errCh:
		return err
	}
}
