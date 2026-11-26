package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kinstall/kinstall/internal/cli"
)

func main() {
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
