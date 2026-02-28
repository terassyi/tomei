package tool

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/executor"
	"github.com/terassyi/tomei/internal/installer/place"
	"github.com/terassyi/tomei/internal/installer/resolve"
	"github.com/terassyi/tomei/internal/registry/aqua"
	"github.com/terassyi/tomei/internal/resource"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()
	downloader := download.NewDownloader()
	placer := place.NewPlacer("/tools", "/bin")

	installer := NewInstaller(downloader, placer)
	assert.NotNil(t, installer)
}

func TestToolInstaller_Install(t *testing.T) {
	t.Parallel()
	binaryContent := []byte("#!/bin/sh\necho hello")
	tarGzContent := createTarGzContent(t, "ripgrep", binaryContent)
	archiveHash := sha256Hash(tarGzContent)

	tests := []struct {
		name       string
		tool       *resource.Tool
		wantErr    bool
		errContain string
	}{
		{
			name: "successful install",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{
					Metadata: resource.Metadata{Name: "ripgrep"},
				},
				ToolSpec: &resource.ToolSpec{
					InstallerRef: "download",
					Version:      "14.1.1",
					Source: &resource.DownloadSource{
						URL: "https://example.com/ripgrep.tar.gz",
						Checksum: &resource.Checksum{
							Value: "sha256:" + archiveHash,
						},
						ArchiveType: "tar.gz",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing source",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{
					Metadata: resource.Metadata{Name: "mytool"},
				},
				ToolSpec: &resource.ToolSpec{
					InstallerRef: "download",
					Version:      "1.0.0",
					Source:       nil,
				},
			},
			wantErr:    true,
			errContain: "source is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dl := &mockDownloader{archiveData: tarGzContent}
			installer := NewInstaller(dl, &mockPlacer{})

			state, err := installer.Install(context.Background(), tt.tool, tt.tool.Name())

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, state)
			assert.Equal(t, tt.tool.ToolSpec.InstallerRef, state.InstallerRef)
			assert.Equal(t, tt.tool.ToolSpec.Version, state.Version)
			assert.NotEmpty(t, state.InstallPath)
			assert.NotEmpty(t, state.BinPath)
		})
	}
}

func TestToolInstaller_Install_Skip(t *testing.T) {
	t.Parallel()
	binaryContent := []byte("#!/bin/sh\necho hello")

	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	binDir := filepath.Join(tmpDir, "bin")

	// Pre-install the binary
	installDir := filepath.Join(toolsDir, "ripgrep", "14.1.1")
	err := os.MkdirAll(installDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(installDir, "ripgrep"), binaryContent, 0755)
	require.NoError(t, err)

	downloader := download.NewDownloader()
	placer := place.NewPlacer(toolsDir, binDir)
	inst := NewInstaller(downloader, placer)

	// No checksum - will skip if binary exists
	tool := &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "ripgrep"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "download",
			Version:      "14.1.1",
			Source: &resource.DownloadSource{
				URL:         "https://example.com/ripgrep.tar.gz",
				Checksum:    nil, // No checksum = existence check only
				ArchiveType: "tar.gz",
			},
		},
	}

	state, err := inst.Install(context.Background(), tool, "ripgrep")

	require.NoError(t, err)
	assert.NotNil(t, state)
	// Should still return valid state even if skipped
	assert.Equal(t, tool.ToolSpec.Version, state.Version)
}

func TestToolInstaller_InstallFromRegistry_NoResolver(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	binDir := filepath.Join(tmpDir, "bin")

	downloader := download.NewDownloader()
	placer := place.NewPlacer(toolsDir, binDir)
	installer := NewInstaller(downloader, placer)

	// No resolver set - should fail

	tool := &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "mytool"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "1.0.0",
			Package: &resource.Package{
				Owner: "test",
				Repo:  "mytool",
			},
		},
	}

	_, err := installer.Install(context.Background(), tool, "mytool")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolver not configured")
}

