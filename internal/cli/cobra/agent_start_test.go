package cobra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentStart_HasRepoRefAndPositionalWorktree(t *testing.T) {
	t.Parallel()

	cmd := newAgentStartCmd()
	require.NotNil(t, cmd.Flag("repo"), "agent start must expose explicit repo selection")
	require.NotNil(t, cmd.Flag("agency-config"), "agent start must expose explicit agency config selection")
	assert.Contains(t, cmd.Use, "[<worktree-ref>]", "agent start must declare the worktree ref as an optional positional argument")
	require.Nil(t, cmd.Flag("worktree"), "agent start must not expose the legacy --worktree flag")
}

func TestAgentStart_HelpExplainsRepoAndWorktreeInference(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCmd("agent", "start", "--help")
	require.NoError(t, err, "expected agent start help to render")
	assert.Contains(t, stdout, "Use --repo to target a registered repo from any cwd")
	assert.Contains(t, stdout, "agency agent start my-feature --repo agency")
	assert.Contains(t, stdout, "Omitting the worktree ref only works when cwd is already inside")
	assert.Contains(t, stdout, "worktree you want to use")
	assert.NotContains(t, stdout, "--worktree")
	assert.NotContains(t, stdout, "defaults to current integration worktree")
}
