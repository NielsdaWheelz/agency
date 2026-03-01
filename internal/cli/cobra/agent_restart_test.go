package cobra

import (
	"io"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentRestartCmd_RequiresCheckpointOrHistory(t *testing.T) {
	t.Parallel()

	cmd := newAgentRestartCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"inv-123"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "either --checkpoint <id> or --history is required")
}

func TestAgentRestartCmd_RejectsCheckpointAndHistoryTogether(t *testing.T) {
	t.Parallel()

	cmd := newAgentRestartCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"inv-123", "--checkpoint", "3", "--history"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "use either --checkpoint or --history")
}

func TestAgentRestartCmd_RejectsCheckpointAndHistoryTogether_WhenCheckpointIsZero(t *testing.T) {
	t.Parallel()

	cmd := newAgentRestartCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"inv-123", "--checkpoint", "0", "--history"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "use either --checkpoint or --history")
}

func TestAgentRestartCmd_RejectsNonPositiveCheckpoint(t *testing.T) {
	t.Parallel()

	cmd := newAgentRestartCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"inv-123", "--checkpoint", "-1"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "--checkpoint must be a positive integer")
}