func TestToolInstaller_InstallFromRegistry_NoRegistryRef(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	binDir := filepath.Join(tmpDir, "bin")
	cacheDir := filepath.Join(tmpDir, "cache")

	downloader := download.NewDownloader()
	placer := place.NewPlacer(toolsDir, binDir)
	installer := NewInstaller(downloader, placer)

	// Set resolver but no registry ref
	resolver := createMockResolver(t, cacheDir, "http://example.com")
	installer.SetResolver(resolver, "") // empty ref

	tool := &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "mytool"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "1.0.0",
			Package: &resource.Package{
				Owner: "test",
				Repo:  "mytool",
			},
		},
	}

	_, err := installer.Install(context.Background(), tool, "mytool")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ref not configured")
}

// Helper functions

func createTarGzContent(t *testing.T, binaryName string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: binaryName,
		Mode: 0755,
		Size: int64(len(content)),
	}
	err := tw.WriteHeader(hdr)
	require.NoError(t, err)
	_, err = tw.Write(content)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return buf.Bytes()
}

func sha256Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// createMockResolver creates a resolver with the given base URL for testing.
func createMockResolver(t *testing.T, cacheDir, baseURL string) *aqua.Resolver {
	t.Helper()
	return aqua.NewResolverWithBaseURL(cacheDir, baseURL)
}

// mockDownloader records whether a progress callback was passed to DownloadWithProgress.
// It writes a valid tar.gz archive to destPath so that subsequent extraction succeeds.
type mockDownloader struct {
	archiveData          []byte
	lastProgressCallback download.ProgressCallback
	lastVerifyChecksum   *resource.Checksum
}

func (m *mockDownloader) Download(_ context.Context, _, destPath string) (string, error) {
	if err := os.WriteFile(destPath, m.archiveData, 0644); err != nil {
		return "", err
	}
	return destPath, nil
}

func (m *mockDownloader) DownloadWithProgress(_ context.Context, _, destPath string, callback download.ProgressCallback) (string, error) {
	m.lastProgressCallback = callback
	if callback != nil {
		callback(100, 200) // trigger to verify which callback was called
	}
	if err := os.WriteFile(destPath, m.archiveData, 0644); err != nil {
		return "", err
	}
	return destPath, nil
}

func (m *mockDownloader) Verify(_ context.Context, _ string, cs *resource.Checksum) error {
	m.lastVerifyChecksum = cs
	return nil
}

// mockPlacer always returns install action and succeeds.
type mockPlacer struct{}

func (m *mockPlacer) BinaryPath(target place.Target) string {
	return "/tools/" + target.Name + "/" + target.Version + "/" + target.BinaryName
}

func (m *mockPlacer) LinkPath(target place.Target) string {
	return "/bin/" + target.BinaryName
}

func (m *mockPlacer) Validate(_ place.Target, _ string) (place.ValidateAction, error) {
	return place.ValidateActionInstall, nil
}

func (m *mockPlacer) Place(_ string, target place.Target) (*place.Result, error) {
	return &place.Result{
		BinaryPath: "/tools/" + target.Name + "/" + target.Version + "/" + target.BinaryName,
	}, nil
}

func (m *mockPlacer) Symlink(target place.Target) (string, error) {
	return "/bin/" + target.BinaryName, nil
}

func (m *mockPlacer) Cleanup(_ string) error {
	return nil
}

