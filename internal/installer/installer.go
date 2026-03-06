package installer

// InstallOption configures the installation.
type InstallOption func(*InstallConfig)

// InstallConfig holds installation configuration.
type InstallConfig struct {
	BinaryName    string // Binary name for placement and symlink (defaults to tool name)
	SrcBinaryName string // Binary name to search in archive (e.g., krew-linux_arm64); empty = BinaryName
	Force         bool   // Replace existing binary even if hash differs
}

// WithBinaryName sets the binary name to look for in the archive.
func WithBinaryName(name string) InstallOption {
	return func(c *InstallConfig) {
		c.BinaryName = name
	}
}

// WithForce allows replacing existing binary with different hash.
func WithForce(force bool) InstallOption {
	return func(c *InstallConfig) {
		c.Force = force
	}
}
