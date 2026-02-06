package main

import (
	"os"

	"github.com/terassyi/toto/internal/errors"
)

var version = "dev"

func main() {
	if err := rootCmd.Execute(); err != nil {
		formatter := errors.NewFormatter(os.Stderr, false)
		output := formatter.Format(err)
		os.Stderr.WriteString(output)
		os.Exit(1)
	}
}
