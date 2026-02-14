package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/terassyi/tomei/internal/resource"
)

func TestValidateUserState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		state        *UserState
		wantValid    bool
		wantWarnings int
	}{
		{
			name: "valid state",
			state: &UserState{
				Version: Version,
				Tools: map[string]*resource.ToolState{
					"gh": {Version: "2.86.0"},
				},
				Runtimes: map[string]*resource.RuntimeState{
					"go": {Version: "1.25.1", InstallPath: "/path/to/go"},
				},
			},
			wantValid:    true,
			wantWarnings: 0,
		},
		{
			name:         "empty version",
			state:        &UserState{},
			wantValid:    true,
			wantWarnings: 1,
		},
		{
			name: "unknown version",
			state: &UserState{
				Version: "999",
			},
			wantValid:    true,
			wantWarnings: 1,
		},
		{
			name: "tool with empty version",
			state: &UserState{
				Version: Version,
				Tools: map[string]*resource.ToolState{
					"gh": {Version: ""},
				},
			},
			wantValid:    true,
			wantWarnings: 1,
		},
		{
			name: "runtime with empty version",
			state: &UserState{
				Version: Version,
				Runtimes: map[string]*resource.RuntimeState{
					"go": {Version: "", InstallPath: "/path/to/go"},
				},
			},
			wantValid:    true,
			wantWarnings: 1,
		},
		{
			name: "runtime with empty installPath",
			state: &UserState{
				Version: Version,
				Runtimes: map[string]*resource.RuntimeState{
					"go": {Version: "1.25.1", InstallPath: ""},
				},
			},
			wantValid:    true,
			wantWarnings: 1,
		},
		{
			name: "multiple warnings",
			state: &UserState{
				Version: "",
				Tools: map[string]*resource.ToolState{
					"gh": {Version: ""},
				},
				Runtimes: map[string]*resource.RuntimeState{
					"go": {Version: "", InstallPath: ""},
				},
			},
			wantValid:    true,
			wantWarnings: 4, // version + tools.gh.version + runtimes.go.version + runtimes.go.installPath
		},
		{
			name: "nil maps are valid",
			state: &UserState{
				Version: Version,
			},
			wantValid:    true,
			wantWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ValidateUserState(tt.state)
			assert.Equal(t, tt.wantValid, result.IsValid())
			assert.Len(t, result.Warnings, tt.wantWarnings)
		})
	}
}

func TestValidateSystemState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		state        *SystemState
		wantValid    bool
		wantWarnings int
	}{
		{
			name:         "valid state",
			state:        &SystemState{Version: Version},
			wantValid:    true,
			wantWarnings: 0,
		},
		{
			name:         "empty version",
			state:        &SystemState{},
			wantValid:    true,
			wantWarnings: 1,
		},
		{
			name:         "unknown version",
			state:        &SystemState{Version: "99"},
			wantValid:    true,
			wantWarnings: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ValidateSystemState(tt.state)
			assert.Equal(t, tt.wantValid, result.IsValid())
			assert.Len(t, result.Warnings, tt.wantWarnings)
		})
	}
}

func TestValidationError_String(t *testing.T) {
	t.Parallel()
	e := ValidationError{Field: "version", Message: "version is empty"}
	assert.Equal(t, "version: version is empty", e.String())
}

func TestValidationResult_HasWarnings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result *ValidationResult
		want   bool
	}{
		{
			name:   "no warnings",
			result: &ValidationResult{},
			want:   false,
		},
		{
			name: "has warnings",
			result: &ValidationResult{
				Warnings: []ValidationError{{Field: "test", Message: "warn"}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.result.HasWarnings())
		})
	}
}
