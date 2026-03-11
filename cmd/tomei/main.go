package main

import (
	"context"
	stderrors "errors"
	"fmt"
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
	if err := run(); err != nil {
		if stderrors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		// Context canceled due to a termination signal.
		if stderrors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "Interrupted.")
			return err
		}
		formatter := errors.NewFormatter(os.Stderr, false)
		output := formatter.Format(err)
		os.Stderr.WriteString(output)
		return err
	}
	return nil
}
