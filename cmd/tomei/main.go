package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/terassyi/tomei/internal/errors"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		formatter := errors.NewFormatter(os.Stderr, false)
		output := formatter.Format(err)
		os.Stderr.WriteString(output)
		os.Exit(1)
	}
}
