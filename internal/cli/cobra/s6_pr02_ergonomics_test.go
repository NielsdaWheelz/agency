package cobra

import (
	"context"
	"io"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/commands"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
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
		{name: "run repo", newCmd: newRunCmd, flagName: "repo", shorthand: "r"},
		{name: "run open", newCmd: newRunCmd, flagName: "open", shorthand: "o"},
		{name: "clean repo", newCmd: newCleanCmd, flagName: "repo", shorthand: "r"},
		{name: "clean yes", newCmd: newCleanCmd, flagName: "yes", shorthand: "y"},
		{name: "resume repo", newCmd: newResumeCmd, flagName: "repo", shorthand: "r"},
		{name: "resume yes", newCmd: newResumeCmd, flagName: "yes", shorthand: "y"},
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

func TestRunCmd_OpenSetsDetachedEquivalent(t *testing.T) {
	original := runCommand
	t.Cleanup(func() { runCommand = original })

	var captured commands.RunOpts
	runCommand = func(_ context.Context, _ agencyexec.CommandRunner, _ fs.FS, _ string, opts commands.RunOpts, _ io.Writer, _ io.Writer) error {
		captured = opts
		return nil
	}

	cmd := newRunCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--name", "demo", "--open"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.True(t, captured.Open)
	assert.False(t, captured.Attach, "--open must force detached-equivalent behavior")
}

func TestRunCmd_DefaultStillAttachesWithoutOpen(t *testing.T) {
	original := runCommand
	t.Cleanup(func() { runCommand = original })

	var captured commands.RunOpts
	runCommand = func(_ context.Context, _ agencyexec.CommandRunner, _ fs.FS, _ string, opts commands.RunOpts, _ io.Writer, _ io.Writer) error {
		captured = opts
		return nil
	}

	cmd := newRunCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--name", "demo"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.False(t, captured.Open)
	assert.True(t, captured.Attach)
}
