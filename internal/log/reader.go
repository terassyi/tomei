package log

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/terassyi/tomei/internal/resource"
)

// SessionInfo holds information about a log session.
type SessionInfo struct {
	ID        string
	Timestamp time.Time
	Dir       string
}

// ResourceLog holds the content of a single resource log file.
type ResourceLog struct {
	Kind    resource.Kind
	Name    string
	Content string
}

// ListSessions returns all sessions in the logs directory, sorted newest first.
func ListSessions(baseDir string) ([]SessionInfo, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read logs directory: %w", err)
	}

	var sessions []SessionInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := time.Parse("20060102T150405", e.Name())
		if err != nil {
			continue // skip non-session directories
		}
		sessions = append(sessions, SessionInfo{
			ID:        e.Name(),
			Timestamp: t,
			Dir:       filepath.Join(baseDir, e.Name()),
		})
	}

	// Sort newest first
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Timestamp.After(sessions[j].Timestamp)
	})

	return sessions, nil
}

// ReadSessionLogs reads all log files from a session directory.
func ReadSessionLogs(sessionDir string) ([]ResourceLog, error) {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read session directory: %w", err)
	}

	var logs []ResourceLog
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}

		kind, name, ok := parseLogFilename(e.Name())
		if !ok {
			continue
		}

		content, err := os.ReadFile(filepath.Join(sessionDir, e.Name()))
		if err != nil {
			continue
		}

		logs = append(logs, ResourceLog{
			Kind:    resource.Kind(kind),
			Name:    name,
			Content: string(content),
		})
	}

	sort.Slice(logs, func(i, j int) bool {
		if logs[i].Kind != logs[j].Kind {
			return logs[i].Kind < logs[j].Kind
		}
		return logs[i].Name < logs[j].Name
	})

	return logs, nil
}

// ReadResourceLog reads a specific resource's log from a session directory.
func ReadResourceLog(sessionDir string, kind resource.Kind, name string) (string, error) {
	filename := fmt.Sprintf("%s_%s.log", kind, name)
	logPath := filepath.Join(sessionDir, filename)

	content, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no log found for %s/%s", kind, name)
		}
		return "", fmt.Errorf("failed to read log file: %w", err)
	}

	return string(content), nil
}

// parseLogFilename parses a log filename like "Tool_ripgrep.log" into kind and name.
func parseLogFilename(filename string) (kind, name string, ok bool) {
	base := strings.TrimSuffix(filename, ".log")
	kind, name, ok = strings.Cut(base, "_")
	if !ok || kind == "" || name == "" {
		return "", "", false
	}
	return kind, name, true
}
