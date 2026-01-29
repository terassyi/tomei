package path

import (
	"os"
	"path/filepath"
)

// Default path constants
const (
	DefaultSystemDataDir = "/var/lib/toto"
)

// Default path suffixes (relative to home directory)
const (
	defaultUserDataSuffix = ".local/share/toto"
	defaultUserBinSuffix  = ".local/bin"
)

// Paths holds configurable paths for toto.
type Paths struct {
	userDataDir   string
	userBinDir    string
	systemDataDir string
}

// Option is a functional option for configuring Paths.
type Option func(*Paths)

// WithUserDataDir sets a custom user data directory.
func WithUserDataDir(dir string) Option {
	return func(p *Paths) {
		p.userDataDir = dir
	}
}

// WithUserBinDir sets a custom user bin directory.
func WithUserBinDir(dir string) Option {
	return func(p *Paths) {
		p.userBinDir = dir
	}
}

// WithSystemDataDir sets a custom system data directory.
func WithSystemDataDir(dir string) Option {
	return func(p *Paths) {
		p.systemDataDir = dir
	}
}

// New creates a new Paths with optional custom configuration.
func New(opts ...Option) (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	p := &Paths{
		userDataDir:   filepath.Join(home, defaultUserDataSuffix),
		userBinDir:    filepath.Join(home, defaultUserBinSuffix),
		systemDataDir: DefaultSystemDataDir,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p, nil
}

// UserDataDir returns the user data directory.
func (p *Paths) UserDataDir() string {
	return p.userDataDir
}

// UserBinDir returns the user bin directory.
func (p *Paths) UserBinDir() string {
	return p.userBinDir
}

// SystemDataDir returns the system data directory.
func (p *Paths) SystemDataDir() string {
	return p.systemDataDir
}

// ToolInstallDir returns the installation directory for a tool.
// Returns <userDataDir>/tools/<name>/<version>
func (p *Paths) ToolInstallDir(name, version string) string {
	return filepath.Join(p.userDataDir, "tools", name, version)
}

// RuntimeInstallDir returns the installation directory for a runtime.
// Returns <userDataDir>/runtimes/<name>/<version>
func (p *Paths) RuntimeInstallDir(name, version string) string {
	return filepath.Join(p.userDataDir, "runtimes", name, version)
}

// UserStateFile returns the path to the user state file.
// Returns <userDataDir>/state.json
func (p *Paths) UserStateFile() string {
	return filepath.Join(p.userDataDir, "state.json")
}

// UserStateLockFile returns the path to the user state lock file.
// Returns <userDataDir>/state.lock
func (p *Paths) UserStateLockFile() string {
	return filepath.Join(p.userDataDir, "state.lock")
}

// SystemStateFile returns the path to the system state file.
// Returns <systemDataDir>/state.json
func (p *Paths) SystemStateFile() string {
	return filepath.Join(p.systemDataDir, "state.json")
}

// SystemStateLockFile returns the path to the system state lock file.
// Returns <systemDataDir>/state.lock
func (p *Paths) SystemStateLockFile() string {
	return filepath.Join(p.systemDataDir, "state.lock")
}

// EnsureDir creates a directory if it doesn't exist.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}
