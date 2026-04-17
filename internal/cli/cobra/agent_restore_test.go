package cobra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentRestoreCmd_HelpMentionsCheckpointAndTurnSelectors(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCmd("agent", "restore", "--help")
	require.NoError(t, err, "expected agent restore help to render")
	assert.Contains(t, stdout, "--checkpoint")
	assert.Contains(t, stdout, "--turn")
	assert.NotContains(t, stdout, "--history")
}

func TestAgentHistoryLogsCmd_HelpMentionsFollowMode(t *testing.T) {
	t.Parallel()

	stdout, _, err := executeCmd("agent", "history", "logs", "--help")
	require.NoError(t, err, "expected agent history logs help to render")
	assert.Contains(t, stdout, "--follow")
	assert.Contains(t, stdout, "--offset")
	assert.Contains(t, stdout, "--kind")
}
