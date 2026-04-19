package cobra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentStart_HasRepoRef(t *testing.T) {
	t.Parallel()

	cmd := newAgentStartCmd()
	require.NotNil(t, cmd.Flag("repo"), "agent start must expose explicit repo selection")
	require.NotNil(t, cmd.Flag("agency-config"), "agent start must expose explicit agency config selection")
}

func TestAgentStart_HelpExplainsRepoAndWorktreeInference(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCmd("agent", "start", "--help")
	require.NoError(t, err, "expected agent start help to render")
	assert.Contains(t, stdout, "Use --repo to target a registered repo from any cwd")
	assert.Contains(t, stdout, "Omitting --worktree only works when cwd is already inside")
	assert.Contains(t, stdout, "worktree you want to use")
	assert.NotContains(t, stdout, "defaults to current integration worktree")
}
