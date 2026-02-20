package checksum

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
)

// Digest represents a hex-encoded hash digest (e.g., SHA256).
type Digest string

// Algorithm represents a checksum hash algorithm.
type Algorithm string

const (
	AlgorithmSHA256 Algorithm = "sha256"
	AlgorithmSHA512 Algorithm = "sha512"
)

// Parse parses a checksum value in format "algorithm:hash".
// Returns the algorithm and hash digest.
func Parse(value string) (Algorithm, Digest, error) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid checksum format: expected 'algorithm:hash', got %q", value)
	}

	algorithm := Algorithm(parts[0])
	hashValue := Digest(parts[1])

	switch algorithm {
	case AlgorithmSHA256, AlgorithmSHA512:
		// Valid algorithm
	default:
		return "", "", fmt.Errorf("unsupported hash algorithm: %s", algorithm)
	}

	return algorithm, hashValue, nil
}

// ExtractHash extracts the hash digest from a checksum value string (e.g., "sha256:abc123").
// For URL-based checksum, pass an empty string (hash is fetched during verification).
func ExtractHash(value string) Digest {
	if value == "" {
		return ""
	}
	_, hashValue, err := Parse(value)
	if err != nil {
		return ""
	}
	return hashValue
}

// Calculate calculates the checksum of a file using the given algorithm.
func Calculate(filePath string, algorithm Algorithm) (Digest, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	return CalculateFromReader(f, algorithm)
}

// CalculateFromReader calculates the checksum from a reader using the given algorithm.
func CalculateFromReader(r io.Reader, algorithm Algorithm) (Digest, error) {
	var h hash.Hash
	switch algorithm {
	case AlgorithmSHA256:
		h = sha256.New()
	case AlgorithmSHA512:
		h = sha512.New()
	default:
		return "", fmt.Errorf("unsupported hash algorithm: %s", algorithm)
	}

	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("failed to read data: %w", err)
	}

	return Digest(hex.EncodeToString(h.Sum(nil))), nil
}

// Verify verifies the checksum of a file.
func Verify(filePath string, algorithm Algorithm, expectedHash Digest) error {
	actualHash, err := Calculate(filePath, algorithm)
	if err != nil {
		return err
	}

	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

// DetectAlgorithm detects the hash algorithm from the hash length.
func DetectAlgorithm(hashValue string) Algorithm {
	switch len(hashValue) {
	case 64: // SHA256
		return AlgorithmSHA256
	case 128: // SHA512
		return AlgorithmSHA512
	default:
		return ""
	}
}

// NewHash returns a new hash.Hash for the given algorithm.
func NewHash(algorithm Algorithm) (hash.Hash, error) {
	switch algorithm {
	case AlgorithmSHA256:
		return sha256.New(), nil
	case AlgorithmSHA512:
		return sha512.New(), nil
	default:
		return nil, fmt.Errorf("unsupported hash algorithm: %s", algorithm)
	}
}
