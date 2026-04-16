package cobra

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorktreeCreate_HasRepoRef(t *testing.T) {
	t.Parallel()

	cmd := newWorktreeCreateCmd()
	require.NotNil(t, cmd.Flag("repo"), "worktree create must expose explicit repo selection")
}

func TestWorktreeCreate_HasBaseFlag(t *testing.T) {
	t.Parallel()

	cmd := newWorktreeCreateCmd()
	require.NotNil(t, cmd.Flag("base"), "worktree create must expose base branch selection")
}

func TestWorktreeCreate_DoesNotExposeParentFlag(t *testing.T) {
	t.Parallel()

	cmd := newWorktreeCreateCmd()
	require.Nil(t, cmd.Flag("parent"), "worktree create must not expose the legacy parent flag")
}