func TestToolInstaller_Install_Args(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func(inst *Installer, captureFile string)
		tool     func(captureFile string) *resource.Tool
		wantArgs string
	}{
		{
			name: "runtime delegation passes args",
			setup: func(inst *Installer, captureFile string) {
				inst.RegisterRuntime("uv", &RuntimeInfo{
					BinDir:      "/usr/local/bin",
					ToolBinPath: filepath.Dir(captureFile),
					Commands: &resource.CommandsSpec{
						Install: []string{"echo {{.Package}}=={{.Version}} {{.Args}} > " + captureFile},
					},
				})
			},
			tool: func(_ string) *resource.Tool {
				return &resource.Tool{
					BaseResource: resource.BaseResource{
						Metadata: resource.Metadata{Name: "ansible"},
					},
					ToolSpec: &resource.ToolSpec{
						RuntimeRef: "uv",
						Version:    "13.3.0",
						Package:    &resource.Package{Name: "ansible"},
						Args:       []string{"--with-executables-from", "ansible-core"},
					},
				}
			},
			wantArgs: "ansible==13.3.0 --with-executables-from ansible-core",
		},
		{
			name: "installer delegation passes args",
			setup: func(inst *Installer, captureFile string) {
				inst.RegisterInstaller("uv", &InstallerInfo{
					Type: resource.InstallTypeDelegation,
					Commands: &resource.CommandsSpec{
						Install: []string{"echo {{.Package}}=={{.Version}} {{.Args}} > " + captureFile},
					},
				})
			},
			tool: func(_ string) *resource.Tool {
				return &resource.Tool{
					BaseResource: resource.BaseResource{
						Metadata: resource.Metadata{Name: "ansible"},
					},
					ToolSpec: &resource.ToolSpec{
						InstallerRef: "uv",
						Version:      "13.3.0",
						Package:      &resource.Package{Name: "ansible"},
						Args:         []string{"--with-executables-from", "ansible-core"},
					},
				}
			},
			wantArgs: "ansible==13.3.0 --with-executables-from ansible-core",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			captureFile := filepath.Join(tmpDir, "captured_cmd.txt")

			inst := NewInstaller(download.NewDownloader(), place.NewPlacer(filepath.Join(tmpDir, "tools"), filepath.Join(tmpDir, "bin")))
			tt.setup(inst, captureFile)

			tool := tt.tool(captureFile)
			state, err := inst.Install(context.Background(), tool, tool.Name())
			require.NoError(t, err)
			require.NotNil(t, state)

			content, err := os.ReadFile(captureFile)
			require.NoError(t, err)
			assert.Equal(t, tt.wantArgs, strings.TrimSpace(string(content)))
		})
	}
}

func TestToolInstaller_ProgressCallback_Priority(t *testing.T) {
	t.Parallel()
	archiveData := createTarGzContent(t, "mytool", []byte("#!/bin/sh\necho hello"))

	makeTool := func() *resource.Tool {
		return &resource.Tool{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "mytool"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "1.0.0",
				Source: &resource.DownloadSource{
					URL:         "https://example.com/mytool.tar.gz",
					Checksum:    &resource.Checksum{Value: "sha256:dummy"},
					ArchiveType: "tar.gz",
				},
			},
		}
	}

	tests := []struct {
		name          string
		fieldCallback bool
		ctxCallback   bool
		wantField     bool
		wantCtx       bool
	}{
		{
			name:          "context callback preferred over field",
			fieldCallback: true,
			ctxCallback:   true,
			wantField:     false,
			wantCtx:       true,
		},
		{
			name:          "field callback used when no context callback",
			fieldCallback: true,
			ctxCallback:   false,
			wantField:     true,
			wantCtx:       false,
		},
		{
			name:          "no callback - nil passed to downloader",
			fieldCallback: false,
			ctxCallback:   false,
			wantField:     false,
			wantCtx:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dl := &mockDownloader{archiveData: archiveData}
			installer := NewInstaller(dl, &mockPlacer{})

			var fieldCalled, ctxCalled bool

			if tt.fieldCallback {
				installer.SetProgressCallback(func(_, _ int64) { fieldCalled = true })
			}

			ctx := context.Background()
			if tt.ctxCallback {
				ctx = download.WithCallback(ctx, download.ProgressCallback(func(_, _ int64) { ctxCalled = true }))
			}

			_, err := installer.Install(ctx, makeTool(), "mytool")
			require.NoError(t, err)

			assert.Equal(t, tt.wantField, fieldCalled, "field callback")
			assert.Equal(t, tt.wantCtx, ctxCalled, "context callback")

			if !tt.fieldCallback && !tt.ctxCallback {
				assert.Nil(t, dl.lastProgressCallback, "callback should be nil")
			}
		})
	}
}

// --- Commands pattern tests ---

// mockCommandRunner records commands executed for verification.
type mockCommandRunner struct {
	executedCmds [][]string
	executedVars []command.Vars
	methods      []string // "Execute", "ExecuteWithEnv", "ExecuteWithOutput", "Check"
	checkedCmds  [][]string
	checkResult  bool
	executeErr   error
}

func (m *mockCommandRunner) Execute(_ context.Context, cmds []string, vars command.Vars) error {
	m.methods = append(m.methods, "Execute")
	m.executedCmds = append(m.executedCmds, cmds)
	m.executedVars = append(m.executedVars, vars)
	return m.executeErr
}

