package cobra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestAgentCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("agent")
	require.Error(t, err, "expected error when agent called without target")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestAgentLS_StartFlagsAreRejected(t *testing.T) {
	_, _, err := executeCmd("agent", "ls", "--mode", "headless")
	require.Error(t, err, "expected start-only flags to be rejected by agent ls")
	assert.Contains(t, err.Error(), "unknown flag: --mode")
}

func TestAgentTarget_StartFlagsAreRejected(t *testing.T) {
	_, _, err := executeCmd("agent", "inv-1", "--mode", "headless")
	require.Error(t, err, "expected start-only flags to be rejected by target-first agent commands")
	assert.Contains(t, err.Error(), "unknown flag: --mode")
}

func TestAgentTarget_ActionFlagsAreRejectedOutsideAction(t *testing.T) {
	_, _, err := executeCmd("agent", "inv-1", "history", "--apply")
	require.Error(t, err, "expected land-only flag to be rejected by history")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "--apply is not valid")
}

func TestAgentStart_InvalidModeReturnsInvalidArgument(t *testing.T) {
	_, _, err := executeCmd("agent", "start", "--mode", "bogus")
	require.Error(t, err, "expected invalid mode to be rejected")
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
}

func TestCompletionAgentCanonicalTargets(t *testing.T) {
	dataDir, configDir := setIsolatedCompletionEnv(t)

	stdout, _, err := executeCmd("__complete", "agent", "s")
	require.NoError(t, err, "expected agent completion to succeed")
	assert.Contains(t, stdout, "start")
	assert.NoDirExists(t, dataDir)
	assert.NoDirExists(t, configDir)
}

func TestCompletionAgentHistoryLogsAction(t *testing.T) {
	stdout, _, err := executeCmd("__complete", "agent", "inv-1", "history", "l")
	require.NoError(t, err, "expected nested target-first completion to succeed")
	assert.Contains(t, stdout, "logs")
}
