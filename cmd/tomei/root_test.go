package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyScope_Predicates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope          ApplyScope
		wantUser       bool
		wantPrivileged bool
		wantSystem     bool
		wantString     string
	}{
		{ScopeUser, true, false, false, "user"},
		{ScopeAll, true, true, true, "system"},
		{ScopeSystemOnly, false, true, true, "system-only"},
	}
	for _, tc := range cases {
		t.Run(tc.wantString, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.wantUser, tc.scope.IncludesUserKinds())
			assert.Equal(t, tc.wantPrivileged, tc.scope.IncludesPrivileged())
			assert.Equal(t, tc.wantSystem, tc.scope.IncludesSystemKinds())
			assert.Equal(t, tc.wantString, tc.scope.String())
		})
	}
}

func TestScopeFromFlags(t *testing.T) {
	// Not parallel: scopeFromFlags reads package globals.
	cases := []struct {
		name           string
		systemMode     bool
		systemOnlyMode bool
		want           ApplyScope
	}{
		{"default", false, false, ScopeUser},
		{"system", true, false, ScopeAll},
		{"system-only", false, true, ScopeSystemOnly},
	}
	prevSystem, prevSystemOnly := systemMode, systemOnlyMode
	t.Cleanup(func() { systemMode, systemOnlyMode = prevSystem, prevSystemOnly })
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			systemMode = tc.systemMode
			systemOnlyMode = tc.systemOnlyMode
			assert.Equal(t, tc.want, scopeFromFlags())
		})
	}
}

func TestPersistentPreRunE_MutualExclusion(t *testing.T) {
	// Not parallel: mutates package globals.
	prevSystem, prevSystemOnly := systemMode, systemOnlyMode
	t.Cleanup(func() { systemMode, systemOnlyMode = prevSystem, prevSystemOnly })

	systemMode = true
	systemOnlyMode = true
	err := rootCmd.PersistentPreRunE(rootCmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--system and --system-only are mutually exclusive")

	systemMode = true
	systemOnlyMode = false
	require.NoError(t, rootCmd.PersistentPreRunE(rootCmd, nil))

	systemMode = false
	systemOnlyMode = true
	require.NoError(t, rootCmd.PersistentPreRunE(rootCmd, nil))

	systemMode = false
	systemOnlyMode = false
	require.NoError(t, rootCmd.PersistentPreRunE(rootCmd, nil))
}
