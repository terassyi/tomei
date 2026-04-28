package system

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOSRelease_Ubuntu(t *testing.T) {
	t.Parallel()
	content := `NAME="Ubuntu"
ID=ubuntu
ID_LIKE=debian
VERSION_ID="22.04"
PRETTY_NAME="Ubuntu 22.04.4 LTS"
`
	info, err := parseOSRelease(content)
	require.NoError(t, err)
	assert.Equal(t, "ubuntu", info.ID)
	assert.Equal(t, []string{"debian"}, info.IDLike)
	assert.Equal(t, "22.04", info.VersionID)
	assert.Equal(t, "Ubuntu 22.04.4 LTS", info.Name)
}

func TestParseOSRelease_Debian(t *testing.T) {
	t.Parallel()
	content := `NAME="Debian GNU/Linux"
ID=debian
VERSION_ID="12"
PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
`
	info, err := parseOSRelease(content)
	require.NoError(t, err)
	assert.Equal(t, "debian", info.ID)
	assert.Empty(t, info.IDLike)
	assert.Equal(t, "12", info.VersionID)
}

func TestParseOSRelease_Fedora(t *testing.T) {
	t.Parallel()
	content := `NAME="Fedora Linux"
ID=fedora
VERSION_ID="40"
PRETTY_NAME="Fedora Linux 40 (Workstation Edition)"
`
	info, err := parseOSRelease(content)
	require.NoError(t, err)
	assert.Equal(t, "fedora", info.ID)
	assert.Empty(t, info.IDLike)
}

func TestParseOSRelease_LinuxMint(t *testing.T) {
	t.Parallel()
	content := `NAME="Linux Mint"
ID=linuxmint
ID_LIKE="ubuntu debian"
VERSION_ID="21.3"
PRETTY_NAME="Linux Mint 21.3"
`
	info, err := parseOSRelease(content)
	require.NoError(t, err)
	assert.Equal(t, "linuxmint", info.ID)
	assert.Equal(t, []string{"ubuntu", "debian"}, info.IDLike)
}

func TestParseOSRelease_RockyLinux(t *testing.T) {
	t.Parallel()
	content := `NAME="Rocky Linux"
ID="rocky"
ID_LIKE="rhel centos fedora"
VERSION_ID="9.3"
PRETTY_NAME="Rocky Linux 9.3 (Blue Onyx)"
`
	info, err := parseOSRelease(content)
	require.NoError(t, err)
	assert.Equal(t, "rocky", info.ID)
	assert.Equal(t, []string{"rhel", "centos", "fedora"}, info.IDLike)
}

func TestParseOSRelease_QuotedValues(t *testing.T) {
	t.Parallel()
	content := `ID="ubuntu"
ID_LIKE="debian"
VERSION_ID="22.04"
PRETTY_NAME="Ubuntu 22.04 LTS"
`
	info, err := parseOSRelease(content)
	require.NoError(t, err)
	assert.Equal(t, "ubuntu", info.ID)
	assert.Equal(t, []string{"debian"}, info.IDLike)
	assert.Equal(t, "22.04", info.VersionID)
}

func TestParseOSRelease_EmptyLines(t *testing.T) {
	t.Parallel()
	content := `# This is a comment
ID=ubuntu

# Another comment
ID_LIKE=debian
`
	info, err := parseOSRelease(content)
	require.NoError(t, err)
	assert.Equal(t, "ubuntu", info.ID)
	assert.Equal(t, []string{"debian"}, info.IDLike)
}

func TestParseOSRelease_Empty(t *testing.T) {
	t.Parallel()
	_, err := parseOSRelease("")
	require.Error(t, err)
}

func TestDistroInfo_IDs_WithIDLike(t *testing.T) {
	t.Parallel()
	info := &DistroInfo{
		ID:     "linuxmint",
		IDLike: []string{"ubuntu", "debian"},
	}
	assert.Equal(t, []string{"linuxmint", "ubuntu", "debian"}, info.IDs())
}

func TestDistroInfo_IDs_WithoutIDLike(t *testing.T) {
	t.Parallel()
	info := &DistroInfo{
		ID: "debian",
	}
	assert.Equal(t, []string{"debian"}, info.IDs())
}

func TestDetectDistro_FallbackPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Only /usr/lib/os-release exists (no /etc/os-release)
	usrLibDir := filepath.Join(dir, "usr", "lib")
	require.NoError(t, os.MkdirAll(usrLibDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(usrLibDir, "os-release"), []byte(`ID=alpine
PRETTY_NAME="Alpine Linux"
`), 0o644))

	info, err := detectDistroFrom(dir)
	require.NoError(t, err)
	assert.Equal(t, "alpine", info.ID)
}

func TestDetectDistro_PrimaryPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Both exist — /etc/os-release should take priority
	etcDir := filepath.Join(dir, "etc")
	require.NoError(t, os.MkdirAll(etcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(etcDir, "os-release"), []byte(`ID=ubuntu
`), 0o644))

	usrLibDir := filepath.Join(dir, "usr", "lib")
	require.NoError(t, os.MkdirAll(usrLibDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(usrLibDir, "os-release"), []byte(`ID=debian
`), 0o644))

	info, err := detectDistroFrom(dir)
	require.NoError(t, err)
	assert.Equal(t, "ubuntu", info.ID)
}

func TestDetectDistro_NeitherExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := detectDistroFrom(dir)
	require.Error(t, err)
}