func (m *mockCommandRunner) ExecuteWithEnv(_ context.Context, cmds []string, vars command.Vars, _ map[string]string) error {
	m.methods = append(m.methods, "ExecuteWithEnv")
	m.executedCmds = append(m.executedCmds, cmds)
	m.executedVars = append(m.executedVars, vars)
	return m.executeErr
}

func (m *mockCommandRunner) ExecuteWithOutput(_ context.Context, cmds []string, vars command.Vars, _ map[string]string, _ command.OutputCallback) error {
	m.methods = append(m.methods, "ExecuteWithOutput")
	m.executedCmds = append(m.executedCmds, cmds)
	m.executedVars = append(m.executedVars, vars)
	return m.executeErr
}

func (m *mockCommandRunner) Check(_ context.Context, cmds []string, vars command.Vars, _ map[string]string) bool {
	m.methods = append(m.methods, "Check")
	m.checkedCmds = append(m.checkedCmds, cmds)
	m.executedVars = append(m.executedVars, vars)
	return m.checkResult
}

// mockCaptureRunner for resolve.Resolver
type mockCaptureRunner struct {
	result string
	err    error
	called bool
}

func (m *mockCaptureRunner) ExecuteCapture(_ context.Context, _ []string, _ command.Vars, _ map[string]string) (string, error) {
	m.called = true
	return m.result, m.err
}

func makeCommandsTool(cmds *resource.ToolCommandSet, version string) *resource.Tool {
	return &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "mytool"},
		},
		ToolSpec: &resource.ToolSpec{
			Version:  version,
			Commands: cmds,
		},
	}
}

func TestInstallByCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		cmds               *resource.ToolCommandSet
		version            string
		action             resource.ActionType
		checkResult        bool
		executeErr         error
		resolver           *mockCaptureRunner // nil = no resolver
		wantErr            string
		wantCmds           [][]string // expected commands executed
		wantVersion        string
		wantVersionKind    resource.VersionKind
		wantSpecVersion    string
		wantCommands       bool  // state should contain Commands
		wantResolverCalled *bool // nil = don't check, non-nil = assert value
	}{
		{
			name: "fresh install",
			cmds: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"curl -fsSL https://example.com/install.sh | sh"},
					Check:   []string{"tool --version"},
				},
			},
			checkResult:     true,
			wantCmds:        [][]string{{"curl -fsSL https://example.com/install.sh | sh"}},
			wantVersionKind: resource.VersionLatest,
			wantCommands:    true,
		},
		{
			name: "upgrade with update command",
			cmds: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"curl -fsSL https://example.com/install.sh | sh"},
				},
				Update: []string{"tool update"},
			},
			action:          resource.ActionUpgrade,
			checkResult:     true,
			wantCmds:        [][]string{{"tool update"}},
			wantVersionKind: resource.VersionLatest,
			wantCommands:    true,
		},
		{
			name: "upgrade falls back to install when no update",
			cmds: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"curl -fsSL https://example.com/install.sh | sh"},
				},
			},
			action:          resource.ActionUpgrade,
			checkResult:     true,
			wantCmds:        [][]string{{"curl -fsSL https://example.com/install.sh | sh"}},
			wantVersionKind: resource.VersionLatest,
			wantCommands:    true,
		},
		{
			name: "reinstall uses update command",
			cmds: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"install-cmd"},
				},
				Update: []string{"update-cmd"},
			},
			action:          resource.ActionReinstall,
			checkResult:     true,
			wantCmds:        [][]string{{"update-cmd"}},
			wantVersionKind: resource.VersionLatest,
			wantCommands:    true,
		},
		{
			name: "install failure propagates",
			cmds: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"bad-cmd"},
				},
			},
			executeErr: fmt.Errorf("install failed"),
			wantErr:    "failed to execute command",
		},
		{
			name: "check after install succeeds",
			cmds: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"install-cmd"},
					Check:   []string{"check-cmd"},
				},
			},
			checkResult:     true,
			wantCmds:        [][]string{{"install-cmd"}},
			wantVersionKind: resource.VersionLatest,
			wantCommands:    true,
		},
		{
			name: "check failure after install",
			cmds: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"install-cmd"},
					Check:   []string{"check-cmd"},
				},
			},
			checkResult: false,
			wantErr:     "check command failed",
		},
		{
			name: "state contains all commands",
			cmds: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"install-cmd"},
					Check:   []string{"check-cmd"},
					Remove:  []string{"remove-cmd"},
				},
				Update:         []string{"update-cmd"},
				ResolveVersion: []string{"resolve-cmd"},
			},
			version:         "1.0.0",
			checkResult:     true,
			wantVersion:     "1.0.0",
			wantVersionKind: resource.VersionExact,
			wantSpecVersion: "1.0.0",
			wantCommands:    true,
		},
		{
			name: "resolveVersion populates state",
			cmds: &resource.ToolCommandSet{
				CommandSet:     resource.CommandSet{Install: []string{"install-cmd"}},
				ResolveVersion: []string{"tool --version"},
			},
			checkResult:     true,
			resolver:        &mockCaptureRunner{result: "1.0.34"},
			wantVersion:     "1.0.34",
			wantVersionKind: resource.VersionLatest,
			wantCommands:    true,
		},
		{
			name: "resolveVersion soft-fail",
			cmds: &resource.ToolCommandSet{
				CommandSet:     resource.CommandSet{Install: []string{"install-cmd"}},
				ResolveVersion: []string{"tool --version"},
			},
			checkResult:     true,
			resolver:        &mockCaptureRunner{err: fmt.Errorf("resolve failed")},
			wantVersionKind: resource.VersionLatest,
			wantCommands:    true,
		},
		{
			name: "exact version skips resolveVersion",
			cmds: &resource.ToolCommandSet{
				CommandSet:     resource.CommandSet{Install: []string{"install-cmd"}},
				ResolveVersion: []string{"tool --version"},
			},
			version:            "1.0.0",
			checkResult:        true,
			resolver:           &mockCaptureRunner{result: "should-not-be-called"},
			wantVersion:        "1.0.0",
			wantVersionKind:    resource.VersionExact,
			wantSpecVersion:    "1.0.0",
			wantCommands:       true,
			wantResolverCalled: new(bool),
		},
		{
			name: "empty version resolves to latest",
			cmds: &resource.ToolCommandSet{
				CommandSet:     resource.CommandSet{Install: []string{"install-cmd"}},
				ResolveVersion: []string{"tool --version"},
			},
			checkResult:     true,
			resolver:        &mockCaptureRunner{result: "2.0.0"},
			wantVersion:     "2.0.0",
			wantVersionKind: resource.VersionLatest,
			wantCommands:    true,
		},
		{
			name: "latest string resolves to VersionLatest",
			cmds: &resource.ToolCommandSet{
				CommandSet:     resource.CommandSet{Install: []string{"install-cmd"}},
				ResolveVersion: []string{"tool --version"},
			},
			version:         "latest",
			checkResult:     true,
			resolver:        &mockCaptureRunner{result: "4.0.0"},
			wantVersion:     "4.0.0",
			wantVersionKind: resource.VersionLatest,
			wantSpecVersion: "latest",
			wantCommands:    true,
		},
		{
			name: "latest string without resolver",
			cmds: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"install-cmd"},
				},
			},
			version:         "latest",
			checkResult:     true,
			wantVersion:     "latest",
			wantVersionKind: resource.VersionLatest,
			wantSpecVersion: "latest",
			wantCommands:    true,
		},
		{
			name: "alias version resolves to VersionAlias",
			cmds: &resource.ToolCommandSet{
				CommandSet:     resource.CommandSet{Install: []string{"install-cmd"}},
				ResolveVersion: []string{"tool --version"},
			},
			version:         "stable",
			checkResult:     true,
			resolver:        &mockCaptureRunner{result: "3.2.1"},
			wantVersion:     "3.2.1",
			wantVersionKind: resource.VersionAlias,
			wantSpecVersion: "stable",
			wantCommands:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &mockCommandRunner{checkResult: tt.checkResult, executeErr: tt.executeErr}
			inst := NewInstallerWithRunner(download.NewDownloader(), &mockPlacer{}, runner)

			if tt.resolver != nil {
				inst.SetVersionResolver(resolve.NewResolver(tt.resolver, nil))
			}

			tool := makeCommandsTool(tt.cmds, tt.version)

			ctx := context.Background()
			if tt.action != "" {
				ctx = executor.WithAction(ctx, tt.action)
			}

			state, err := inst.Install(ctx, tool, "mytool")

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, state)

			if tt.wantCmds != nil {
				assert.Equal(t, tt.wantCmds, runner.executedCmds)
			}
			assert.Equal(t, tt.wantVersion, state.Version)
			assert.Equal(t, tt.wantVersionKind, state.VersionKind)
			assert.Equal(t, tt.wantSpecVersion, state.SpecVersion)
			if tt.wantCommands {
				assert.Equal(t, tt.cmds, state.Commands)
			}
			if tt.wantResolverCalled != nil && tt.resolver != nil {
				assert.Equal(t, *tt.wantResolverCalled, tt.resolver.called, "resolver.called")
			}
		})
	}
}

func TestRemoveByCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		state      *resource.ToolState
		executeErr error
		wantErr    string
		wantCmds   [][]string
	}{
		{
			name: "remove with command",
			state: &resource.ToolState{
				Version: "1.0.0",
				Commands: &resource.ToolCommandSet{
					CommandSet: resource.CommandSet{
						Install: []string{"install-cmd"},
						Remove:  []string{"tool uninstall"},
					},
				},
			},
			wantCmds: [][]string{{"tool uninstall"}},
		},
		{
			name: "remove without command is no-op",
			state: &resource.ToolState{
				Version: "1.0.0",
				Commands: &resource.ToolCommandSet{
					CommandSet: resource.CommandSet{
						Install: []string{"install-cmd"},
					},
				},
			},
		},
		{
			name: "remove failure propagates",
			state: &resource.ToolState{
				Version: "1.0.0",
				Commands: &resource.ToolCommandSet{
					CommandSet: resource.CommandSet{
						Install: []string{"install-cmd"},
						Remove:  []string{"tool uninstall"},
					},
				},
			},
			executeErr: fmt.Errorf("uninstall failed"),
			wantErr:    "failed to execute remove command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &mockCommandRunner{checkResult: true, executeErr: tt.executeErr}
			inst := NewInstallerWithRunner(download.NewDownloader(), &mockPlacer{}, runner)

			err := inst.Remove(context.Background(), tt.state, "mytool")

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			if tt.wantCmds != nil {
				assert.Equal(t, tt.wantCmds, runner.executedCmds)
			} else {
				assert.Empty(t, runner.executedCmds)
			}
		})
	}
}

func TestInstallFromRegistry_ChecksumAlgorithmPropagation(t *testing.T) {
	t.Parallel()

	binaryContent := []byte("#!/bin/sh\necho hello")
	tarGzContent := createTarGzContent(t, "mytool", binaryContent)

	// Setup aqua registry cache with checksum algorithm
	cacheDir := t.TempDir()
	ref := aqua.RegistryRef("v4.465.0")
	pkg := "test/mytool"

	registryYAML := `packages:
  - type: github_release
    repo_owner: test
    repo_name: mytool
    asset: mytool_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    checksum:
      type: github_release
      asset: mytool_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz.sha256
      algorithm: sha256
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	dl := &mockDownloader{archiveData: tarGzContent}
	inst := NewInstaller(dl, &mockPlacer{})

	resolver := aqua.NewResolver(cacheDir, nil)
	inst.SetResolver(resolver, ref)

	tool := &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "mytool"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "v1.0.0",
			Package: &resource.Package{
				Owner: "test",
				Repo:  "mytool",
			},
		},
	}

	state, err := inst.Install(context.Background(), tool, "mytool")
	require.NoError(t, err)
	require.NotNil(t, state)

	// Verify ChecksumAlgorithm was propagated to the checksum passed to Verify
	require.NotNil(t, dl.lastVerifyChecksum, "Verify should have been called with a checksum")
	assert.Equal(t, "sha256", string(dl.lastVerifyChecksum.Algorithm), "algorithm should be propagated from aqua resolver")
	assert.NotEmpty(t, dl.lastVerifyChecksum.URL, "checksum URL should be set")
}
