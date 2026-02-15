package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"github.com/terassyi/tomei/internal/installer/engine"
	tomeilog "github.com/terassyi/tomei/internal/log"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// spinnerFrames are the frames used for the delegation spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ApplyResults tracks apply operation results.
type ApplyResults struct {
	Installed   int
	Upgraded    int
	Reinstalled int
	Removed     int
	Failed      int
}

// ProgressManager manages progress display for downloads and commands.
type ProgressManager struct {
	mu                  sync.Mutex
	w                   io.Writer
	isTTY               bool
	progress            *mpb.Progress
	bars                map[string]*mpb.Bar
	cmdView             *CommandView
	downloadHeaderShown bool
}

// NewProgressManager creates a new progress manager.
func NewProgressManager(w io.Writer) *ProgressManager {
	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

	pm := &ProgressManager{
		w:       w,
		isTTY:   isTTY,
		bars:    make(map[string]*mpb.Bar),
		cmdView: NewCommandView(w),
	}

	if isTTY {
		pm.progress = mpb.New(mpb.WithOutput(w), mpb.WithWidth(40))
	}

	return pm
}

// Wait waits for all progress to complete.
func (pm *ProgressManager) Wait() {
	if pm.progress != nil {
		pm.progress.Wait()
	}
}

// HandleEvent handles engine events for progress display.
func (pm *ProgressManager) HandleEvent(event engine.Event, results *ApplyResults) {
	key := resourceKey(event.Kind, event.Name)
	isDownload := isDownloadMethod(event.Method)

	switch event.Type {
	case engine.EventStart:
		if isDownload {
			pm.handleDownloadStart(event, key)
		} else {
			pm.handleCommandStart(event, key)
		}
	case engine.EventProgress:
		pm.handleProgress(event, key)
	case engine.EventOutput:
		pm.handleOutput(event, key)
	case engine.EventComplete:
		pm.handleComplete(event, results, key, isDownload)
	case engine.EventError:
		pm.handleError(event, results, key, isDownload)
	}
}

// resourceKey returns a unique key for a resource.
func resourceKey(kind resource.Kind, name string) string {
	return fmt.Sprintf("%s/%s", kind, name)
}

// isDownloadMethod returns true if the method is a download pattern.
func isDownloadMethod(method string) bool {
	return method == "" || method == "download"
}

// handleDownloadStart handles EventStart for download pattern.
func (pm *ProgressManager) handleDownloadStart(event engine.Event, key string) {
	style := NewStyle()

	pm.mu.Lock()
	showHeader := !pm.downloadHeaderShown && !pm.isTTY
	pm.downloadHeaderShown = true

	if pm.isTTY {
		pm.bars[key] = pm.progress.AddBar(0,
			mpb.BarFillerClearOnComplete(),
			mpb.PrependDecorators(
				decor.Name(fmt.Sprintf("  %s %s/%s ",
					style.ActionIcon(event.Action), event.Kind, style.Path.Sprint(event.Name)),
					decor.WC{W: 30, C: decor.DindentRight}),
				decor.Name(event.Version, decor.WC{W: 12}),
			),
			mpb.AppendDecorators(
				decor.CountersKibiByte("% .1f / % .1f"),
				decor.OnComplete(decor.Name(""), " done"),
			),
		)
		pm.mu.Unlock()
	} else {
		if showHeader {
			fmt.Fprintln(pm.w)
			fmt.Fprintln(pm.w, "Downloads:")
		}
		fmt.Fprintf(pm.w, "  %s %s/%s %s\n",
			style.ActionIcon(event.Action), event.Kind, style.Path.Sprint(event.Name), event.Version)
		pm.mu.Unlock()
	}
}

// handleCommandStart handles EventStart for delegation pattern.
func (pm *ProgressManager) handleCommandStart(event engine.Event, key string) {
	pm.cmdView.StartTask(key, event.Kind, event.Name, event.Version, event.Method)

	if pm.isTTY {
		style := NewStyle()
		label := fmt.Sprintf(" => %s/%s %s (%s) ",
			event.Kind, style.Path.Sprint(event.Name), event.Version, event.Method)

		bar, _ := pm.progress.Add(0,
			mpb.SpinnerStyle(spinnerFrames...).Build(),
			mpb.BarFillerClearOnComplete(),
			mpb.PrependDecorators(
				decor.Name(label, decor.WC{W: 40, C: decor.DindentRight}),
			),
			mpb.AppendDecorators(
				decor.Any(func(s decor.Statistics) string {
					return truncateLine(pm.cmdView.LastLog(key), 50)
				}),
				decor.Elapsed(decor.ET_STYLE_GO, decor.WC{W: 8}),
				decor.OnComplete(decor.Name(""), " done"),
			),
		)

		pm.mu.Lock()
		pm.bars[key] = bar
		pm.mu.Unlock()
	} else {
		pm.mu.Lock()
		pm.cmdView.PrintTaskStart(key)
		pm.mu.Unlock()
	}
}

// handleProgress handles EventProgress.
func (pm *ProgressManager) handleProgress(event engine.Event, key string) {
	if !pm.isTTY {
		return
	}

	pm.mu.Lock()
	bar, ok := pm.bars[key]
	pm.mu.Unlock()

	if ok {
		if event.Total > 0 {
			bar.SetTotal(event.Total, false)
		}
		bar.SetCurrent(event.Downloaded)
	}
}

