//go:build darwin

package extract

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPkgExtractor_Extract(t *testing.T) {
	// Not parallel — depends on pkgbuild system tool
	if _, err := exec.LookPath("pkgbuild"); err != nil {
		t.Skip("pkgbuild not found, skipping pkg extraction test")
	}

	tmpDir := t.TempDir()

	// Create a minimal payload: a single executable binary
	payloadDir := filepath.Join(tmpDir, "payload")
	binDir := filepath.Join(payloadDir, "usr", "local", "bin")
	require.NoError(t, os.MkdirAll(binDir, 0755))

	binaryPath := filepath.Join(binDir, "hello")
	require.NoError(t, os.WriteFile(binaryPath, []byte("#!/bin/sh\necho hello\n"), 0755))

	// Build a .pkg using pkgbuild
	pkgPath := filepath.Join(tmpDir, "test.pkg")
	cmd := exec.Command("pkgbuild",
		"--root", payloadDir,
		"--identifier", "net.terassyi.tomei.test",
		"--version", "1.0",
		pkgPath,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "pkgbuild failed: %s", string(out))

	// Extract using pkgExtractor
	destDir := filepath.Join(tmpDir, "dest")

	extractor, err := NewExtractor(ArchiveTypePkg)
	require.NoError(t, err)

	f, err := os.Open(pkgPath)
	require.NoError(t, err)
	defer f.Close()

	err = extractor.Extract(f, destDir)
	require.NoError(t, err)

	// Verify the binary exists somewhere under destDir
	// pkgutil --expand-full creates a <pkg-id>.pkg/Payload/ structure
	found := false
	err = filepath.WalkDir(destDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "hello" && !d.IsDir() {
			found = true
			content, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, "#!/bin/sh\necho hello\n", string(content))

			// Verify executable permission is preserved
			info, statErr := os.Stat(path)
			require.NoError(t, statErr)
			assert.NotEqual(t, fs.FileMode(0), info.Mode()&0111, "expected executable permission")
		}
		return nil
	})
	require.NoError(t, err)
	assert.True(t, found, "expected to find 'hello' binary in extracted pkg")
}

func TestPkgExtractor_ExtractFailsOnBrokenPkg(t *testing.T) {
	// Not parallel — depends on pkgutil system tool
	if _, err := exec.LookPath("pkgutil"); err != nil {
		t.Skip("pkgutil not found, skipping broken pkg test")
	}

	tmpDir := t.TempDir()

	// Create a corrupted .pkg file
	corruptedPkg := filepath.Join(tmpDir, "corrupt.pkg")
	require.NoError(t, os.WriteFile(corruptedPkg, []byte("not a real pkg"), 0644))

	f, err := os.Open(corruptedPkg)
	require.NoError(t, err)
	defer f.Close()

	extractor, err := NewExtractor(ArchiveTypePkg)
	require.NoError(t, err)

	err = extractor.Extract(f, filepath.Join(tmpDir, "dest"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to expand pkg")
}
