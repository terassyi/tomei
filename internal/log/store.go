package log

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/terassyi/tomei/internal/resource"
)

// FailedResource holds log information for a failed resource.
type FailedResource struct {
	Kind    resource.Kind
	Name    string
	Version string
	Action  string
	Method  string
	Error   error
	Output  string // all accumulated output lines joined
}

// resourceMeta holds metadata about a resource being tracked.
type resourceMeta struct {
	kind    resource.Kind
	name    string
	version string
	action  string
	method  string
}

// Store accumulates installation output per resource and persists logs for failed resources.
type Store struct {
	baseDir    string
	sessionID  string
	sessionDir string
	mu         sync.Mutex
	buffers    map[string]*bytes.Buffer
	metadata   map[string]*resourceMeta
	failed     map[string]error
}

// NewStore creates a new Store with a new session under baseDir.
func NewStore(baseDir string) (*Store, error) {
	sessionID := time.Now().Format("20060102T150405")
	sessionDir := filepath.Join(baseDir, sessionID)

	return &Store{
		baseDir:    baseDir,
		sessionID:  sessionID,
		sessionDir: sessionDir,
		buffers:    make(map[string]*bytes.Buffer),
		metadata:   make(map[string]*resourceMeta),
		failed:     make(map[string]error),
	}, nil
}

// resourceKey returns a unique key for a resource.
func resourceKey(kind resource.Kind, name string) string {
	return string(kind) + "/" + name
}

// RecordStart records the start of an action for a resource.
func (s *Store) RecordStart(kind resource.Kind, name, version, action, method string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := resourceKey(kind, name)
	s.buffers[key] = &bytes.Buffer{}
	s.metadata[key] = &resourceMeta{
		kind:    kind,
		name:    name,
		version: version,
		action:  action,
		method:  method,
	}
}

// RecordOutput appends an output line for a resource.
func (s *Store) RecordOutput(kind resource.Kind, name, line string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := resourceKey(kind, name)
	if buf, ok := s.buffers[key]; ok {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
}

// RecordError marks a resource as failed.
func (s *Store) RecordError(kind resource.Kind, name string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := resourceKey(kind, name)
	s.failed[key] = err
}

// RecordComplete marks a resource as successfully completed, discarding its buffer.
func (s *Store) RecordComplete(kind resource.Kind, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := resourceKey(kind, name)
	delete(s.buffers, key)
	delete(s.metadata, key)
}

// FailedResources returns information about all failed resources.
func (s *Store) FailedResources() []FailedResource {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []FailedResource
	for key, err := range s.failed {
		meta := s.metadata[key]
		if meta == nil {
			continue
		}

		output := ""
		if buf, ok := s.buffers[key]; ok {
			output = buf.String()
		}

		result = append(result, FailedResource{
			Kind:    meta.kind,
			Name:    meta.name,
			Version: meta.version,
			Action:  meta.action,
			Method:  meta.method,
			Error:   err,
			Output:  output,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Name < result[j].Name
	})

	return result
}

// Flush writes log files for all failed resources to disk.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.failed) == 0 {
		return nil
	}

	if err := os.MkdirAll(s.sessionDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	var errs []error
	for key, failErr := range s.failed {
		meta := s.metadata[key]
		if meta == nil {
			continue
		}

		output := ""
		if buf, ok := s.buffers[key]; ok {
			output = buf.String()
		}

		content := buildLogContent(meta, failErr, output)
		filename := fmt.Sprintf("%s_%s.log", meta.kind, meta.name)
		logPath := filepath.Join(s.sessionDir, filename)

		if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
			errs = append(errs, fmt.Errorf("failed to write log for %s: %w", key, err))
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// SessionDir returns the path to the current session directory.
func (s *Store) SessionDir() string {
	return s.sessionDir
}

// Cleanup removes old session directories, keeping the most recent keepSessions.
func (s *Store) Cleanup(keepSessions int) error {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read logs directory: %w", err)
	}

	// Filter to directories only
	var dirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e)
		}
	}

	if len(dirs) <= keepSessions {
		return nil
	}

	// Sort by name (timestamp format ensures chronological order)
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].Name() < dirs[j].Name()
	})

	// Remove oldest entries
	toRemove := dirs[:len(dirs)-keepSessions]
	for _, d := range toRemove {
		dirPath := filepath.Join(s.baseDir, d.Name())
		if err := os.RemoveAll(dirPath); err != nil {
			return fmt.Errorf("failed to remove old session %s: %w", d.Name(), err)
		}
	}

	return nil
}

// buildLogContent creates the log file content with a header.
func buildLogContent(meta *resourceMeta, err error, output string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# tomei installation log")
	fmt.Fprintf(&b, "# Resource: %s/%s\n", meta.kind, meta.name)
	fmt.Fprintf(&b, "# Version: %s\n", meta.version)
	fmt.Fprintf(&b, "# Action: %s\n", meta.action)
	if meta.method != "" {
		fmt.Fprintf(&b, "# Method: %s\n", meta.method)
	}
	fmt.Fprintf(&b, "# Timestamp: %s\n", time.Now().Format(time.RFC3339))
	if err != nil {
		fmt.Fprintf(&b, "# Error: %v\n", err)
	}
	b.WriteByte('\n')
	b.WriteString(output)
	return b.String()
}
