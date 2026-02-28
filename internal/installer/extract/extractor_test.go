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
	"github.com/ulikunitz/xz"
)

func TestNormalizeArchiveType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  ArchiveType
	}{
		{name: "tar.gz", input: "tar.gz", want: ArchiveTypeTarGz},
		{name: "tgz", input: "tgz", want: ArchiveTypeTarGz},
		{name: "TGZ uppercase", input: "TGZ", want: ArchiveTypeTarGz},
		{name: "tar.xz", input: "tar.xz", want: ArchiveTypeTarXz},
		{name: "txz", input: "txz", want: ArchiveTypeTarXz},
		{name: "zip", input: "zip", want: ArchiveTypeZip},
		{name: "raw", input: "raw", want: ArchiveTypeRaw},
		{name: "unknown", input: "unknown", want: ArchiveType("unknown")},
		{name: "empty", input: "", want: ArchiveType("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, NormalizeArchiveType(tt.input))
		})
	}
}

func TestDetectArchiveType(t *testing.T) {
	t.Parallel()
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
			name:     "tar.xz extension",
			input:    "https://ziglang.org/download/0.14.0/zig-x86_64-linux-0.14.0.tar.xz",
			expected: ArchiveTypeTarXz,
		},
		{
			name:     "txz extension",
			input:    "https://example.com/tool.txz",
			expected: ArchiveTypeTarXz,
		},
		{
			name:     "simple filename tar.xz",
			input:    "archive.tar.xz",
			expected: ArchiveTypeTarXz,
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
			t.Parallel()
			result := DetectArchiveType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewExtractor(t *testing.T) {
	t.Parallel()
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
			name:        "tar.xz extractor",
			archiveType: ArchiveTypeTarXz,
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
			t.Parallel()
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
	t.Parallel()
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
			t.Parallel()
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

func TestExtractor_Extract_TarXz_Stream(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		createData func(t *testing.T) io.Reader
		wantFiles  map[string]string // path -> content
		wantErr    bool
	}{
		{
			name:       "extract from stream",
			createData: createTarXzStream,
			wantFiles: map[string]string{
				"bin/tool":       "tool binary content",
				"README.md":      "readme content",
				"dir/nested.txt": "nested file content",
			},
			wantErr: false,
		},
		{
			name: "invalid xz stream",
			createData: func(t *testing.T) io.Reader {
				return bytes.NewReader([]byte("not a valid xz"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			destDir := filepath.Join(tmpDir, "dest")

			extractor, err := NewExtractor(ArchiveTypeTarXz)
			require.NoError(t, err)

			r := tt.createData(t)
			err = extractor.Extract(r, destDir)

			if tt.wantErr {
				require.Error(t, err)
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

func TestExtractor_TarXz_PreservesExecutablePermission(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "dest")

	r := createTarXzStreamWithExecutable(t)

	extractor, err := NewExtractor(ArchiveTypeTarXz)
	require.NoError(t, err)

	err = extractor.Extract(r, destDir)
	require.NoError(t, err)

	// Check executable permission
	info, err := os.Stat(filepath.Join(destDir, "bin/tool"))
	require.NoError(t, err)
	assert.NotEqual(t, fs.FileMode(0), info.Mode()&0111, "expected executable permission")
}

func TestExtractor_Extract_Zip_File(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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

func TestExtractor_Zip_SkipsMacOSMetadata(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.zip")
	destDir := filepath.Join(tmpDir, "dest")

	// Create a ZIP with __MACOSX metadata entries
	f, err := os.Create(archivePath)
	require.NoError(t, err)
	zw := zip.NewWriter(f)

	// Real content
	w, err := zw.Create("mydir/binary")
	require.NoError(t, err)
	_, err = w.Write([]byte("binary content"))
	require.NoError(t, err)

	// macOS metadata (should be skipped)
	w, err = zw.Create("__MACOSX/._binary")
	require.NoError(t, err)
	_, err = w.Write([]byte("metadata"))
	require.NoError(t, err)

	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	// Extract
	extractor, err := NewExtractor(ArchiveTypeZip)
	require.NoError(t, err)

	zf, err := os.Open(archivePath)
	require.NoError(t, err)
	defer zf.Close()

	err = extractor.Extract(zf, destDir)
	require.NoError(t, err)

	// Verify real content exists
	content, err := os.ReadFile(filepath.Join(destDir, "mydir", "binary"))
	require.NoError(t, err)
	assert.Equal(t, "binary content", string(content))

	// Verify __MACOSX was NOT extracted
	_, err = os.Stat(filepath.Join(destDir, "__MACOSX"))
	assert.True(t, os.IsNotExist(err), "__MACOSX directory should not exist after extraction")
}

func TestIsOSMetadataPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "__MACOSX bare", input: "__MACOSX", want: true},
		{name: "__MACOSX with slash", input: "__MACOSX/", want: true},
		{name: "__MACOSX nested", input: "__MACOSX/._binary", want: true},
		{name: "regular path", input: "mydir/binary", want: false},
		{name: "lowercase", input: "__macosx/", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isOSMetadataPath(tt.input))
		})
	}
}

func TestExtractor_TarGz_PreservesExecutablePermission(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
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

func TestExtractTar_DeferredLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []tarEntry
		verify  func(t *testing.T, destDir string)
	}{
		{
			name: "symlink with missing parent directory",
			entries: []tarEntry{
				{typeflag: tar.TypeReg, name: "root/data.txt", content: "hello"},
				{typeflag: tar.TypeSymlink, name: "root/sub/deep/link.txt", linkname: "../../data.txt"},
			},
			verify: func(t *testing.T, destDir string) {
				target, err := os.Readlink(filepath.Join(destDir, "root/sub/deep/link.txt"))
				require.NoError(t, err)
				assert.Equal(t, "../../data.txt", target)

				content, err := os.ReadFile(filepath.Join(destDir, "root/sub/deep/link.txt"))
				require.NoError(t, err)
				assert.Equal(t, "hello", string(content))
			},
		},
		{
			name: "forward-reference symlink",
			entries: []tarEntry{
				{typeflag: tar.TypeDir, name: "pkg/", mode: 0755},
				{typeflag: tar.TypeSymlink, name: "pkg/link.txt", linkname: "real.txt"},
				{typeflag: tar.TypeReg, name: "pkg/real.txt", content: "forward ref"},
			},
			verify: func(t *testing.T, destDir string) {
				target, err := os.Readlink(filepath.Join(destDir, "pkg/link.txt"))
				require.NoError(t, err)
				assert.Equal(t, "real.txt", target)

				content, err := os.ReadFile(filepath.Join(destDir, "pkg/link.txt"))
				require.NoError(t, err)
				assert.Equal(t, "forward ref", string(content))
			},
		},
		{
			name: "hard link",
			entries: []tarEntry{
				{typeflag: tar.TypeReg, name: "original.txt", content: "hardlink content"},
				{typeflag: tar.TypeLink, name: "linked.txt", linkname: "original.txt"},
			},
			verify: func(t *testing.T, destDir string) {
				orig, err := os.ReadFile(filepath.Join(destDir, "original.txt"))
				require.NoError(t, err)
				linked, err := os.ReadFile(filepath.Join(destDir, "linked.txt"))
				require.NoError(t, err)
				assert.Equal(t, string(orig), string(linked))

				origInfo, err := os.Stat(filepath.Join(destDir, "original.txt"))
				require.NoError(t, err)
				linkedInfo, err := os.Stat(filepath.Join(destDir, "linked.txt"))
				require.NoError(t, err)
				assert.True(t, os.SameFile(origInfo, linkedInfo), "expected hard link (same inode)")
			},
		},
		{
			name: "forward-reference hard link",
			entries: []tarEntry{
				{typeflag: tar.TypeLink, name: "link-first.txt", linkname: "target-later.txt"},
				{typeflag: tar.TypeReg, name: "target-later.txt", content: "deferred hard"},
			},
			verify: func(t *testing.T, destDir string) {
				orig, err := os.ReadFile(filepath.Join(destDir, "target-later.txt"))
				require.NoError(t, err)
				linked, err := os.ReadFile(filepath.Join(destDir, "link-first.txt"))
				require.NoError(t, err)
				assert.Equal(t, string(orig), string(linked))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := createTarGzStreamWithEntries(t, tt.entries)
			destDir := filepath.Join(t.TempDir(), "dest")

			ext, err := NewExtractor(ArchiveTypeTarGz)
			require.NoError(t, err)
			require.NoError(t, ext.Extract(r, destDir))

			tt.verify(t, destDir)
		})
	}
}

func TestExtractTar_LinkEscape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		entries    []tarEntry
		errContain string
	}{
		{
			name: "symlink escapes destDir",
			entries: []tarEntry{
				{typeflag: tar.TypeSymlink, name: "escape", linkname: "../../../etc/passwd"},
			},
			errContain: "invalid symlink target",
		},
		{
			name: "hard link escapes destDir",
			entries: []tarEntry{
				{typeflag: tar.TypeLink, name: "escape", linkname: "../../../etc/passwd"},
			},
			errContain: "invalid hard link target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := createTarGzStreamWithEntries(t, tt.entries)
			destDir := filepath.Join(t.TempDir(), "dest")

			ext, err := NewExtractor(ArchiveTypeTarGz)
			require.NoError(t, err)

			err = ext.Extract(r, destDir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContain)
		})
	}
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

