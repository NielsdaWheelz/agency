package cobra

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWatchInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		want       time.Duration
		wantErr    bool
		errContain string
	}{
		{"valid_500ms", "500ms", 500 * time.Millisecond, false, ""},
		{"valid_1s", "1s", 1 * time.Second, false, ""},
		{"valid_2.5s", "2.5s", 2500 * time.Millisecond, false, ""},
		{"boundary_250ms", "250ms", 250 * time.Millisecond, false, ""},
		{"boundary_5s", "5s", 5 * time.Second, false, ""},
		{"below_minimum", "100ms", 0, true, "between 250ms and 5s"},
		{"above_maximum", "10s", 0, true, "between 250ms and 5s"},
		{"no_unit_rejected", "500", 0, true, "not a valid duration"},
		{"garbage_rejected", "abc", 0, true, "not a valid duration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, err := parseWatchInterval(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, d)
		})
	}
}

// These tests use executeCmd (NewRootCmd) which binds package-level flag vars;
// must not run in parallel with other executeCmd callers.

func TestAgentLS_WatchAndJSONMutualExclusion(t *testing.T) {
	_, _, err := executeCmd("agent", "ls", "--watch", "--json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--watch and --json cannot be used together")
}

func TestWorktreeLS_WatchAndJSONMutualExclusion(t *testing.T) {
	_, _, err := executeCmd("worktree", "ls", "--watch", "--json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--watch and --json cannot be used together")
}
