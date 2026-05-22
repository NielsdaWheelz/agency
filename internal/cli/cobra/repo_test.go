package cobra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestRepoCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("repo")
	require.Error(t, err, "expected error when repo called without target")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestRepoTarget_ActionFlagsAreRejectedOutsideAction(t *testing.T) {
	_, _, err := executeCmd("repo", "add", "--yes")
	require.Error(t, err, "expected rm-only flag to be rejected by add")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "--yes is not valid")
}

func TestCompletionRepoCanonicalTargets(t *testing.T) {
	dataDir, configDir := setIsolatedCompletionEnv(t)

	stdout, _, err := executeCmd("__complete", "repo", "a")
	require.NoError(t, err, "expected repo completion to succeed")
	assert.Contains(t, stdout, "add")
	assert.NoDirExists(t, dataDir)
	assert.NoDirExists(t, configDir)
}
