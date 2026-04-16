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