func createTarXzStream(t *testing.T) io.Reader {
	t.Helper()

	files := map[string]string{
		"bin/tool":       "tool binary content",
		"README.md":      "readme content",
		"dir/nested.txt": "nested file content",
	}

	var buf bytes.Buffer
	xw, err := xz.NewWriter(&buf)
	require.NoError(t, err)
	tw := tar.NewWriter(xw)

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
	require.NoError(t, xw.Close())

	return &buf
}

func createTarXzStreamWithExecutable(t *testing.T) io.Reader {
	t.Helper()

	var buf bytes.Buffer
	xw, err := xz.NewWriter(&buf)
	require.NoError(t, err)
	tw := tar.NewWriter(xw)

	content := "executable content"
	hdr := &tar.Header{
		Name: "bin/tool",
		Mode: 0755,
		Size: int64(len(content)),
	}
	err = tw.WriteHeader(hdr)
	require.NoError(t, err)
	_, err = tw.Write([]byte(content))
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, xw.Close())

	return &buf
}

// tarEntry describes a single entry for createTarGzStreamWithEntries.
type tarEntry struct {
	typeflag byte
	name     string
	content  string // only for TypeReg
	linkname string // for TypeSymlink / TypeLink
	mode     int64  // 0 defaults to 0644 (files) or 0755 (dirs)
}

// createTarGzStreamWithEntries builds a tar.gz stream from arbitrary entries,
// preserving the given order so that tests can exercise forward-reference scenarios.
func createTarGzStreamWithEntries(t *testing.T, entries []tarEntry) io.Reader {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, e := range entries {
		mode := e.mode
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Linkname: e.linkname,
		}
		switch e.typeflag {
		case tar.TypeDir:
			if mode == 0 {
				mode = 0755
			}
			hdr.Mode = mode
		case tar.TypeReg:
			if mode == 0 {
				mode = 0644
			}
			hdr.Mode = mode
			hdr.Size = int64(len(e.content))
		case tar.TypeSymlink, tar.TypeLink:
			// no size / mode needed
		}

		require.NoError(t, tw.WriteHeader(hdr))
		if e.typeflag == tar.TypeReg {
			_, err := tw.Write([]byte(e.content))
			require.NoError(t, err)
		}
	}

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
