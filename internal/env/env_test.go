package env

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/terassyi/tomei/internal/resource"
)

func TestGenerate(t *testing.T) {
	home, _ := os.UserHomeDir()
	userBinDir := home + "/.local/bin"

	tests := []struct {
		name            string
		runtimes        map[string]*resource.RuntimeState
		shell           ShellType
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:     "no runtimes - posix",
			runtimes: map[string]*resource.RuntimeState{},
			shell:    ShellPosix,
			wantContains: []string{
				`export PATH="$HOME/.local/bin:$PATH"`,
			},
		},
		{
			name:     "no runtimes - fish",
			runtimes: map[string]*resource.RuntimeState{},
			shell:    ShellFish,
			wantContains: []string{
				`fish_add_path "$HOME/.local/bin"`,
			},
		},
		{
			name: "go runtime - posix",
			runtimes: map[string]*resource.RuntimeState{
				"go": {
					Version:     "1.25.6",
					BinDir:      home + "/go/bin",
					ToolBinPath: home + "/go/bin",
					Env: map[string]string{
						"GOROOT": home + "/.local/share/tomei/runtimes/go/1.25.6",
						"GOBIN":  home + "/go/bin",
					},
				},
			},
			shell: ShellPosix,
			wantContains: []string{
				`export GOROOT="$HOME/.local/share/tomei/runtimes/go/1.25.6"`,
				`export GOBIN="$HOME/go/bin"`,
				`$HOME/.local/bin`,
				`$HOME/go/bin`,
				`export PATH=`,
			},
		},
		{
			name: "go runtime - fish",
			runtimes: map[string]*resource.RuntimeState{
				"go": {
					Version:     "1.25.6",
					BinDir:      home + "/go/bin",
					ToolBinPath: home + "/go/bin",
					Env: map[string]string{
						"GOROOT": home + "/.local/share/tomei/runtimes/go/1.25.6",
					},
				},
			},
			shell: ShellFish,
			wantContains: []string{
				`set -gx GOROOT "$HOME/.local/share/tomei/runtimes/go/1.25.6"`,
				`fish_add_path`,
				`$HOME/go/bin`,
			},
		},
		{
			name: "multiple runtimes with deduplicated PATH",
			runtimes: map[string]*resource.RuntimeState{
				"go": {
					Version:     "1.25.6",
					BinDir:      home + "/go/bin",
					ToolBinPath: home + "/go/bin",
					Env: map[string]string{
						"GOROOT": home + "/.local/share/tomei/runtimes/go/1.25.6",
					},
				},
				"rust": {
					Version:     "1.80.0",
					BinDir:      home + "/.cargo/bin",
					ToolBinPath: home + "/.cargo/bin",
					Env: map[string]string{
						"CARGO_HOME":  home + "/.cargo",
						"RUSTUP_HOME": home + "/.rustup",
					},
				},
			},
			shell: ShellPosix,
			wantContains: []string{
				`export GOROOT=`,
				`export CARGO_HOME=`,
				`export RUSTUP_HOME=`,
				`$HOME/go/bin`,
				`$HOME/.cargo/bin`,
			},
		},
		{
			name: "BinDir different from ToolBinPath",
			runtimes: map[string]*resource.RuntimeState{
				"go": {
					Version:     "1.25.6",
					BinDir:      home + "/.local/share/tomei/runtimes/go/1.25.6/bin",
					ToolBinPath: home + "/go/bin",
					Env:         map[string]string{},
				},
			},
			shell: ShellPosix,
			wantContains: []string{
				`$HOME/.local/share/tomei/runtimes/go/1.25.6/bin`,
				`$HOME/go/bin`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewFormatter(tt.shell)
			lines := Generate(tt.runtimes, userBinDir, f)
			output := joinLines(lines)

			for _, want := range tt.wantContains {
				assert.Contains(t, output, want)
			}
			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, output, notWant)
			}
		})
	}
}

func TestToShellPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "path under home",
			input: home + "/go/bin",
			want:  "$HOME/go/bin",
		},
		{
			name:  "home directory itself",
			input: home,
			want:  "$HOME",
		},
		{
			name:  "path not under home",
			input: "/opt/local/bin",
			want:  "/opt/local/bin",
		},
		{
			name:  "empty path",
			input: "",
			want:  "",
		},
		{
			name:  "nested path under home",
			input: home + "/.local/share/tomei/runtimes/go/1.25.6",
			want:  "$HOME/.local/share/tomei/runtimes/go/1.25.6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toShellPath(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDedupStrings(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "no duplicates",
			input: []string{"a", "b", "c"},
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "with duplicates preserves order",
			input: []string{"a", "b", "a", "c", "b"},
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "all same",
			input: []string{"a", "a", "a"},
			want:  []string{"a"},
		},
		{
			name:  "empty",
			input: []string{},
			want:  []string{},
		},
		{
			name:  "nil",
			input: nil,
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupStrings(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func joinLines(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}
