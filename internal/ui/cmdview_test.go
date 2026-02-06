package ui

import (
	"bytes"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/resource"
)

func TestCommandView_StartTask(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	cv.StartTask("Tool/rg", resource.KindTool, "rg", "14.1.1", "download")

	cv.mu.Lock()
	task, ok := cv.tasks["Tool/rg"]
	cv.mu.Unlock()

	require.True(t, ok)
	assert.Equal(t, resource.KindTool, task.kind)
	assert.Equal(t, "rg", task.name)
	assert.Equal(t, "14.1.1", task.version)
	assert.Equal(t, "download", task.method)
	assert.False(t, task.done)
}

func TestCommandView_AddOutput(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	cv.StartTask("Tool/gopls", resource.KindTool, "gopls", "0.17.0", "go install")
	cv.AddOutput("Tool/gopls", "downloading golang.org/x/tools...")

	assert.Equal(t, "downloading golang.org/x/tools...", cv.LastLog("Tool/gopls"))
}

func TestCommandView_AddOutput_IgnoresUnknownTask(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	// Should not panic
	cv.AddOutput("Tool/nonexistent", "some output")
}

func TestCommandView_AddOutput_IgnoresDoneTask(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	cv.StartTask("Tool/rg", resource.KindTool, "rg", "1.0.0", "download")
	cv.CompleteTask("Tool/rg")
	cv.AddOutput("Tool/rg", "should be ignored")

	assert.Empty(t, cv.LastLog("Tool/rg"))
}

func TestCommandView_LastLog(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(cv *CommandView)
		key      string
		expected string
	}{
		{
			name: "returns last log",
			setup: func(cv *CommandView) {
				cv.StartTask("Tool/a", resource.KindTool, "a", "1.0", "go install")
				cv.AddOutput("Tool/a", "line 1")
				cv.AddOutput("Tool/a", "line 2")
			},
			key:      "Tool/a",
			expected: "line 2",
		},
		{
			name:     "returns empty for unknown task",
			setup:    func(cv *CommandView) {},
			key:      "Tool/unknown",
			expected: "",
		},
		{
			name: "returns empty after complete",
			setup: func(cv *CommandView) {
				cv.StartTask("Tool/b", resource.KindTool, "b", "1.0", "download")
				cv.AddOutput("Tool/b", "some log")
				cv.CompleteTask("Tool/b")
			},
			key:      "Tool/b",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cv := NewCommandView(&buf)
			tt.setup(cv)
			assert.Equal(t, tt.expected, cv.LastLog(tt.key))
		})
	}
}

func TestCommandView_CompleteTask(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	cv.StartTask("Tool/rg", resource.KindTool, "rg", "1.0.0", "download")
	cv.AddOutput("Tool/rg", "some log")
	cv.CompleteTask("Tool/rg")

	cv.mu.Lock()
	task := cv.tasks["Tool/rg"]
	cv.mu.Unlock()

	assert.True(t, task.done)
	assert.Empty(t, task.lastLog)
	assert.NoError(t, task.err)
}

func TestCommandView_FailTask(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	cv.StartTask("Tool/rg", resource.KindTool, "rg", "1.0.0", "download")
	testErr := assert.AnError
	cv.FailTask("Tool/rg", testErr)

	cv.mu.Lock()
	task := cv.tasks["Tool/rg"]
	cv.mu.Unlock()

	assert.True(t, task.done)
	assert.Equal(t, testErr, task.err)
}

func TestCommandView_PrintTaskStart(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	cv.StartTask("Tool/gopls", resource.KindTool, "gopls", "0.17.0", "go install")
	cv.PrintTaskStart("Tool/gopls")

	output := buf.String()
	assert.Contains(t, output, "Commands:")
	assert.Contains(t, output, "Tool/")
	assert.Contains(t, output, "gopls")
	assert.Contains(t, output, "0.17.0")
	assert.Contains(t, output, "go install")
}

func TestCommandView_PrintTaskStart_HeaderOnce(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	cv.StartTask("Tool/a", resource.KindTool, "a", "1.0", "go install")
	cv.StartTask("Tool/b", resource.KindTool, "b", "2.0", "go install")
	cv.PrintTaskStart("Tool/a")
	cv.PrintTaskStart("Tool/b")

	output := buf.String()
	// "Commands:" header should appear exactly once
	count := 0
	for i := 0; i+len("Commands:") <= len(output); i++ {
		if output[i:i+len("Commands:")] == "Commands:" {
			count++
		}
	}
	assert.Equal(t, 1, count, "Commands: header should appear exactly once")
}

func TestCommandView_PrintTaskStart_NilTask(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	// Should not panic
	cv.PrintTaskStart("Tool/nonexistent")
	assert.Empty(t, buf.String())
}

func TestCommandView_PrintOutput(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	cv.PrintOutput("go: downloading golang.org/x/tools v0.28.0")

	output := buf.String()
	assert.Contains(t, output, "go: downloading golang.org/x/tools v0.28.0")
	assert.Contains(t, output, "    ") // indented
}

func TestCommandView_PrintTaskComplete_Success(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	cv.StartTask("Tool/gopls", resource.KindTool, "gopls", "0.17.0", "go install")
	cv.CompleteTask("Tool/gopls")
	cv.PrintTaskComplete("Tool/gopls")

	output := buf.String()
	assert.Contains(t, output, "gopls")
	assert.Contains(t, output, "done")
}

func TestCommandView_PrintTaskComplete_Error(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	cv.StartTask("Tool/gopls", resource.KindTool, "gopls", "0.17.0", "go install")
	cv.FailTask("Tool/gopls", assert.AnError)
	cv.PrintTaskComplete("Tool/gopls")

	output := buf.String()
	assert.Contains(t, output, "gopls")
	assert.Contains(t, output, "failed")
}

func TestCommandView_PrintTaskComplete_NilTask(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	// Should not panic
	cv.PrintTaskComplete("Tool/nonexistent")
	assert.Empty(t, buf.String())
}

func TestTruncateLine(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short line",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "long line truncated",
			input:  "this is a very long line that needs truncation",
			maxLen: 20,
			want:   "this is a very lo...",
		},
		{
			name:   "trims whitespace",
			input:  "  hello  ",
			maxLen: 10,
			want:   "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateLine(tt.input, tt.maxLen)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCommandView_ConcurrentAccess(t *testing.T) {
	var buf bytes.Buffer
	cv := NewCommandView(&buf)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n * 3)

	// Concurrent StartTask + AddOutput + LastLog
	for i := range n {
		key := resourceKey(resource.KindTool, func() string {
			return "tool" + string(rune('a'+i))
		}())

		go func(k string) {
			defer wg.Done()
			cv.StartTask(k, resource.KindTool, "tool", "1.0", "download")
		}(key)

		go func(k string) {
			defer wg.Done()
			cv.AddOutput(k, "some output")
		}(key)

		go func(k string) {
			defer wg.Done()
			_ = cv.LastLog(k)
		}(key)
	}

	wg.Wait()

	// Concurrent CompleteTask + FailTask
	wg.Add(n)
	for i := range n {
		key := resourceKey(resource.KindTool, "tool"+string(rune('a'+i)))
		go func(k string) {
			defer wg.Done()
			cv.CompleteTask(k)
		}(key)
	}

	wg.Wait()
}
