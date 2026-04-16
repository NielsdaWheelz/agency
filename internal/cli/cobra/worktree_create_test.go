package cobra

import (
	"bytes"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestWorktreeCreate_HasRepoRef(t *testing.T) {
	t.Parallel()

	cmd := newWorktreeCreateCmd()
	require.NotNil(t, cmd.Flag("repo"), "worktree create must expose explicit repo selection")
}

func TestWorktreeCreate_HasBaseAlias(t *testing.T) {
	t.Parallel()

	cmd := newWorktreeCreateCmd()
	require.NotNil(t, cmd.Flag("base"), "worktree create must expose base branch selection")
}

func TestWorktreeCreate_ParentBaseConflict(t *testing.T) {
	t.Parallel()

	cmd := newWorktreeCreateCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{
		"--name", "my-feature",
		"--parent", "main",
		"--base", "develop",
	})

	err := cmd.Execute()
	require.Error(t, err)
	require.Equal(t, errors.EUsage, errors.GetCode(err))
}
