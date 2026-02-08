package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseShellType(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ShellType
		wantErr bool
	}{
		{
			name:  "posix",
			input: "posix",
			want:  ShellPosix,
		},
		{
			name:  "fish",
			input: "fish",
			want:  ShellFish,
		},
		{
			name:  "empty defaults to posix",
			input: "",
			want:  ShellPosix,
		},
		{
			name:  "bash maps to posix",
			input: "bash",
			want:  ShellPosix,
		},
		{
			name:  "sh maps to posix",
			input: "sh",
			want:  ShellPosix,
		},
		{
			name:  "zsh maps to posix",
			input: "zsh",
			want:  ShellPosix,
		},
		{
			name:    "unsupported shell",
			input:   "powershell",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseShellType(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported shell type")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPosixFormatter(t *testing.T) {
	f := posixFormatter{}

	t.Run("ExportVar", func(t *testing.T) {
		got := f.ExportVar("GOROOT", "$HOME/.local/share/tomei/runtimes/go/1.25.6")
		assert.Equal(t, `export GOROOT="$HOME/.local/share/tomei/runtimes/go/1.25.6"`, got)
	})

	t.Run("ExportPath", func(t *testing.T) {
		got := f.ExportPath([]string{"$HOME/.local/bin", "$HOME/go/bin"})
		assert.Equal(t, `export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`, got)
	})

	t.Run("Ext", func(t *testing.T) {
		assert.Equal(t, ".sh", f.Ext())
	})
}

func TestFishFormatter(t *testing.T) {
	f := fishFormatter{}

	t.Run("ExportVar", func(t *testing.T) {
		got := f.ExportVar("GOROOT", "$HOME/.local/share/tomei/runtimes/go/1.25.6")
		assert.Equal(t, `set -gx GOROOT "$HOME/.local/share/tomei/runtimes/go/1.25.6"`, got)
	})

	t.Run("ExportPath", func(t *testing.T) {
		got := f.ExportPath([]string{"$HOME/.local/bin", "$HOME/go/bin"})
		assert.Equal(t, `fish_add_path "$HOME/.local/bin" "$HOME/go/bin"`, got)
	})

	t.Run("Ext", func(t *testing.T) {
		assert.Equal(t, ".fish", f.Ext())
	})
}

func TestNewFormatter(t *testing.T) {
	tests := []struct {
		name      string
		shellType ShellType
		wantType  string
	}{
		{
			name:      "posix",
			shellType: ShellPosix,
			wantType:  ".sh",
		},
		{
			name:      "fish",
			shellType: ShellFish,
			wantType:  ".fish",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewFormatter(tt.shellType)
			assert.Equal(t, tt.wantType, f.Ext())
		})
	}
}
