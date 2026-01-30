package tests

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
	"github.com/terassyi/toto/internal/installer/download"
	"github.com/terassyi/toto/internal/installer/engine"
	"github.com/terassyi/toto/internal/installer/place"
	"github.com/terassyi/toto/internal/installer/tool"
	"github.com/terassyi/toto/internal/state"
)

// TestIntegration_FullInstallFlow tests the complete flow:
// CUE config -> Engine -> Download -> Extract -> Place -> Symlink -> State
func TestIntegration_FullInstallFlow(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho 'hello from ripgrep'\n")
	tarGzContent := createTarGz(t, "ripgrep", binaryContent)
	archiveHash := sha256sum(tarGzContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			_, _ = w.Write([]byte(archiveHash + "  ripgrep.tar.gz\n"))
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			_, _ = w.Write(tarGzContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	dataDir := filepath.Join(tmpDir, "data")
	binDir := filepath.Join(tmpDir, "bin")

	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.MkdirAll(binDir, 0755))

	cueContent := `package toto

ripgrep: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "download"
		version: "14.1.1"
		source: {
			url: "` + server.URL + `/ripgrep-14.1.1.tar.gz"
			checksum: { value: "sha256:` + archiveHash + `" }
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueContent), 0644))

	store, err := state.NewStore[state.UserState](dataDir)
	require.NoError(t, err)

	downloader := download.NewDownloader()
	placer := place.NewPlacer(filepath.Join(dataDir, "tools"), binDir)
	toolInstaller := tool.NewInstaller(downloader, placer)
	eng := engine.NewEngine(toolInstaller, store)

	err = eng.Apply(context.Background(), configDir)
	require.NoError(t, err)

	// Verify binary
	binaryPath := filepath.Join(dataDir, "tools", "ripgrep", "14.1.1", "ripgrep")
	assert.FileExists(t, binaryPath)
	content, err := os.ReadFile(binaryPath)
	require.NoError(t, err)
	assert.Equal(t, binaryContent, content)

	// Verify symlink
	symlinkPath := filepath.Join(binDir, "ripgrep")
	linkTarget, err := os.Readlink(symlinkPath)
	require.NoError(t, err)
	assert.Equal(t, binaryPath, linkTarget)

	// Verify state
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)
	require.NotNil(t, st.Tools["ripgrep"])
	assert.Equal(t, "14.1.1", st.Tools["ripgrep"].Version)
}

// TestIntegration_UpgradeFlow tests upgrading a tool
func TestIntegration_UpgradeFlow(t *testing.T) {
	v1Content := []byte("#!/bin/sh\necho 'v1'\n")
	v2Content := []byte("#!/bin/sh\necho 'v2'\n")
	v1TarGz := createTarGz(t, "mytool", v1Content)
	v2TarGz := createTarGz(t, "mytool", v2Content)
	v1Hash := sha256sum(v1TarGz)
	v2Hash := sha256sum(v2TarGz)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "1.0.0"):
			if strings.HasSuffix(r.URL.Path, ".sha256") {
				_, _ = w.Write([]byte(v1Hash))
			} else {
				_, _ = w.Write(v1TarGz)
			}
		case strings.Contains(r.URL.Path, "2.0.0"):
			if strings.HasSuffix(r.URL.Path, ".sha256") {
				_, _ = w.Write([]byte(v2Hash))
			} else {
				_, _ = w.Write(v2TarGz)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	dataDir := filepath.Join(tmpDir, "data")
	binDir := filepath.Join(tmpDir, "bin")

	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.MkdirAll(binDir, 0755))

	store, err := state.NewStore[state.UserState](dataDir)
	require.NoError(t, err)

	downloader := download.NewDownloader()
	placer := place.NewPlacer(filepath.Join(dataDir, "tools"), binDir)
	toolInstaller := tool.NewInstaller(downloader, placer)
	eng := engine.NewEngine(toolInstaller, store)

	// Install v1
	cueV1 := `package toto
mytool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "mytool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "` + server.URL + `/mytool-1.0.0.tar.gz"
			checksum: { value: "sha256:` + v1Hash + `" }
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueV1), 0644))
	require.NoError(t, eng.Apply(context.Background(), configDir))

	// Verify v1
	content, err := os.ReadFile(filepath.Join(dataDir, "tools", "mytool", "1.0.0", "mytool"))
	require.NoError(t, err)
	assert.Equal(t, v1Content, content)

	// Upgrade to v2
	cueV2 := `package toto
mytool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "mytool"
	spec: {
		installerRef: "download"
		version: "2.0.0"
		source: {
			url: "` + server.URL + `/mytool-2.0.0.tar.gz"
			checksum: { value: "sha256:` + v2Hash + `" }
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueV2), 0644))
	require.NoError(t, eng.Apply(context.Background(), configDir))

	// Verify v2
	content, err = os.ReadFile(filepath.Join(dataDir, "tools", "mytool", "2.0.0", "mytool"))
	require.NoError(t, err)
	assert.Equal(t, v2Content, content)

	// Verify state
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", st.Tools["mytool"].Version)
}

// TestIntegration_RemoveFlow tests removing a tool
func TestIntegration_RemoveFlow(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho 'hello'\n")
	tarGzContent := createTarGz(t, "removeme", binaryContent)
	archiveHash := sha256sum(tarGzContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(archiveHash))
		} else {
			_, _ = w.Write(tarGzContent)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	dataDir := filepath.Join(tmpDir, "data")
	binDir := filepath.Join(tmpDir, "bin")

	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.MkdirAll(binDir, 0755))

	store, err := state.NewStore[state.UserState](dataDir)
	require.NoError(t, err)

	downloader := download.NewDownloader()
	placer := place.NewPlacer(filepath.Join(dataDir, "tools"), binDir)
	toolInstaller := tool.NewInstaller(downloader, placer)
	eng := engine.NewEngine(toolInstaller, store)

	// Install tool
	cueWithTool := `package toto
