package cobra

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestWorktreeCreate_HelpExplainsRepoSelectionAndBaseDefault(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCmd("worktree", "create", "--help")
	require.NoError(t, err, "expected worktree create help to render")
	assert.Contains(t, stdout, "Use --repo to target a registered repo from any cwd")
	assert.Contains(t, stdout, "defaults to the current branch of the selected checkout")
	assert.NotContains(t, stdout, "defaults to current directory")
}
