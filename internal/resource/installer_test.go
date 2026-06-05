package resource

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestInstallerSpec_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spec    InstallerSpec
		wantErr string
	}{
		{
			name:    "empty type",
			spec:    InstallerSpec{},
			wantErr: "type is required",
		},
		{
			name: "invalid type",
			spec: InstallerSpec{
				Type: "invalid",
			},
			wantErr: "type must be 'download' or 'delegation'",
		},
		{
			name: "valid download type",
			spec: InstallerSpec{
				Type: InstallTypeDownload,
			},
			wantErr: "",
		},
		{
			name: "delegation without commands",
			spec: InstallerSpec{
				Type: InstallTypeDelegation,
			},
			wantErr: "commands is required for delegation type",
		},
		{
			name: "valid delegation with commands",
			spec: InstallerSpec{
				Type: InstallTypeDelegation,
				Commands: &CommandsSpec{
					Install: []string{"go install {{.Package}}@{{.Version}}"},
				},
			},
			wantErr: "",
		},
		{
			name: "both runtimeRef and toolRef",
			spec: InstallerSpec{
				Type:       InstallTypeDelegation,
				RuntimeRef: "go",
				ToolRef:    "pnpm",
				Commands: &CommandsSpec{
					Install: []string{"some command"},
				},
			},
			wantErr: "cannot specify both runtimeRef and toolRef",
		},
		{
			name: "valid with runtimeRef",
			spec: InstallerSpec{
				Type:       InstallTypeDelegation,
				RuntimeRef: "go",
				Commands: &CommandsSpec{
					Install: []string{"go install {{.Package}}@{{.Version}}"},
				},
			},
			wantErr: "",
		},
		{
			name: "valid with toolRef",
			spec: InstallerSpec{
				Type:    InstallTypeDelegation,
				ToolRef: "pnpm",
				Commands: &CommandsSpec{
					Install: []string{"pnpm add -g {{.Package}}@{{.Version}}"},
				},
			},
			wantErr: "",
		},
		{
			name: "delegation with dependsOn",
			spec: InstallerSpec{
				Type:      InstallTypeDelegation,
				ToolRef:   "krew",
				DependsOn: []string{"kubectl"},
				Commands: &CommandsSpec{
					Install: []string{"krew install {{.Package}}"},
				},
			},
			wantErr: "",
		},
		{
			name: "download with dependsOn",
			spec: InstallerSpec{
				Type:      InstallTypeDownload,
				DependsOn: []string{"kubectl"},
			},
			wantErr: "",
		},
		{
			name: "dependsOn overlaps toolRef (tolerated)",
			spec: InstallerSpec{
				Type:      InstallTypeDelegation,
				ToolRef:   "krew",
				DependsOn: []string{"krew"},
				Commands: &CommandsSpec{
					Install: []string{"krew install {{.Package}}"},
				},
			},
			wantErr: "",
		},
		{
			name: "dependsOn has duplicates",
			spec: InstallerSpec{
				Type:      InstallTypeDelegation,
				DependsOn: []string{"kubectl", "kubectl"},
				Commands: &CommandsSpec{
					Install: []string{"some command"},
				},
			},
			wantErr: "dependsOn contains duplicate entry",
		},
		{
			name: "dependsOn has empty string",
			spec: InstallerSpec{
				Type:      InstallTypeDownload,
				DependsOn: []string{""},
			},
			wantErr: "dependsOn must not contain empty strings",
		},
		{
			name: "valid delegation with binDir tilde",
			spec: InstallerSpec{
				Type:   InstallTypeDelegation,
				BinDir: "~/.krew/bin",
				Commands: &CommandsSpec{
					Install: []string{"krew install {{.Package}}"},
				},
			},
			wantErr: "",
		},
		{
			name: "valid delegation with binDir absolute",
			spec: InstallerSpec{
				Type:   InstallTypeDelegation,
				BinDir: "/opt/bin",
				Commands: &CommandsSpec{
					Install: []string{"some command"},
				},
			},
			wantErr: "",
		},
		{
			name: "binDir with relative path",
			spec: InstallerSpec{
				Type:   InstallTypeDelegation,
				BinDir: "relative/path",
				Commands: &CommandsSpec{
					Install: []string{"some command"},
				},
			},
			wantErr: "binDir must start with",
		},
		{
			name: "binDir on download type rejected",
			spec: InstallerSpec{
				Type:   InstallTypeDownload,
				BinDir: "~/.some/bin",
			},
			wantErr: "binDir is not supported for download type",
		},
		{
			name: "valid with minimumReleaseAge",
			spec: InstallerSpec{
				Type:              InstallTypeDownload,
				MinimumReleaseAge: "168h",
			},
			wantErr: "",
		},
		{
			name: "invalid minimumReleaseAge surfaces from Validate",
			spec: InstallerSpec{
				Type:              InstallTypeDownload,
				MinimumReleaseAge: "7d",
			},
			wantErr: "minimumReleaseAge",
		},
		{
			name: "negative minimumReleaseAge rejected from Validate",
			spec: InstallerSpec{
				Type:              InstallTypeDownload,
				MinimumReleaseAge: "-1h",
			},
			wantErr: "minimumReleaseAge must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.spec.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestInstallerSpec_Dependencies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec InstallerSpec
		want []Ref
	}{
		{
			name: "no dependencies",
			spec: InstallerSpec{
				Type: InstallTypeDownload,
			},
			want: nil,
		},
		{
			name: "runtimeRef dependency",
			spec: InstallerSpec{
				Type:       InstallTypeDelegation,
				RuntimeRef: "go",
			},
			want: []Ref{{Kind: KindRuntime, Name: "go"}},
		},
		{
			name: "toolRef dependency",
			spec: InstallerSpec{
				Type:    InstallTypeDelegation,
				ToolRef: "pnpm",
			},
			want: []Ref{{Kind: KindTool, Name: "pnpm"}},
		},
		{
			name: "toolRef and dependsOn",
			spec: InstallerSpec{
				Type:      InstallTypeDelegation,
				ToolRef:   "krew",
				DependsOn: []string{"kubectl"},
			},
			want: []Ref{
				{Kind: KindTool, Name: "krew"},
				{Kind: KindTool, Name: "kubectl"},
			},
		},
		{
			name: "dependsOn only",
			spec: InstallerSpec{
				Type:      InstallTypeDownload,
				DependsOn: []string{"kubectl", "helm"},
			},
			want: []Ref{
				{Kind: KindTool, Name: "kubectl"},
				{Kind: KindTool, Name: "helm"},
			},
		},
		{
			name: "runtimeRef and dependsOn",
			spec: InstallerSpec{
				Type:       InstallTypeDelegation,
				RuntimeRef: "go",
				DependsOn:  []string{"gopls"},
			},
			want: []Ref{
				{Kind: KindRuntime, Name: "go"},
				{Kind: KindTool, Name: "gopls"},
			},
		},
		{
			name: "dependsOn overlaps toolRef (deduplicated)",
			spec: InstallerSpec{
				Type:      InstallTypeDelegation,
				ToolRef:   "krew",
				DependsOn: []string{"krew", "kubectl"},
			},
			want: []Ref{
				{Kind: KindTool, Name: "krew"},
				{Kind: KindTool, Name: "kubectl"},
			},
		},
		{
			name: "dependsOn empty list",
			spec: InstallerSpec{
				Type:      InstallTypeDownload,
				DependsOn: []string{},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.spec.Dependencies()
			if len(got) != len(tt.want) {
				t.Errorf("Dependencies() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Dependencies()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestInstallerSpec_ParsedMinimumReleaseAge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
		errSub  string
	}{
		{name: "empty disabled", input: "", want: 0},
		{name: "168h", input: "168h", want: 168 * time.Hour},
		{name: "leading plus accepted", input: "+168h", want: 168 * time.Hour},
		{name: "compound", input: "1h30m", want: 90 * time.Minute},
		{name: "0s explicit zero", input: "0s", want: 0},
		{name: "0 unitless accepted as zero", input: "0", want: 0},
		{name: "7d rejected", input: "7d", wantErr: true, errSub: "minimumReleaseAge"},
		{name: "uppercase H rejected", input: "168H", wantErr: true, errSub: "minimumReleaseAge"},
		{name: "leading whitespace rejected", input: "  168h", wantErr: true, errSub: "minimumReleaseAge"},
		{name: "garbage rejected", input: "garbage", wantErr: true, errSub: "minimumReleaseAge"},
		{name: "negative hour", input: "-1h", wantErr: true, errSub: "minimumReleaseAge must be non-negative"},
		{name: "negative ns", input: "-1ns", wantErr: true, errSub: "minimumReleaseAge must be non-negative"},
		{name: "shell metacharacters rejected", input: "168h; rm -rf /", wantErr: true, errSub: "minimumReleaseAge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := InstallerSpec{MinimumReleaseAge: tt.input}
			got, err := s.ParsedMinimumReleaseAge()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParsedMinimumReleaseAge(%q) expected error, got nil", tt.input)
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("ParsedMinimumReleaseAge(%q) error = %q, want containing %q", tt.input, err.Error(), tt.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsedMinimumReleaseAge(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParsedMinimumReleaseAge(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateBuiltinInstallerOverrides(t *testing.T) {
	t.Parallel()
	mkInstaller := func(name string, spec *InstallerSpec) *Installer {
		return &Installer{
			BaseResource: BaseResource{
				APIVersion:   GroupVersion,
				ResourceKind: KindInstaller,
				Metadata:     Metadata{Name: name},
			},
			InstallerSpec: spec,
		}
	}
	tests := []struct {
		name      string
		resources []Resource
		wantErr   string
	}{
		{name: "nil slice", resources: nil},
		{name: "empty slice", resources: []Resource{}},
		{
			name: "aqua with download type",
			resources: []Resource{
				mkInstaller("aqua", &InstallerSpec{Type: InstallTypeDownload}),
			},
		},
		{
			name: "aqua with download type and minimumReleaseAge",
			resources: []Resource{
				mkInstaller("aqua", &InstallerSpec{Type: InstallTypeDownload, MinimumReleaseAge: "168h"}),
			},
		},
		{
			name: "aqua with delegation type rejected",
			resources: []Resource{
				mkInstaller("aqua", &InstallerSpec{Type: InstallTypeDelegation}),
			},
			wantErr: `installer "aqua" overrides builtin and must use type "download"`,
		},
		{
			// Locks in the deterministic ordering documented on
			// builtinInstallerOrder: aqua is checked before download, so
			// when both overrides are misused at once the error names
			// "aqua" first regardless of map iteration order. Extra
			// "download" absence assertion runs after this row to prove
			// the function short-circuits on the first violation rather
			// than aggregating both.
			name: "both aqua and download misused — aqua reported first",
			resources: []Resource{
				mkInstaller("download", &InstallerSpec{Type: InstallTypeDelegation}),
				mkInstaller("aqua", &InstallerSpec{Type: InstallTypeDelegation}),
			},
			wantErr: `installer "aqua"`,
		},
		{
			name: "download with delegation type rejected",
			resources: []Resource{
				mkInstaller("download", &InstallerSpec{Type: InstallTypeDelegation}),
			},
			wantErr: `installer "download" overrides builtin and must use type "download"`,
		},
		{
			name: "unrelated installer name with delegation tolerated",
			resources: []Resource{
				mkInstaller("foo", &InstallerSpec{Type: InstallTypeDelegation}),
			},
		},
		{
			name: "aqua with nil InstallerSpec rejected (still shadows builtin)",
			resources: []Resource{
				mkInstaller("aqua", nil),
			},
			wantErr: `installer "aqua" overrides builtin and must declare a spec`,
		},
		{
			name: "nil resource entry is skipped",
			resources: []Resource{
				nil,
				mkInstaller("aqua", &InstallerSpec{Type: InstallTypeDownload}),
			},
		},
		{
			// Two Installer/aqua occurrences (e.g., split across manifests).
			// A valid one must NOT mask an invalid sibling — every
			// occurrence is checked. Order chosen so the invalid one
			// appears second to also defend against "last write wins"
			// regressions on a map-based dedup.
			name: "duplicate aqua: invalid sibling must still error",
			resources: []Resource{
				mkInstaller("aqua", &InstallerSpec{Type: InstallTypeDownload}),
				mkInstaller("aqua", &InstallerSpec{Type: InstallTypeDelegation}),
			},
			wantErr: `installer "aqua" overrides builtin and must use type "download"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateBuiltinInstallerOverrides(tt.resources)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateBuiltinInstallerOverrides() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateBuiltinInstallerOverrides() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateBuiltinInstallerOverrides() error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}

	// Separate from the table because it asserts NotContains, not Contains:
	// when both builtins are misused, the function short-circuits on
	// "aqua" (the first entry in builtinInstallerOrder) and never reports
	// "download". This locks in single-error semantics.
	t.Run("both misused short-circuits on aqua, does not also report download", func(t *testing.T) {
		t.Parallel()
		err := ValidateBuiltinInstallerOverrides([]Resource{
			mkInstaller("download", &InstallerSpec{Type: InstallTypeDelegation}),
			mkInstaller("aqua", &InstallerSpec{Type: InstallTypeDelegation}),
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		// The error must mention the aqua override but NOT the download
		// override — proving the function short-circuits on the first
		// builtin name in builtinInstallerOrder rather than aggregating.
		// Care: the literal substring "download" legitimately appears in
		// the required-type slot of the error format string
		// (`... must use type "download", got "delegation"`), so we
		// assert on the full `installer "<name>"` prefix instead of bare
		// names.
		if !strings.Contains(err.Error(), `installer "aqua"`) {
			t.Errorf("error should mention `installer \"aqua\"`: %v", err)
		}
		if strings.Contains(err.Error(), `installer "download"`) {
			t.Errorf("short-circuit broken: error must not also mention `installer \"download\"`: %v", err)
		}
	})
}

// TestBuiltinInstallerInvariants pins the contract between
// builtinInstallerTypes (map) and builtinInstallerOrder (slice): every
// name in the slice must appear in the map, and the slice must cover the
// map exactly. A drift would silently skip override validation for the
// missing name.
func TestBuiltinInstallerInvariants(t *testing.T) {
	t.Parallel()
	if len(builtinInstallerOrder) != len(builtinInstallerTypes) {
		t.Fatalf("builtinInstallerOrder (len=%d) and builtinInstallerTypes (len=%d) must have the same length",
			len(builtinInstallerOrder), len(builtinInstallerTypes))
	}
	seen := make(map[string]struct{}, len(builtinInstallerOrder))
	for _, name := range builtinInstallerOrder {
		if _, ok := builtinInstallerTypes[name]; !ok {
			t.Errorf("builtinInstallerOrder entry %q has no corresponding key in builtinInstallerTypes", name)
		}
		if _, dup := seen[name]; dup {
			t.Errorf("builtinInstallerOrder contains duplicate entry %q", name)
		}
		seen[name] = struct{}{}
	}
	for name := range builtinInstallerTypes {
		if _, ok := seen[name]; !ok {
			t.Errorf("builtinInstallerTypes key %q is missing from builtinInstallerOrder", name)
		}
	}
}

// TestInstallerSpec_MinimumReleaseAge_JSONRoundTrip locks in the
// omitempty / round-trip behavior of the minimumReleaseAge JSON tag.
func TestInstallerSpec_MinimumReleaseAge_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	t.Run("set value round-trips", func(t *testing.T) {
		t.Parallel()
		original := InstallerSpec{Type: InstallTypeDownload, MinimumReleaseAge: "168h"}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if !strings.Contains(string(data), `"minimumReleaseAge":"168h"`) {
			t.Errorf("Marshal output should contain the field; got %s", data)
		}
		var got InstallerSpec
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if got.MinimumReleaseAge != "168h" {
			t.Errorf("after round-trip MinimumReleaseAge = %q, want %q", got.MinimumReleaseAge, "168h")
		}
	})
	t.Run("empty value is omitted via omitempty", func(t *testing.T) {
		t.Parallel()
		data, err := json.Marshal(InstallerSpec{Type: InstallTypeDownload})
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(data), "minimumReleaseAge") {
			t.Errorf("empty MinimumReleaseAge should be omitted; got %s", data)
		}
	})
	t.Run("unmarshal absent field yields empty string", func(t *testing.T) {
		t.Parallel()
		var got InstallerSpec
		if err := json.Unmarshal([]byte(`{"type":"download"}`), &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if got.MinimumReleaseAge != "" {
			t.Errorf("absent MinimumReleaseAge should unmarshal to empty string; got %q", got.MinimumReleaseAge)
		}
	})
}
