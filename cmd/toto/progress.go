package main

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"github.com/terassyi/toto/internal/installer/engine"
	"github.com/terassyi/toto/internal/resource"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// applyResults tracks apply operation results.
type applyResults struct {
	installed int
	upgraded  int
	removed   int
	failed    int
}

// progressManager manages progress display for downloads and commands.
type progressManager struct {
	mu       sync.Mutex
	w        io.Writer
	isTTY    bool
	progress *mpb.Progress
	bars     map[string]*mpb.Bar

	// Commands section (for delegation pattern)
	cmdView             *CommandView
	downloadHeaderShown bool
}

// newProgressManager creates a new progress manager.
func newProgressManager(w io.Writer) *progressManager {
	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

	pm := &progressManager{
		w:       w,
		isTTY:   isTTY,
		bars:    make(map[string]*mpb.Bar),
		cmdView: NewCommandView(w, isTTY),
	}

	if isTTY {
		pm.progress = mpb.New(
			mpb.WithOutput(w),
			mpb.WithWidth(40),
		)
	}

	return pm
}

// Wait waits for all progress to complete.
func (pm *progressManager) Wait() {
	if pm.progress != nil {
		pm.progress.Wait()
	}
}

// barKey returns a unique key for a resource.
func barKey(kind resource.Kind, name string) string {
	return fmt.Sprintf("%s/%s", kind, name)
}

// isDownloadMethod returns true if the method is a download pattern.
func isDownloadMethod(method string) bool {
	return method == "" || method == "download"
}

// handleEvent handles engine events for progress display.
func (pm *progressManager) handleEvent(event engine.Event, results *applyResults) {
	style := newOutputStyle()
	key := barKey(event.Kind, event.Name)

	switch event.Type {
	case engine.EventStart:
		if isDownloadMethod(event.Method) {
			pm.handleDownloadStart(event, style, key)
		} else {
			pm.handleCommandStart(event, key)
		}
	case engine.EventProgress:
		pm.handleProgress(event, key)
	case engine.EventOutput:
		pm.handleOutput(event, key)
	case engine.EventComplete:
		if isDownloadMethod(event.Method) {
			pm.handleDownloadComplete(event, results, key)
		} else {
			pm.handleCommandComplete(event, results, key)
		}
	case engine.EventError:
		if isDownloadMethod(event.Method) {
			pm.handleDownloadError(event, results, style, key)
		} else {
			pm.handleCommandError(event, results, key)
		}
	}
}

// handleDownloadStart handles EventStart for download pattern.
func (pm *progressManager) handleDownloadStart(event engine.Event, style *outputStyle, key string) {
	// Show Downloads header if needed
	if !pm.downloadHeaderShown {
		if pm.isTTY {
			// mpb handles its own output
		} else {
			fmt.Fprintln(pm.w)
			fmt.Fprintln(pm.w, "Downloads:")
		}
		pm.downloadHeaderShown = true
	}

	if pm.isTTY {
		pm.mu.Lock()
		bar := pm.progress.AddBar(0,
			mpb.BarFillerClearOnComplete(),
			mpb.PrependDecorators(
				decor.Name(fmt.Sprintf("  %s %s/%s ",
					style.actionIcon(event.Action),
					event.Kind,
					style.path.Sprint(event.Name)),
					decor.WC{W: 30, C: decor.DindentRight}),
				decor.Name(event.Version, decor.WC{W: 12}),
			),
			mpb.AppendDecorators(
				decor.CountersKibiByte("% .1f / % .1f"),
				decor.OnComplete(decor.Name(""), " done"),
			),
		)
		pm.bars[key] = bar
		pm.mu.Unlock()
	} else {
		fmt.Fprintf(pm.w, "  %s %s/%s %s\n",
			style.actionIcon(event.Action),
			event.Kind,
			style.path.Sprint(event.Name),
			event.Version)
	}
}

