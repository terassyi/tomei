package main

import (
	"os"

	"github.com/terassyi/toto/internal/errors"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		formatter := errors.NewFormatter(os.Stderr, false)
		output := formatter.Format(err)
		os.Stderr.WriteString(output)
		os.Exit(1)
	}
}
