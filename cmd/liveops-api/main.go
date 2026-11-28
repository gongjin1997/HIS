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

	"github.com/kinstall/kinstall/internal/bootstrap"
	httptransport "github.com/kinstall/kinstall/internal/transport/http"
	"github.com/kinstall/kinstall/internal/worker"
	"github.com/kinstall/kinstall/pkg/platform/probes"
	"github.com/kinstall/kinstall/web"
)

func main() {
	defaultDir := bootstrap.DefaultDataDir()
	listen := flag.String("listen", "0.0.0.0:8080", "HTTP listen address")
	dbDriver := flag.String("db-driver", "sqlite", "Database driver (sqlite | postgres)")
	dbDSN := flag.String("db-dsn", filepath.Join(defaultDir, "liveops.db"), "Database DSN or SQLite file path")
	keyring := flag.String("keyring", filepath.Join(defaultDir, "keyring.json"), "KMS keyring path")
	embeddedWorker := flag.Bool("embedded-worker", false, "Run worker loops inside API process (dev single-process mode)")
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

	probeReg := probes.NewRegistry("liveops-api", bootstrap.Version, c.DB.Ping)
	router := httptransport.NewRouter(httptransport.Dependencies{
		Identity: c.Identity,
		Assets:   c.Assets,
		Config:   c.Config,
		Deploy:   c.Deploy,
		Nodes:    c.Nodes,
		Gateway:  c.Gateway,
		Tokens:   c.Tokens,
		Probes:   probeReg,
		GWSecret: c.GWSecret,
		StaticFS: web.DistFS(),
		Logger:   c.Logger,
	})

	if *embeddedWorker {
		runner := worker.NewRunner(worker.Config{WorkerID: "embedded-api-worker"}, c.Repo, c.Deploy, c.Nodes, nil, c.Logger)
		go runner.Run(ctx)
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		probeReg.MarkDraining()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	probeReg.MarkStarted()
	c.Logger.Info("liveops-api listening", "addr", *listen, "dialect", c.DB.Dialect(), "embeddedWorker", *embeddedWorker)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		c.Logger.Error("liveops-api exited with error", "err", err)
		os.Exit(1)
	}
}