// handleOutput handles EventOutput.
func (pm *ProgressManager) handleOutput(event engine.Event, key string) {
	pm.cmdView.AddOutput(key, event.Output)
	if !pm.isTTY {
		pm.mu.Lock()
		pm.cmdView.PrintOutput(event.Output)
		pm.mu.Unlock()
	}
	// TTY: the spinner bar's decor.Any callback reads LastLog dynamically via cmdView
}

// handleComplete handles EventComplete.
func (pm *ProgressManager) handleComplete(event engine.Event, results *ApplyResults, key string, isDownload bool) {
	if isDownload {
		if pm.isTTY {
			pm.mu.Lock()
			if bar, ok := pm.bars[key]; ok {
				bar.SetTotal(bar.Current(), true)
				delete(pm.bars, key)
			}
			pm.mu.Unlock()
		}
	} else {
		pm.cmdView.CompleteTask(key)
		if pm.isTTY {
			pm.mu.Lock()
			if bar, ok := pm.bars[key]; ok {
				bar.SetTotal(1, true)
				bar.SetCurrent(1)
				delete(pm.bars, key)
			}
			pm.mu.Unlock()
		} else {
			pm.mu.Lock()
			pm.cmdView.PrintTaskComplete(key)
			pm.mu.Unlock()
		}
	}

	pm.mu.Lock()
	updateResults(event.Action, event.Phase, results)
	pm.mu.Unlock()
}

// handleError handles EventError.
func (pm *ProgressManager) handleError(event engine.Event, results *ApplyResults, key string, isDownload bool) {
	style := NewStyle()

	if isDownload {
		pm.mu.Lock()
		if pm.isTTY {
			if bar, ok := pm.bars[key]; ok {
				bar.Abort(true)
				delete(pm.bars, key)
			}
		}
		fmt.Fprintf(pm.w, "  %s %s/%s failed: %v\n",
			style.FailMark, event.Kind, event.Name, event.Error)
		pm.mu.Unlock()
	} else {
		pm.cmdView.FailTask(key, event.Error)
		pm.mu.Lock()
		if pm.isTTY {
			if bar, ok := pm.bars[key]; ok {
				bar.Abort(true)
				delete(pm.bars, key)
			}
		} else {
			pm.cmdView.PrintTaskComplete(key)
		}
		pm.mu.Unlock()
	}

	pm.mu.Lock()
	results.Failed++
	pm.mu.Unlock()
}

// updateResults updates the results based on action type and execution phase.
// When the phase is PhaseTaint, actions are counted as reinstalls regardless
// of the reconciler-reported action type (which is typically ActionUpgrade).
func updateResults(action resource.ActionType, phase engine.Phase, results *ApplyResults) {
	if phase == engine.PhaseTaint {
		results.Reinstalled++
		return
	}
	switch action {
	case resource.ActionInstall:
		results.Installed++
	case resource.ActionReinstall:
		results.Reinstalled++
	case resource.ActionUpgrade:
		results.Upgraded++
	case resource.ActionRemove:
		results.Removed++
	}
}

// PrintApplySummary prints the apply summary.
func PrintApplySummary(w io.Writer, results *ApplyResults) {
	style := NewStyle()

	total := results.Installed + results.Upgraded + results.Reinstalled + results.Removed
	if total == 0 && results.Failed == 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%s No changes to apply\n", style.SuccessMark)
		return
	}

	fmt.Fprintln(w)
	style.Header.Fprintln(w, "Summary:")

	if results.Installed > 0 {
		fmt.Fprintf(w, "  %s Installed:   %d\n", style.SuccessMark, results.Installed)
	}
	if results.Upgraded > 0 {
		fmt.Fprintf(w, "  %s Upgraded:    %d\n", style.UpgradeMark, results.Upgraded)
	}
	if results.Reinstalled > 0 {
		fmt.Fprintf(w, "  %s Reinstalled: %d\n", style.ReinstallMark, results.Reinstalled)
	}
	if results.Removed > 0 {
		fmt.Fprintf(w, "  %s Removed:     %d\n", style.RemoveMark, results.Removed)
	}
	if results.Failed > 0 {
		fmt.Fprintf(w, "  %s Failed:      %d\n", style.FailMark, results.Failed)
	}

	fmt.Fprintln(w)
	if results.Failed == 0 {
		style.Success.Fprintln(w, "Apply complete!")
	} else {
		color.New(color.FgRed, color.Bold).Fprintln(w, "Apply completed with errors")
	}
}

// maxFailureLogLines is the maximum number of output lines to display per failed resource.
const maxFailureLogLines = 20

// PrintFailureLogs prints detailed failure logs for failed resources.
func PrintFailureLogs(w io.Writer, failed []tomeilog.FailedResource) {
	if len(failed) == 0 {
		return
	}

	style := NewStyle()

	fmt.Fprintln(w)
	style.Header.Fprintln(w, "Failure Details:")

	for _, f := range failed {
		fmt.Fprintf(w, "\n  %s %s/%s: %v\n", style.FailMark, f.Kind, f.Name, f.Error)

		if f.Output == "" {
			continue
		}

		lines := strings.Split(strings.TrimRight(f.Output, "\n"), "\n")
		totalLines := len(lines)

		if totalLines > maxFailureLogLines {
			// Show last N lines
			lines = lines[totalLines-maxFailureLogLines:]
			fmt.Fprintf(w, "    ... (%d lines omitted, see: tomei logs %s/%s)\n", totalLines-maxFailureLogLines, f.Kind, f.Name)
		}
		for _, line := range lines {
			fmt.Fprintf(w, "    %s\n", line)
		}
	}
}
