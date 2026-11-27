package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kinstall/kinstall/internal/agenthub"
	"github.com/kinstall/kinstall/internal/cli"
	"github.com/kinstall/kinstall/internal/obs"
)

func main() {
	// Fast-path zero-DB edge execution for `kinstall agent` (no SQLite/master.key required on edge hosts)
	if len(os.Args) > 1 && os.Args[1] == "agent" {
		fs := flag.NewFlagSet("agent", flag.ExitOnError)
		serverURL := fs.String("server", os.Getenv("KINSTALL_SERVER"), "Control plane URL")
		token := fs.String("token", os.Getenv("KINSTALL_TOKEN"), "One-time enroll token")
		zone := fs.String("zone", "default", "Security zone")
		stateDir := fs.String("state-dir", "", "Agent state directory")
		_ = fs.Parse(os.Args[2:])

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		logger := obs.NewLogger(os.Getenv("KINSTALL_LOG_FORMAT"), os.Getenv("KINSTALL_LOG_LEVEL"))
		if err := agenthub.RunAgent(ctx, agenthub.RunnerConfig{
			ServerURL:   *serverURL,
			EnrollToken: *token,
			Zone:        *zone,
			StateDir:    *stateDir,
			Logger:      logger,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "kinstall agent error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx := context.Background()
	application, err := BuildApplication(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kinstall init error: %v\n", err)
		os.Exit(1)
	}
	defer application.Close()

	rootCmd := cli.NewRootCmd(*application.NewRootCommand())
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
