package extract

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectArchiveType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ArchiveType
	}{
		{
			name:     "tar.gz extension",
			input:    "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_linux_amd64.tar.gz",
			expected: ArchiveTypeTarGz,
		},
		{
			name:     "tgz extension",
			input:    "https://example.com/tool.tgz",
			expected: ArchiveTypeTarGz,
		},
		{
			name:     "zip extension",
			input:    "https://github.com/example/releases/download/v1.0.0/tool_windows_amd64.zip",
			expected: ArchiveTypeZip,
		},
		{
			name:     "simple filename tar.gz",
			input:    "archive.tar.gz",
			expected: ArchiveTypeTarGz,
		},
		{
			name:     "simple filename zip",
			input:    "archive.zip",
			expected: ArchiveTypeZip,
		},
		{
			name:     "unknown extension",
			input:    "https://example.com/tool.exe",
			expected: "",
		},
		{
			name:     "no extension",
			input:    "https://example.com/download",
			expected: "",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectArchiveType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewExtractor(t *testing.T) {
	tests := []struct {
		name        string
		archiveType ArchiveType
		wantErr     bool
		errContain  string
	}{
		{
			name:        "tar.gz extractor",
			archiveType: ArchiveTypeTarGz,
			wantErr:     false,
		},
		{
			name:        "zip extractor",
			archiveType: ArchiveTypeZip,
			wantErr:     false,
		},
		{
			name:        "raw extractor",
			archiveType: ArchiveTypeRaw,
			wantErr:     false,
		},
		{
			name:        "unsupported archive type",
			archiveType: ArchiveType("unknown"),
			wantErr:     true,
			errContain:  "unsupported archive type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor, err := NewExtractor(tt.archiveType)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				assert.Nil(t, extractor)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, extractor)
		})
	}
}

func TestExtractor_Extract_TarGz_Stream(t *testing.T) {
	tests := []struct {
		name       string
		createData func(t *testing.T) io.Reader
		wantFiles  map[string]string // path -> content
		wantErr    bool
		errContain string
	}{
		{
			name:       "extract from stream",
			createData: createTarGzStream,
			wantFiles: map[string]string{
				"bin/tool":       "tool binary content",
				"README.md":      "readme content",
				"dir/nested.txt": "nested file content",
			},
			wantErr: false,
		},
		{
			name: "invalid gzip stream",
			createData: func(t *testing.T) io.Reader {
				return bytes.NewReader([]byte("not a valid gzip"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			destDir := filepath.Join(tmpDir, "dest")

			extractor, err := NewExtractor(ArchiveTypeTarGz)
			require.NoError(t, err)

			r := tt.createData(t)
			err = extractor.Extract(r, destDir)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)

			// Verify extracted files
			for path, wantContent := range tt.wantFiles {
				fullPath := filepath.Join(destDir, path)
				content, err := os.ReadFile(fullPath)
				require.NoError(t, err, "failed to read %s", path)
				assert.Equal(t, wantContent, string(content), "content mismatch for %s", path)
			}
		})
	}
}

func TestExtractor_Extract_Zip_File(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, archivePath string)
		wantFiles  map[string]string // path -> content
		wantErr    bool
		errContain string
	}{
		{
			name:  "extract from file",
			setup: createZipFile,
			wantFiles: map[string]string{
				"bin/tool":       "tool binary content",
				"README.md":      "readme content",
				"dir/nested.txt": "nested file content",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			archivePath := filepath.Join(tmpDir, "archive.zip")
			destDir := filepath.Join(tmpDir, "dest")

			tt.setup(t, archivePath)

			extractor, err := NewExtractor(ArchiveTypeZip)
			require.NoError(t, err)

			// zip requires file path, pass as io.Reader with file
			f, err := os.Open(archivePath)
			require.NoError(t, err)
			defer f.Close()

			err = extractor.Extract(f, destDir)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)

			// Verify extracted files
			for path, wantContent := range tt.wantFiles {
				fullPath := filepath.Join(destDir, path)
				content, err := os.ReadFile(fullPath)
				require.NoError(t, err, "failed to read %s", path)
				assert.Equal(t, wantContent, string(content), "content mismatch for %s", path)
			}
		})
	}
}

