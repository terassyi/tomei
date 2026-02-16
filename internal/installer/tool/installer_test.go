package tool

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/place"
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
	// Create test server
	binaryContent := []byte("#!/bin/sh\necho hello")
	tarGzContent := createTarGzContent(t, "ripgrep", binaryContent)
	archiveHash := sha256Hash(tarGzContent) // Hash of archive, not binary

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			// Return checksum file
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(archiveHash + "  ripgrep.tar.gz\n"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(tarGzContent)
	}))
	defer server.Close()

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
						URL: server.URL + "/ripgrep.tar.gz",
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
			tmpDir := t.TempDir()
			toolsDir := filepath.Join(tmpDir, "tools")
			binDir := filepath.Join(tmpDir, "bin")

			downloader := download.NewDownloader()
			placer := place.NewPlacer(toolsDir, binDir)

			installer := NewInstaller(downloader, placer)

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
			assert.NotEmpty(t, state.Digest)

			// Verify binary exists
			_, err = os.Stat(state.InstallPath)
			require.NoError(t, err)

			// Verify symlink exists
			_, err = os.Lstat(state.BinPath)
			require.NoError(t, err)
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

func TestToolInstaller_InstallFromRegistry(t *testing.T) {
	t.Parallel()
	// Create test binary content
	binaryContent := []byte("#!/bin/sh\necho hello from registry")
	tarGzContent := createTarGzContent(t, "mytool", binaryContent)

	// Create test server that serves both registry YAML and binary
	// NOTE: The registry uses type: http to point to our mock server for downloads
	// Checksum is disabled for simplicity in this test
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/pkgs/test/mytool/registry.yaml"):
			// Serve registry definition with type: http to use our mock server for downloads
			// No checksum configured for simpler testing
			w.WriteHeader(http.StatusOK)
			registryYAML := `packages:
  - type: http
    repo_owner: test
    repo_name: mytool
    url: "` + serverURL + `/releases/mytool_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz"
    format: tar.gz
`
			_, _ = w.Write([]byte(registryYAML))
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			// Serve binary archive
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(tarGzContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	binDir := filepath.Join(tmpDir, "bin")
	cacheDir := filepath.Join(tmpDir, "cache")

	downloader := download.NewDownloader()
	placer := place.NewPlacer(toolsDir, binDir)
	installer := NewInstaller(downloader, placer)

	// Create mock resolver that uses our test server
	resolver := createMockResolver(t, cacheDir, server.URL)
	installer.SetResolver(resolver, "v4.465.0")

	// Create tool with registry package
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

	// Install
	state, err := installer.Install(context.Background(), tool, "mytool")
	require.NoError(t, err)
	require.NotNil(t, state)

	// Verify state
	assert.Equal(t, "aqua", state.InstallerRef)
	assert.Equal(t, "1.0.0", state.Version)
	assert.NotEmpty(t, state.InstallPath)
	assert.NotEmpty(t, state.BinPath)
	assert.Equal(t, "test", state.Package.Owner)
	assert.Equal(t, "mytool", state.Package.Repo)

	// Verify binary exists
	_, err = os.Stat(state.InstallPath)
	require.NoError(t, err)
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

func (m *mockDownloader) Verify(_ context.Context, _ string, _ *resource.Checksum) error {
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
