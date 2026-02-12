package ui

import (
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/resource"
)

const progressThrottleInterval = 100 * time.Millisecond

// sender abstracts tea.Program.Send for testing.
type sender interface {
	Send(msg tea.Msg)
}

// ThrottledReporter bridges engine events to Bubble Tea,
// throttling EventProgress to reduce UI update frequency.
type ThrottledReporter struct {
	target       sender
	mu           sync.Mutex
	lastProgress map[string]time.Time
}

// NewThrottledReporter creates a reporter that forwards events to the given sender.
func NewThrottledReporter(target sender) *ThrottledReporter {
	return &ThrottledReporter{
		target:       target,
		lastProgress: make(map[string]time.Time),
	}
}

// HandleEvent processes an engine event, throttling progress events per resource.
func (r *ThrottledReporter) HandleEvent(event engine.Event) {
	if event.Type == engine.EventProgress {
		key := progressKey(event.Kind, event.Name)
		r.mu.Lock()
		last, ok := r.lastProgress[key]
		now := time.Now()
		if ok && now.Sub(last) < progressThrottleInterval {
			r.mu.Unlock()
			return
		}
		r.lastProgress[key] = now
		r.mu.Unlock()
	}

	r.target.Send(engineEventMsg{event: event})
}

// Done sends an applyDoneMsg to signal completion.
func (r *ThrottledReporter) Done(err error) {
	r.target.Send(applyDoneMsg{err: err})
}

// progressKey returns a unique key for a resource (used by throttling).
func progressKey(kind resource.Kind, name string) string {
	return fmt.Sprintf("%s/%s", kind, name)
}