// handleCommandStart handles EventStart for delegation pattern.
func (pm *progressManager) handleCommandStart(event engine.Event, key string) {
	pm.cmdView.StartTask(key, event.Kind, event.Name, event.Version, event.Method)
	if !pm.isTTY {
		pm.cmdView.PrintTaskStart(key)
	}
}

// handleProgress handles EventProgress - updates progress bar.
func (pm *progressManager) handleProgress(event engine.Event, key string) {
	if !pm.isTTY {
		return
	}

	pm.mu.Lock()
	bar, exists := pm.bars[key]
	pm.mu.Unlock()

	if exists {
		if event.Total > 0 {
			bar.SetTotal(event.Total, false)
		}
		bar.SetCurrent(event.Downloaded)
	}
}

// handleOutput handles EventOutput - adds command output line.
func (pm *progressManager) handleOutput(event engine.Event, key string) {
	pm.cmdView.AddOutput(key, event.Output)
	if !pm.isTTY {
		pm.cmdView.PrintOutput(event.Output)
	}
}

// handleDownloadComplete handles EventComplete for download pattern.
func (pm *progressManager) handleDownloadComplete(event engine.Event, results *applyResults, key string) {
	if pm.isTTY {
		pm.mu.Lock()
		bar, exists := pm.bars[key]
		if exists {
			bar.SetTotal(bar.Current(), true)
			delete(pm.bars, key)
		}
		pm.mu.Unlock()
	}

	updateResults(event.Action, results)
}

// handleCommandComplete handles EventComplete for delegation pattern.
func (pm *progressManager) handleCommandComplete(event engine.Event, results *applyResults, key string) {
	pm.cmdView.CompleteTask(key)
	if !pm.isTTY {
		pm.cmdView.PrintTaskComplete(key)
	}

	updateResults(event.Action, results)
}

// handleDownloadError handles EventError for download pattern.
func (pm *progressManager) handleDownloadError(event engine.Event, results *applyResults, style *outputStyle, key string) {
	if pm.isTTY {
		pm.mu.Lock()
		bar, exists := pm.bars[key]
		if exists {
			bar.Abort(true)
			delete(pm.bars, key)
		}
		pm.mu.Unlock()
	}

	results.failed++
	fmt.Fprintf(pm.w, "  %s %s/%s failed: %v\n",
		style.failMark,
		event.Kind,
		event.Name,
		event.Error)
}

// handleCommandError handles EventError for delegation pattern.
func (pm *progressManager) handleCommandError(event engine.Event, results *applyResults, key string) {
	pm.cmdView.FailTask(key, event.Error)
	if !pm.isTTY {
		pm.cmdView.PrintTaskComplete(key)
	}

	results.failed++
}

// updateResults updates the results based on action type.
func updateResults(action resource.ActionType, results *applyResults) {
	switch action {
	case resource.ActionInstall, resource.ActionReinstall:
		results.installed++
	case resource.ActionUpgrade:
		results.upgraded++
	case resource.ActionRemove:
		results.removed++
	}
}

// printApplySummary prints the apply summary.
func printApplySummary(w io.Writer, results *applyResults) {
	style := newOutputStyle()

	total := results.installed + results.upgraded + results.removed
	if total == 0 && results.failed == 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%s No changes to apply\n", style.successMark)
		return
	}

	fmt.Fprintln(w)
	style.header.Fprintln(w, "Summary:")

	if results.installed > 0 {
		fmt.Fprintf(w, "  %s Installed: %d\n", style.successMark, results.installed)
	}
	if results.upgraded > 0 {
		fmt.Fprintf(w, "  %s Upgraded:  %d\n", style.upgradeMark, results.upgraded)
	}
	if results.removed > 0 {
		fmt.Fprintf(w, "  %s Removed:   %d\n", style.removeMark, results.removed)
	}
	if results.failed > 0 {
		fmt.Fprintf(w, "  %s Failed:    %d\n", style.failMark, results.failed)
	}

	fmt.Fprintln(w)
	if results.failed == 0 {
		style.success.Fprintln(w, "Apply complete!")
	} else {
		color.New(color.FgRed, color.Bold).Fprintln(w, "Apply completed with errors")
	}
}