func TestExtractor_TarGz_PreservesExecutablePermission(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "dest")

	r := createTarGzStreamWithExecutable(t)

	extractor, err := NewExtractor(ArchiveTypeTarGz)
	require.NoError(t, err)

	err = extractor.Extract(r, destDir)
	require.NoError(t, err)

	// Check executable permission
	info, err := os.Stat(filepath.Join(destDir, "bin/tool"))
	require.NoError(t, err)
	assert.NotEqual(t, fs.FileMode(0), info.Mode()&0111, "expected executable permission")
}

func TestExtractor_InvalidStream(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "dest")

	extractor, err := NewExtractor(ArchiveTypeTarGz)
	require.NoError(t, err)

	r := bytes.NewReader([]byte("not a valid tar.gz"))
	err = extractor.Extract(r, destDir)
	require.Error(t, err)
}

// pureReader wraps an io.Reader without implementing io.ReaderAt
type pureReader struct {
	r io.Reader
}

func (p *pureReader) Read(b []byte) (int, error) {
	return p.r.Read(b)
}

func TestExtractor_Zip_RequiresReaderAt(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "dest")

	extractor, err := NewExtractor(ArchiveTypeZip)
	require.NoError(t, err)

	// Pass a pure io.Reader (not io.ReaderAt)
	r := &pureReader{r: bytes.NewReader([]byte("dummy"))}
	err = extractor.Extract(r, destDir)

	// Should error because zip needs ReaderAt
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ReaderAt")
}

func TestExtractor_Extract_Raw(t *testing.T) {
	tests := []struct {
		name        string
		destDirName string // final component of destDir becomes binary name
		content     string
		wantErr     bool
	}{
		{
			name:        "extract raw binary",
			destDirName: "jq",
			content:     "binary content here",
			wantErr:     false,
		},
		{
			name:        "extract raw binary with different name",
			destDirName: "mytool",
			content:     "#!/bin/sh\necho hello",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			destDir := filepath.Join(tmpDir, tt.destDirName)

			extractor, err := NewExtractor(ArchiveTypeRaw)
			require.NoError(t, err)

			r := bytes.NewReader([]byte(tt.content))
			err = extractor.Extract(r, destDir)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify the binary was created with the correct name
			binaryPath := filepath.Join(destDir, tt.destDirName)
			content, err := os.ReadFile(binaryPath)
			require.NoError(t, err)
			assert.Equal(t, tt.content, string(content))

			// Verify executable permission
			info, err := os.Stat(binaryPath)
			require.NoError(t, err)
			assert.NotEqual(t, fs.FileMode(0), info.Mode()&0111, "expected executable permission")
		})
	}
}

func TestExtractor_Raw_CreatesParentDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "nested", "path", "toolname")

	extractor, err := NewExtractor(ArchiveTypeRaw)
	require.NoError(t, err)

	content := "binary content"
	r := bytes.NewReader([]byte(content))
	err = extractor.Extract(r, destDir)
	require.NoError(t, err)

	// Verify the binary was created
	binaryPath := filepath.Join(destDir, "toolname")
	_, err = os.Stat(binaryPath)
	require.NoError(t, err)
}

// Helper functions to create test data

func createTarGzStream(t *testing.T) io.Reader {
	t.Helper()

	files := map[string]string{
		"bin/tool":       "tool binary content",
		"README.md":      "readme content",
		"dir/nested.txt": "nested file content",
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		err := tw.WriteHeader(hdr)
		require.NoError(t, err)
		_, err = tw.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return &buf
}

func createTarGzStreamWithExecutable(t *testing.T) io.Reader {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := "executable content"
	hdr := &tar.Header{
		Name: "bin/tool",
		Mode: 0755,
		Size: int64(len(content)),
	}
	err := tw.WriteHeader(hdr)
	require.NoError(t, err)
	_, err = tw.Write([]byte(content))
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return &buf
}

func createZipFile(t *testing.T, archivePath string) {
	t.Helper()

	files := map[string]string{
		"bin/tool":       "tool binary content",
		"README.md":      "readme content",
		"dir/nested.txt": "nested file content",
	}

	f, err := os.Create(archivePath)
	require.NoError(t, err)
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for name, content := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
}
