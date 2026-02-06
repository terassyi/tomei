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

// progressManager manages progress display for downloads.
type progressManager struct {
	mu       sync.Mutex
	w        io.Writer
	isTTY    bool
	progress *mpb.Progress
	bars     map[string]*mpb.Bar
}

// newProgressManager creates a new progress manager.
func newProgressManager(w io.Writer) *progressManager {
	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

	pm := &progressManager{
		w:     w,
		isTTY: isTTY,
		bars:  make(map[string]*mpb.Bar),
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

// handleEvent handles engine events for progress display.
func (pm *progressManager) handleEvent(event engine.Event, results *applyResults) {
	style := newOutputStyle()
	key := barKey(event.Kind, event.Name)

	switch event.Type {
	case engine.EventStart:
		pm.handleStart(event, style, key)
	case engine.EventProgress:
		pm.handleProgress(event, key)
	case engine.EventComplete:
		pm.handleComplete(event, results, key)
	case engine.EventError:
		pm.handleError(event, results, style, key)
	}
}

// handleStart handles EventStart - creates progress bar or prints start line.
func (pm *progressManager) handleStart(event engine.Event, style *outputStyle, key string) {
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

// handleComplete handles EventComplete - completes progress bar and updates results.
func (pm *progressManager) handleComplete(event engine.Event, results *applyResults, key string) {
	if pm.isTTY {
		pm.mu.Lock()
		bar, exists := pm.bars[key]
		if exists {
			bar.SetTotal(bar.Current(), true)
			delete(pm.bars, key)
		}
		pm.mu.Unlock()
	}

	switch event.Action {
	case resource.ActionInstall, resource.ActionReinstall:
		results.installed++
	case resource.ActionUpgrade:
		results.upgraded++
	case resource.ActionRemove:
		results.removed++
	}
}

// handleError handles EventError - aborts progress bar and prints error.
func (pm *progressManager) handleError(event engine.Event, results *applyResults, style *outputStyle, key string) {
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
