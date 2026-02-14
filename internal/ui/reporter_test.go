package ui

import (
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/resource"
)

// mockSender collects sent messages for testing.
type mockSender struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (m *mockSender) Send(msg tea.Msg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, msg)
}

func (m *mockSender) messages() []tea.Msg {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]tea.Msg, len(m.msgs))
	copy(result, m.msgs)
	return result
}

func TestThrottledReporter_ForwardsNonProgressEvents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		eventType engine.EventType
	}{
		{name: "EventStart", eventType: engine.EventStart},
		{name: "EventOutput", eventType: engine.EventOutput},
		{name: "EventComplete", eventType: engine.EventComplete},
		{name: "EventError", eventType: engine.EventError},
		{name: "EventLayerStart", eventType: engine.EventLayerStart},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ms := &mockSender{}
			r := NewThrottledReporter(ms)

			event := engine.Event{
				Type: tt.eventType,
				Kind: resource.KindTool,
				Name: "test",
			}
			r.HandleEvent(event)

			msgs := ms.messages()
			require.Len(t, msgs, 1)
			msg, ok := msgs[0].(engineEventMsg)
			require.True(t, ok)
			assert.Equal(t, tt.eventType, msg.event.Type)
		})
	}
}

func TestThrottledReporter_ThrottlesProgressEvents(t *testing.T) {
	t.Parallel()
	ms := &mockSender{}
	r := NewThrottledReporter(ms)

	event := engine.Event{
		Type:       engine.EventProgress,
		Kind:       resource.KindTool,
		Name:       "bat",
		Downloaded: 100,
		Total:      1000,
	}

	// First event should pass through
	r.HandleEvent(event)
	require.Len(t, ms.messages(), 1)

	// Immediate second event should be throttled
	event.Downloaded = 200
	r.HandleEvent(event)
	assert.Len(t, ms.messages(), 1, "second progress event should be throttled")

	// After throttle interval, event should pass through
	r.mu.Lock()
	r.lastProgress[progressKey(resource.KindTool, "bat")] = time.Now().Add(-progressThrottleInterval - time.Millisecond)
	r.mu.Unlock()

	event.Downloaded = 500
	r.HandleEvent(event)
	assert.Len(t, ms.messages(), 2, "event after throttle interval should pass through")
}

func TestThrottledReporter_ThrottlesPerResource(t *testing.T) {
	t.Parallel()
	ms := &mockSender{}
	r := NewThrottledReporter(ms)

	eventA := engine.Event{
		Type: engine.EventProgress,
		Kind: resource.KindTool,
		Name: "bat",
	}
	eventB := engine.Event{
		Type: engine.EventProgress,
		Kind: resource.KindTool,
		Name: "rg",
	}

	// Both first events should pass through
	r.HandleEvent(eventA)
	r.HandleEvent(eventB)
	assert.Len(t, ms.messages(), 2, "first events for different resources should both pass")

	// Second events should be throttled independently
	r.HandleEvent(eventA)
	r.HandleEvent(eventB)
	assert.Len(t, ms.messages(), 2, "second events should be throttled")
}

func TestThrottledReporter_Done(t *testing.T) {
	t.Parallel()
	ms := &mockSender{}
	r := NewThrottledReporter(ms)

	r.Done(nil)

	msgs := ms.messages()
	require.Len(t, msgs, 1)
	msg, ok := msgs[0].(applyDoneMsg)
	require.True(t, ok)
	assert.NoError(t, msg.err)
}

func TestThrottledReporter_DoneWithError(t *testing.T) {
	t.Parallel()
	ms := &mockSender{}
	r := NewThrottledReporter(ms)

	r.Done(assert.AnError)

	msgs := ms.messages()
	require.Len(t, msgs, 1)
	msg, ok := msgs[0].(applyDoneMsg)
	require.True(t, ok)
	assert.ErrorIs(t, msg.err, assert.AnError)
}
