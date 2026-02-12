package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/terassyi/tomei/internal/resource"
)

const (
	tickInterval = 50 * time.Millisecond
	maxLogLines  = 5
)

// taskStatus represents the current state of a task.
type taskStatus int

const (
	taskRunning taskStatus = iota
	taskDone
	taskFailed
)

// taskState holds the state for a single resource being installed.
type taskState struct {
	key         string
	kind        resource.Kind
	name        string
	version     string
	method      string
	action      resource.ActionType
	status      taskStatus
	startTime   time.Time
	downloaded  int64
	total       int64
	hasProgress bool // true after first EventProgress received
	installPath string
	logLines    []string
	elapsed     time.Duration // set on complete/error; for running tasks, computed from startTime
	err         error
}

// layerState holds the snapshot of a completed layer.
type layerState struct {
	elapsed        time.Duration
	tasks          map[string]*taskState
	taskOrder      []string
	completedOrder []string // keys in completion order (done/failed)
}

// ApplyModel is the Bubble Tea model for the apply TUI.
type ApplyModel struct {
	// All layer information (set from first EventLayerStart.AllLayerNodes)
	allLayerNodes [][]string
	totalLayers   int

	// Current layer index
	currentLayer int
	layerStart   time.Time
	layerElapsed time.Duration // cached for View()

	// Completed layer snapshots (indexed by layer number)
	completedLayers []*layerState

	// Timing
	applyStart   time.Time
	totalElapsed time.Duration // cached for View()

	// Current layer tasks (only tasks with EventStart are tracked)
	tasks          map[string]*taskState
	taskOrder      []string
	completedOrder []string // keys in completion order (done/failed)

	// Results
	results *ApplyResults

	// State
	done  bool
	err   error
	width int
}

// NewApplyModel creates a new ApplyModel.
func NewApplyModel(results *ApplyResults) *ApplyModel {
	return &ApplyModel{
		tasks:   make(map[string]*taskState),
		results: results,
		width:   80,
	}
}

// Init implements tea.Model.
func (m *ApplyModel) Init() tea.Cmd {
	return tick()
}

// Err returns the error from apply, if any.
func (m *ApplyModel) Err() error {
	return m.err
}

// FinalView returns the final rendered output for printing after AltScreen exits.
// This is the same as View() but intended for post-run output to scrollback.
func (m *ApplyModel) FinalView() string {
	return m.View()
}

// tick returns a command that sends a tickMsg after the tick interval.
func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
