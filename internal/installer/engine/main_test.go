package engine

import (
	"log/slog"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Suppress WARN-level logs (e.g. state validation warnings from mock stores)
	// to keep test output clean.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	})))
	os.Exit(m.Run())
}
