package ui

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTUILogHandler_WarnAndErrorAreSent(t *testing.T) {
	t.Parallel()
	s := &mockSender{}
	handler := NewTUILogHandler(s, slog.LevelWarn)
	logger := slog.New(handler)

	logger.Warn("first warning")
	logger.Error("first error")

	msgs := s.messages()
	require.Len(t, msgs, 2)

	msg0 := msgs[0].(slogMsg)
	assert.Equal(t, slog.LevelWarn, msg0.level)
	assert.Equal(t, "first warning", msg0.message)

	msg1 := msgs[1].(slogMsg)
	assert.Equal(t, slog.LevelError, msg1.level)
	assert.Equal(t, "first error", msg1.message)
}

func TestTUILogHandler_DebugAndInfoIgnoredAtWarnLevel(t *testing.T) {
	t.Parallel()
	s := &mockSender{}
	handler := NewTUILogHandler(s, slog.LevelWarn)
	logger := slog.New(handler)

	logger.Debug("debug msg")
	logger.Info("info msg")

	msgs := s.messages()
	assert.Empty(t, msgs)
}

func TestTUILogHandler_AllLevelsAtDebugLevel(t *testing.T) {
	t.Parallel()
	s := &mockSender{}
	handler := NewTUILogHandler(s, slog.LevelDebug)
	logger := slog.New(handler)

	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")

	msgs := s.messages()
	require.Len(t, msgs, 4)

	assert.Equal(t, slog.LevelDebug, msgs[0].(slogMsg).level)
	assert.Equal(t, slog.LevelInfo, msgs[1].(slogMsg).level)
	assert.Equal(t, slog.LevelWarn, msgs[2].(slogMsg).level)
	assert.Equal(t, slog.LevelError, msgs[3].(slogMsg).level)
}

func TestTUILogHandler_AttrsIncludedInMessage(t *testing.T) {
	t.Parallel()
	s := &mockSender{}
	handler := NewTUILogHandler(s, slog.LevelWarn)
	logger := slog.New(handler)

	logger.Warn("failed to backup", "error", "permission denied", "path", "/tmp/state.json")

	msgs := s.messages()
	require.Len(t, msgs, 1)

	msg := msgs[0].(slogMsg)
	assert.Contains(t, msg.message, "failed to backup")
	assert.Contains(t, msg.message, "permission denied")
	assert.Contains(t, msg.message, "/tmp/state.json")
}

func TestTUILogHandler_WithAttrs(t *testing.T) {
	t.Parallel()
	s := &mockSender{}
	handler := NewTUILogHandler(s, slog.LevelWarn)
	child := handler.WithAttrs([]slog.Attr{slog.String("component", "engine")})
	logger := slog.New(child)

	logger.Warn("something happened")

	msgs := s.messages()
	require.Len(t, msgs, 1)

	msg := msgs[0].(slogMsg)
	assert.Contains(t, msg.message, "component")
	assert.Contains(t, msg.message, "engine")
}

func TestTUILogHandler_WithGroup(t *testing.T) {
	t.Parallel()
	s := &mockSender{}
	handler := NewTUILogHandler(s, slog.LevelWarn)
	child := handler.WithGroup("installer")
	logger := slog.New(child)

	logger.Warn("download failed", "url", "https://example.com")

	msgs := s.messages()
	require.Len(t, msgs, 1)

	msg := msgs[0].(slogMsg)
	assert.Contains(t, msg.message, "installer.url")
}
