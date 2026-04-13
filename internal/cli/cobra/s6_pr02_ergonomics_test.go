package cobra

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestS6PR02_HighTrafficFlagAliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		newCmd    func() *cobra.Command
		flagName  string
		shorthand string
	}{
		{name: "worktree pr sync repo", newCmd: newWorktreePRSyncCmd, flagName: "repo", shorthand: "r"},
		{name: "worktree pr sync json", newCmd: newWorktreePRSyncCmd, flagName: "json", shorthand: "j"},
		{name: "worktree merge repo", newCmd: newWorktreeMergeCmd, flagName: "repo", shorthand: "r"},
		{name: "worktree merge json", newCmd: newWorktreeMergeCmd, flagName: "json", shorthand: "j"},
		{name: "worktree merge yes", newCmd: newWorktreeMergeCmd, flagName: "yes", shorthand: "y"},
		{name: "worktree update repo", newCmd: newWorktreeUpdateCmd, flagName: "repo", shorthand: "r"},
		{name: "worktree update json", newCmd: newWorktreeUpdateCmd, flagName: "json", shorthand: "j"},
		{name: "worktree create open", newCmd: newWorktreeCreateCmd, flagName: "open", shorthand: "o"},
		{name: "worktree rm repo", newCmd: newWorktreeRmCmd, flagName: "repo", shorthand: "r"},
		{name: "worktree rm yes", newCmd: newWorktreeRmCmd, flagName: "yes", shorthand: "y"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			flag := tt.newCmd().Flags().Lookup(tt.flagName)
			require.NotNil(t, flag, "flag %q must exist", tt.flagName)
			assert.Equal(t, tt.shorthand, flag.Shorthand)
		})
	}
}
