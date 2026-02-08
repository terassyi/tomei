package checksum

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		wantAlgorithm Algorithm
		wantHash      string
		wantErr       bool
	}{
		{
			name:          "sha256",
			value:         "sha256:abc123",
			wantAlgorithm: AlgorithmSHA256,
			wantHash:      "abc123",
			wantErr:       false,
		},
		{
			name:          "sha512",
			value:         "sha512:def456",
			wantAlgorithm: AlgorithmSHA512,
			wantHash:      "def456",
			wantErr:       false,
		},
		{
			name:    "missing algorithm",
			value:   "abc123",
			wantErr: true,
		},
		{
			name:    "unsupported algorithm",
			value:   "md5:abc123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alg, hash, err := Parse(tt.value)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAlgorithm, alg)
			assert.Equal(t, tt.wantHash, hash)
		})
	}
}

func TestExtractHash(t *testing.T) {
	tests := []struct {
		name     string
		checksum *resource.Checksum
		want     string
	}{
		{
			name:     "nil checksum",
			checksum: nil,
			want:     "",
		},
		{
			name:     "empty checksum",
			checksum: &resource.Checksum{},
			want:     "",
		},
		{
			name: "url only",
			checksum: &resource.Checksum{
				URL: "https://example.com/checksums.txt",
			},
			want: "",
		},
		{
			name: "valid sha256",
			checksum: &resource.Checksum{
				Value: "sha256:abc123def456",
			},
			want: "abc123def456",
		},
		{
			name: "invalid format",
			checksum: &resource.Checksum{
				Value: "invalidformat",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractHash(tt.checksum)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCalculate(t *testing.T) {
	content := []byte("hello world")
	expectedSHA256 := fmt.Sprintf("%x", sha256.Sum256(content))
	expectedSHA512 := fmt.Sprintf("%x", sha512.Sum512(content))

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testfile")
	err := os.WriteFile(filePath, content, 0644)
	require.NoError(t, err)

	tests := []struct {
		name      string
		algorithm Algorithm
		want      string
		wantErr   bool
	}{
		{
			name:      "sha256",
			algorithm: AlgorithmSHA256,
			want:      expectedSHA256,
			wantErr:   false,
		},
		{
			name:      "sha512",
			algorithm: AlgorithmSHA512,
			want:      expectedSHA512,
			wantErr:   false,
		},
		{
			name:      "unsupported algorithm",
			algorithm: "md5",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Calculate(filePath, tt.algorithm)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCalculateFromReader(t *testing.T) {
	content := []byte("hello world")
	expectedSHA256 := fmt.Sprintf("%x", sha256.Sum256(content))

	hash, err := CalculateFromReader(bytes.NewReader(content), AlgorithmSHA256)
	require.NoError(t, err)
	assert.Equal(t, expectedSHA256, hash)
}

func TestVerify(t *testing.T) {
	content := []byte("hello world")
	expectedSHA256 := fmt.Sprintf("%x", sha256.Sum256(content))

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testfile")
	err := os.WriteFile(filePath, content, 0644)
	require.NoError(t, err)

	tests := []struct {
		name      string
		hash      string
		algorithm Algorithm
		wantErr   bool
	}{
		{
			name:      "valid checksum",
			hash:      expectedSHA256,
			algorithm: AlgorithmSHA256,
			wantErr:   false,
		},
		{
			name:      "invalid checksum",
			hash:      "0000000000000000000000000000000000000000000000000000000000000000",
			algorithm: AlgorithmSHA256,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Verify(filePath, tt.algorithm, tt.hash)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestDetectAlgorithm(t *testing.T) {
	tests := []struct {
		name string
		hash string
		want Algorithm
	}{
		{
			name: "sha256 length",
			hash: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			want: AlgorithmSHA256,
		},
		{
			name: "sha512 length",
			hash: "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
			want: AlgorithmSHA512,
		},
		{
			name: "unknown length",
			hash: "abc123",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectAlgorithm(tt.hash)
			assert.Equal(t, tt.want, got)
		})
	}
}
