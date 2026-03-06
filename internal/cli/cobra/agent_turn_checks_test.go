package cobra

import (
	"testing"

	spcobra "github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentCmd_ExposesReviewAndHidesChecks(t *testing.T) {
	t.Parallel()

	cmd := newAgentCmd()
	checks := cmd.Commands()

	foundReview := false
	foundChecks := false
	for _, sub := range checks {
		if sub.Name() == "review" {
			foundReview = true
		}
		if sub.Name() == "checks" {
			foundChecks = true
		}
	}

	require.True(t, foundReview, "agent command must expose canonical review surface")
	require.False(t, foundChecks, "agent command must not expose deprecated checks surface")
}

func TestAgentDiffCmd_AcceptsTurnFlagsAndJSON(t *testing.T) {
	t.Parallel()

	t.Run("single_turn_selector", func(t *testing.T) {
		t.Parallel()
		cmd := newAgentDiffCmd()
		err := cmd.ParseFlags([]string{"--repo", "repo-1", "--json", "--turn", "inv_event:2:agency.followup_prompt"})
		require.NoError(t, err)
	})

	t.Run("turn_range_selector", func(t *testing.T) {
		t.Parallel()
		cmd := newAgentDiffCmd()
		err := cmd.ParseFlags([]string{"--turn-range", "stream:1..stream:4"})
		require.NoError(t, err)
	})
}

func TestAgentReviewCmd_AcceptsJSON(t *testing.T) {
	t.Parallel()

	cmd := newAgentCmd()
	var reviewCmd *spcobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "review" {
			reviewCmd = sub
			break
		}
	}

	require.NotNil(t, reviewCmd, "review subcommand should be registered")
	err := reviewCmd.ParseFlags([]string{"--repo", "repo-1", "--json"})
	assert.NoError(t, err)
}

func TestWorktreePRSyncCmd_AcceptsPolicyFlags(t *testing.T) {
	t.Parallel()

	cmd := newWorktreeCmd()
	var prCmd *spcobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "pr" {
			prCmd = sub
			break
		}
	}
	require.NotNil(t, prCmd, "pr command should be registered")

	var syncCmd *spcobra.Command
	for _, sub := range prCmd.Commands() {
		if sub.Name() == "sync" {
			syncCmd = sub
			break
		}
	}
	require.NotNil(t, syncCmd, "pr sync subcommand should be registered")

	err := syncCmd.ParseFlags([]string{
		"--repo", "repo-1",
		"--json",
		"--allow-dirty",
		"--force-with-lease",
	})
	assert.NoError(t, err)
}
