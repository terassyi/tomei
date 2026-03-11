package main

import (
	"context"
	stderrors "errors"
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
		// Context canceled due to a termination signal: already printed
		// "Interrupted." to stdout; exit with status 130 and no extra
		// stderr output.
		if stderrors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		formatter := errors.NewFormatter(os.Stderr, false)
		output := formatter.Format(err)
		os.Stderr.WriteString(output)
		os.Exit(1)
	}
}
