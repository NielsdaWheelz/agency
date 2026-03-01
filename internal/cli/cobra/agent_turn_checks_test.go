package cobra

import (
	"testing"

	spcobra "github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentCmd_ExposesChecksSubcommand(t *testing.T) {
	t.Parallel()

	cmd := newAgentCmd()
	checks := cmd.Commands()

	foundChecks := false
	for _, sub := range checks {
		if sub.Name() == "checks" {
			foundChecks = true
			break
		}
	}

	require.True(t, foundChecks, "agent command must expose checks-first readiness surface")
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

func TestAgentChecksCmd_AcceptsJSON(t *testing.T) {
	t.Parallel()

	cmd := newAgentCmd()
	var checksCmd *spcobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "checks" {
			checksCmd = sub
			break
		}
	}

	require.NotNil(t, checksCmd, "checks subcommand should be registered")
	err := checksCmd.ParseFlags([]string{"--repo", "repo-1", "--json"})
	assert.NoError(t, err)
}
