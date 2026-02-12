package ui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// TUILogHandler is a slog.Handler that forwards log records to a Bubble Tea
// program via Send(). Only records at or above the configured level are sent.
type TUILogHandler struct {
	target sender     // tea.Program (Send-capable)
	level  slog.Level // minimum level to forward
	attrs  []slog.Attr
	group  string
}

// NewTUILogHandler creates a handler that sends slogMsg to the given sender.
func NewTUILogHandler(target sender, level slog.Level) *TUILogHandler {
	return &TUILogHandler{
		target: target,
		level:  level,
	}
}

// Enabled reports whether the handler handles records at the given level.
func (h *TUILogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle formats the record and sends it to the TUI as a slogMsg.
func (h *TUILogHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)

	// Append handler-level attrs
	for _, a := range h.attrs {
		fmt.Fprintf(&b, " %s=%q", h.qualifiedKey(a.Key), a.Value)
	}

	// Append record-level attrs
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%q", h.qualifiedKey(a.Key), a.Value)
		return true
	})

	h.target.Send(slogMsg{
		level:   r.Level,
		message: b.String(),
	})
	return nil
}

// WithAttrs returns a new handler with the given attributes.
func (h *TUILogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &TUILogHandler{
		target: h.target,
		level:  h.level,
		attrs:  newAttrs,
		group:  h.group,
	}
}

// WithGroup returns a new handler with the given group name.
func (h *TUILogHandler) WithGroup(name string) slog.Handler {
	newGroup := name
	if h.group != "" {
		newGroup = h.group + "." + name
	}
	return &TUILogHandler{
		target: h.target,
		level:  h.level,
		attrs:  h.attrs,
		group:  newGroup,
	}
}

// qualifiedKey prepends the group prefix to a key.
func (h *TUILogHandler) qualifiedKey(key string) string {
	if h.group == "" {
		return key
	}
	return h.group + "." + key
}
