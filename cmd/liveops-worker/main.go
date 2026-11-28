package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/bootstrap"
	"github.com/kinstall/kinstall/internal/worker"
	"github.com/kinstall/kinstall/pkg/platform/probes"
)

func main() {
	defaultDir := bootstrap.DefaultDataDir()
	probeListen := flag.String("probe-listen", "127.0.0.1:18085", "Worker K8s probes HTTP listen address")
	dbDriver := flag.String("db-driver", "sqlite", "Database driver (sqlite | postgres)")
	dbDSN := flag.String("db-dsn", filepath.Join(defaultDir, "liveops.db"), "Database DSN or SQLite file path")
	keyring := flag.String("keyring", filepath.Join(defaultDir, "keyring.json"), "KMS keyring path")
	workerID := flag.String("worker-id", "", "Explicit worker instance identifier")
	concurrency := flag.Int("concurrency", 4, "Max concurrent task executions")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	c, err := bootstrap.Build(ctx, bootstrap.Options{
		DBDriver:    *dbDriver,
		DBDSN:       *dbDSN,
		KeyringPath: *keyring,
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = c.Close() }()

	probeReg := probes.NewRegistry("liveops-worker", bootstrap.Version, c.DB.Ping)
	pr := chi.NewRouter()
	pr.Get("/startupz", probeReg.HandleStartupz)
	pr.Get("/livez", probeReg.HandleLivez)
	pr.Get("/readyz", probeReg.HandleReadyz)
	pr.Get("/healthy", probeReg.HandleLegacyHealthy)

	srv := &http.Server{
		Addr:              *probeListen,
		Handler:           pr,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.Logger.Error("worker probe server error", "err", err)
		}
	}()

	probeReg.MarkStarted()
	c.Logger.Info("liveops-worker ready", "probeAddr", *probeListen, "dialect", c.DB.Dialect())

	runner := worker.NewRunner(worker.Config{
		WorkerID:    *workerID,
		Concurrency: *concurrency,
	}, c.Repo, c.Deploy, c.Nodes, nil, c.Logger)

	runner.Run(ctx)
	probeReg.MarkDraining()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	os.Exit(0)
}