removeme: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "removeme"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "` + server.URL + `/removeme.tar.gz"
			checksum: { value: "sha256:` + archiveHash + `" }
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueWithTool), 0644))
	require.NoError(t, eng.Apply(context.Background(), configDir))

	// Verify installed
	binaryPath := filepath.Join(dataDir, "tools", "removeme", "1.0.0", "removeme")
	symlinkPath := filepath.Join(binDir, "removeme")
	assert.FileExists(t, binaryPath)

	// Remove tool - config with only an Installer (loader requires at least one resource)
	cueEmpty := `package toto

downloadInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "download"
	spec: pattern: "download"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueEmpty), 0644))
	require.NoError(t, eng.Apply(context.Background(), configDir))

	// Verify removed
	assert.NoFileExists(t, binaryPath)
	_, err = os.Lstat(symlinkPath)
	assert.True(t, os.IsNotExist(err))

	// Verify state
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)
	assert.Nil(t, st.Tools["removeme"])
}

// TestIntegration_MultipleTools tests installing multiple tools
func TestIntegration_MultipleTools(t *testing.T) {
	tool1Content := []byte("#!/bin/sh\necho 'tool1'\n")
	tool2Content := []byte("#!/bin/sh\necho 'tool2'\n")
	tool1TarGz := createTarGz(t, "tool1", tool1Content)
	tool2TarGz := createTarGz(t, "tool2", tool2Content)
	tool1Hash := sha256sum(tool1TarGz)
	tool2Hash := sha256sum(tool2TarGz)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "tool1"):
			if strings.HasSuffix(r.URL.Path, ".sha256") {
				_, _ = w.Write([]byte(tool1Hash))
			} else {
				_, _ = w.Write(tool1TarGz)
			}
		case strings.Contains(r.URL.Path, "tool2"):
			if strings.HasSuffix(r.URL.Path, ".sha256") {
				_, _ = w.Write([]byte(tool2Hash))
			} else {
				_, _ = w.Write(tool2TarGz)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	dataDir := filepath.Join(tmpDir, "data")
	binDir := filepath.Join(tmpDir, "bin")

	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.MkdirAll(binDir, 0755))

	store, err := state.NewStore[state.UserState](dataDir)
	require.NoError(t, err)

	downloader := download.NewDownloader()
	placer := place.NewPlacer(filepath.Join(dataDir, "tools"), binDir)
	toolInstaller := tool.NewInstaller(downloader, placer)
	eng := engine.NewEngine(toolInstaller, store)

	cue := `package toto

