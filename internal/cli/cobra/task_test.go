package cobra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestTaskCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("task")
	require.Error(t, err, "expected error when task called without target")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestTaskStart_RequiresName(t *testing.T) {
	_, _, err := executeCmd("task", "start")
	require.Error(t, err, "expected error when task start is missing name")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestTaskTarget_ActionFlagsAreRejectedOutsideAction(t *testing.T) {
	_, _, err := executeCmd("task", "task-1", "watch", "--prompt", "retry")
	require.Error(t, err, "expected retry-only flag to be rejected by watch")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "--prompt is not valid")
}

func TestCompletionTaskCanonicalTargets(t *testing.T) {
	dataDir, configDir := setIsolatedCompletionEnv(t)

	stdout, _, err := executeCmd("__complete", "task", "s")
	require.NoError(t, err, "expected task completion to succeed")
	assert.Contains(t, stdout, "start")
	assert.NoDirExists(t, dataDir)
	assert.NoDirExists(t, configDir)
}
