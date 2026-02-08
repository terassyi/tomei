package checksum

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectFileFormat(t *testing.T) {
	sha256Hash := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	tests := []struct {
		name    string
		content []byte
		want    FileFormat
	}{
		{
			name:    "GNU format",
			content: []byte(sha256Hash + "  filename.tar.gz"),
			want:    FileFormatGNU,
		},
		{
			name:    "GNU format with binary indicator",
			content: []byte(sha256Hash + " *filename.tar.gz"),
			want:    FileFormatGNU,
		},
		{
			name:    "GNU format with whitespace",
			content: []byte("  " + sha256Hash + "  filename.tar.gz\n"),
			want:    FileFormatGNU,
		},
		{
			name:    "BSD format SHA256",
			content: []byte("SHA256 (filename.tar.gz) = " + sha256Hash),
			want:    FileFormatBSD,
		},
		{
			name:    "BSD format with whitespace",
			content: []byte("  SHA256 (filename.tar.gz) = " + sha256Hash + "\n"),
			want:    FileFormatBSD,
		},
		{
			name:    "Go JSON format",
			content: []byte(`[{"version":"go1.23.5","files":[]}]`),
			want:    FileFormatGoJSON,
		},
		{
			name:    "Go JSON format with whitespace",
			content: []byte(`  [{"version":"go1.23.5","files":[]}]`),
			want:    FileFormatGoJSON,
		},
		{
			name:    "non-Go JSON is unknown",
			content: []byte(`{"foo":"bar"}`),
			want:    FileFormatUnknown,
		},
		{
			name:    "empty content",
			content: []byte(""),
			want:    FileFormatUnknown,
		},
		{
			name:    "invalid content",
			content: []byte("not a valid checksum file"),
			want:    FileFormatUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectFileFormat(tt.content)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseFile_GNU(t *testing.T) {
	sha256Hash := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	tests := []struct {
		name       string
		content    []byte
		filename   string
		wantAlgo   Algorithm
		wantHash   string
		wantErr    bool
		errContain string
	}{
		{
			name:     "standard format",
			content:  []byte(sha256Hash + "  test.tar.gz\n"),
			filename: "test.tar.gz",
			wantAlgo: AlgorithmSHA256,
			wantHash: sha256Hash,
			wantErr:  false,
		},
		{
			name:     "binary mode with asterisk",
			content:  []byte(sha256Hash + " *test.tar.gz\n"),
			filename: "test.tar.gz",
			wantAlgo: AlgorithmSHA256,
			wantHash: sha256Hash,
			wantErr:  false,
		},
		{
			name: "multiple files",
			content: []byte(
				"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  other.tar.gz\n" +
					sha256Hash + "  test.tar.gz\n" +
					"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  another.tar.gz\n",
			),
			filename: "test.tar.gz",
			wantAlgo: AlgorithmSHA256,
			wantHash: sha256Hash,
			wantErr:  false,
		},
		{
			name:     "match by basename",
			content:  []byte(sha256Hash + "  path/to/test.tar.gz\n"),
			filename: "test.tar.gz",
			wantAlgo: AlgorithmSHA256,
			wantHash: sha256Hash,
			wantErr:  false,
		},
		{
			name:       "file not found",
			content:    []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  other.tar.gz\n"),
			filename:   "test.tar.gz",
			wantErr:    true,
			errContain: "not found in GNU checksums file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			algo, hash, err := ParseFile(tt.content, tt.filename)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAlgo, algo)
			assert.Equal(t, tt.wantHash, hash)
		})
	}
}

func TestParseFile_BSD(t *testing.T) {
	sha256Hash := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	sha512Hash := "309ecc489c12d6eb4cc40f50c902f2b4d0ed77ee511a7c7a9bcd3ca86d4cd86f989dd35bc5ff499670da34255b45b0cfd830e81f605dcf7dc5542e93ae9cd76f"

	tests := []struct {
		name       string
		content    []byte
		filename   string
		wantAlgo   Algorithm
		wantHash   string
		wantErr    bool
		errContain string
	}{
		{
			name:     "SHA256 format",
			content:  []byte("SHA256 (test.tar.gz) = " + sha256Hash + "\n"),
			filename: "test.tar.gz",
			wantAlgo: AlgorithmSHA256,
			wantHash: sha256Hash,
			wantErr:  false,
		},
		{
			name:     "SHA512 format",
			content:  []byte("SHA512 (test.tar.gz) = " + sha512Hash + "\n"),
			filename: "test.tar.gz",
			wantAlgo: AlgorithmSHA512,
			wantHash: sha512Hash,
			wantErr:  false,
		},
		{
			name: "multiple files",
			content: []byte(
				"SHA256 (other.tar.gz) = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
					"SHA256 (test.tar.gz) = " + sha256Hash + "\n" +
					"SHA256 (another.tar.gz) = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
			),
			filename: "test.tar.gz",
			wantAlgo: AlgorithmSHA256,
			wantHash: sha256Hash,
			wantErr:  false,
		},
		{
			name:     "match by basename",
			content:  []byte("SHA256 (path/to/test.tar.gz) = " + sha256Hash + "\n"),
			filename: "test.tar.gz",
			wantAlgo: AlgorithmSHA256,
			wantHash: sha256Hash,
			wantErr:  false,
		},
		{
			name:       "file not found",
			content:    []byte("SHA256 (other.tar.gz) = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"),
			filename:   "test.tar.gz",
			wantErr:    true,
			errContain: "not found in BSD checksums file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			algo, hash, err := ParseFile(tt.content, tt.filename)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAlgo, algo)
			assert.Equal(t, tt.wantHash, hash)
		})
	}
}

func TestParseFile_GoJSON(t *testing.T) {
	sha256Hash := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	tests := []struct {
		name       string
		content    []byte
		filename   string
		wantAlgo   Algorithm
		wantHash   string
		wantErr    bool
		errContain string
	}{
		{
			name: "single release",
			content: []byte(`[
				{
					"version": "go1.23.5",
					"stable": true,
					"files": [
						{
							"filename": "go1.23.5.linux-amd64.tar.gz",
							"os": "linux",
							"arch": "amd64",
							"sha256": "` + sha256Hash + `",
							"size": 12345,
							"kind": "archive"
						}
					]
				}
			]`),
			filename: "go1.23.5.linux-amd64.tar.gz",
			wantAlgo: AlgorithmSHA256,
			wantHash: sha256Hash,
			wantErr:  false,
		},
		{
			name: "multiple releases and files",
			content: []byte(`[
				{
					"version": "go1.24.0",
					"stable": true,
					"files": [
						{"filename": "go1.24.0.linux-amd64.tar.gz", "sha256": "aaaa", "kind": "archive"}
					]
				},
				{
					"version": "go1.23.5",
					"stable": true,
					"files": [
						{"filename": "go1.23.5.linux-amd64.tar.gz", "sha256": "` + sha256Hash + `", "kind": "archive"},
						{"filename": "go1.23.5.darwin-arm64.tar.gz", "sha256": "bbbb", "kind": "archive"}
					]
				}
			]`),
			filename: "go1.23.5.linux-amd64.tar.gz",
			wantAlgo: AlgorithmSHA256,
			wantHash: sha256Hash,
			wantErr:  false,
		},
		{
			name: "file not found",
			content: []byte(`[
				{
					"version": "go1.23.5",
					"stable": true,
					"files": [
						{"filename": "go1.23.5.darwin-arm64.tar.gz", "sha256": "aaaa", "kind": "archive"}
					]
				}
			]`),
			filename:   "go1.23.5.linux-amd64.tar.gz",
			wantErr:    true,
			errContain: "not found in Go JSON checksums",
		},
		{
			name: "invalid JSON falls back to unknown format",
			// Invalid JSON is detected as unknown format
			content:    []byte(`[invalid json`),
			filename:   "go1.23.5.linux-amd64.tar.gz",
			wantErr:    true,
			errContain: "unknown or unsupported checksum file format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			algo, hash, err := ParseFile(tt.content, tt.filename)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAlgo, algo)
			assert.Equal(t, tt.wantHash, hash)
		})
	}
}