tool1: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool1"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "` + server.URL + `/tool1.tar.gz"
			checksum: { value: "sha256:` + tool1Hash + `" }
		}
	}
}

tool2: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool2"
	spec: {
		installerRef: "download"
		version: "2.0.0"
		source: {
			url: "` + server.URL + `/tool2.tar.gz"
			checksum: { value: "sha256:` + tool2Hash + `" }
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cue), 0644))
	require.NoError(t, eng.Apply(context.Background(), configDir))

	// Verify both installed
	assert.FileExists(t, filepath.Join(dataDir, "tools", "tool1", "1.0.0", "tool1"))
	assert.FileExists(t, filepath.Join(dataDir, "tools", "tool2", "2.0.0", "tool2"))

	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)
	assert.Len(t, st.Tools, 2)
}

// TestIntegration_IdempotentApply tests idempotency
func TestIntegration_IdempotentApply(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho 'hello'\n")
	tarGzContent := createTarGz(t, "idempotent", binaryContent)
	archiveHash := sha256sum(tarGzContent)

	downloadCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(archiveHash))
		} else {
			downloadCount++
			_, _ = w.Write(tarGzContent)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	dataDir := filepath.Join(tmpDir, "data")
	binDir := filepath.Join(tmpDir, "bin")

	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.MkdirAll(binDir, 0755))

	store, err := state.NewStore[state.UserState](dataDir)
	require.NoError(t, err)

	downloader := download.NewDownloader()
	placer := place.NewPlacer(filepath.Join(dataDir, "tools"), binDir)
	toolInstaller := tool.NewInstaller(downloader, placer)
	eng := engine.NewEngine(toolInstaller, store)

	cue := `package toto
idempotent: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "idempotent"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "` + server.URL + `/idempotent.tar.gz"
			checksum: { value: "sha256:` + archiveHash + `" }
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cue), 0644))

	// First apply
	require.NoError(t, eng.Apply(context.Background(), configDir))
	assert.Equal(t, 1, downloadCount)

	// Second apply - should skip
	require.NoError(t, eng.Apply(context.Background(), configDir))
	assert.Equal(t, 1, downloadCount, "should not download again")

	// Third apply - still idempotent
	require.NoError(t, eng.Apply(context.Background(), configDir))
	assert.Equal(t, 1, downloadCount, "should not download again")
}

// TestIntegration_Plan tests plan without execution
func TestIntegration_Plan(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho 'hello'\n")
	tarGzContent := createTarGz(t, "planned", binaryContent)
	archiveHash := sha256sum(tarGzContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Plan should not download anything")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	dataDir := filepath.Join(tmpDir, "data")
	binDir := filepath.Join(tmpDir, "bin")

	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.MkdirAll(binDir, 0755))

	store, err := state.NewStore[state.UserState](dataDir)
	require.NoError(t, err)

	downloader := download.NewDownloader()
	placer := place.NewPlacer(filepath.Join(dataDir, "tools"), binDir)
	toolInstaller := tool.NewInstaller(downloader, placer)
	eng := engine.NewEngine(toolInstaller, store)

	cue := `package toto
planned: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "planned"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "` + server.URL + `/planned.tar.gz"
			checksum: { value: "sha256:` + archiveHash + `" }
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cue), 0644))

	actions, err := eng.Plan(context.Background(), configDir)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	assert.Equal(t, "planned", actions[0].Name)

	// Verify nothing installed
	assert.NoFileExists(t, filepath.Join(dataDir, "tools", "planned", "1.0.0", "planned"))
}

// Helper functions

func createTarGz(t *testing.T, binaryName string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: binaryName,
		Mode: 0755,
		Size: int64(len(content)),
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write(content)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return buf.Bytes()
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
