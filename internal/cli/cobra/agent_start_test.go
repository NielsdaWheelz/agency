package cobra

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentStart_HasExplicitRepoAndWorktreeFlags(t *testing.T) {
	t.Parallel()

	cmd := newAgentStartCmd()
	require.NotNil(t, cmd.Flag("repo"), "agent start must expose explicit repo selection")
	require.NotNil(t, cmd.Flag("worktree"), "agent start must expose explicit worktree selection")
	require.NotNil(t, cmd.Flag("agency-config"), "agent start must expose explicit agency config selection")
	assert.Equal(t, "start", cmd.Use, "agent start must not accept a positional worktree argument")
}

func TestAgentStart_HelpExplainsContextAndExplicitWorktreeOverride(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCmd("agent", "start", "--help")
	require.NoError(t, err, "expected agent start help to render")
	assert.Contains(t, stdout, "Use --worktree to target a specific worktree from any cwd.")
	assert.Contains(t, stdout, "active context first")
	assert.Contains(t, stdout, "agency agent start --worktree my-feature")
	assert.Contains(t, stdout, "agency agent start --worktree my-feature --repo agency")
	assert.Contains(t, stdout, "agency context use my-feature --repo agency")
	assert.Contains(t, stdout, "If --agency-config is relative, it is resolved from the current directory")
	assert.Contains(t, stdout, "--worktree string")
	assert.NotContains(t, stdout, "agency agent start my-feature")
	assert.NotContains(t, stdout, "[<worktree-ref>]")
}

func TestAgentStart_RejectsPositionalWorktree(t *testing.T) {
	t.Parallel()

	cmd := newAgentStartCmd()
	err := cmd.Args(cmd, []string{"my-feature"})
	require.Error(t, err, "expected positional worktree refs to be rejected after the cutover")
	assert.Contains(t, err.Error(), "my-feature")
}

func TestAgentCmd_HelpShowsContextAwareStartSurface(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCmd("agent", "--help")
	require.NoError(t, err, "expected agent help to render")
	assert.Contains(t, stdout, "agency agent start       to create a new sandbox from the active context")
	assert.Contains(t, stdout, "agency agent start --worktree <worktree-ref>")
	assert.NotContains(t, stdout, "agency agent start [<worktree-ref>]")
}

func TestContextCmd_HelpShowsShowUseAndUnsetSurface(t *testing.T) {
	t.Parallel()

	cmd := newContextCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	require.NoError(t, cmd.Help(), "expected context help to render")
	assert.Contains(t, stdout.String(), "agency context           to show the active context")
	assert.Contains(t, stdout.String(), "agency context use <worktree-ref>")
	assert.Contains(t, stdout.String(), "agency context unset")
}
