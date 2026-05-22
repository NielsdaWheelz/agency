package commands

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestWorktreeTarget_UsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantMessage string
	}{
		{
			name:        "missing target",
			args:        nil,
			wantMessage: "specify a worktree ref",
		},
		{
			name:        "bare pr",
			args:        []string{"wt-1", "pr"},
			wantMessage: "use 'agency worktree <worktree-ref> pr sync' or 'agency worktree <worktree-ref> pr merge'",
		},
		{
			name:        "unknown target action",
			args:        []string{"wt-1", "nope"},
			wantMessage: "unknown command \"nope\" for \"agency worktree\"",
		},
		{
			name:        "unknown pr action",
			args:        []string{"wt-1", "pr", "nope"},
			wantMessage: "unknown command \"nope\" for \"agency worktree wt-1 pr\"",
		},
		{
			name:        "too many args",
			args:        []string{"wt-1", "path", "extra"},
			wantMessage: "unknown command \"path\" for \"agency worktree\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := WorktreeTarget(context.Background(), nil, nil, "", WorktreeTargetOpts{Args: tt.args}, &bytes.Buffer{}, &bytes.Buffer{})

			require.Error(t, err)
			if got := errors.GetCode(err); got != errors.EUsage {
				t.Fatalf("error code = %s, want %s", got, errors.EUsage)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("error = %q, want message containing %q", err.Error(), tt.wantMessage)
			}
		})
	}
}
