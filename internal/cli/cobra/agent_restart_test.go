package cobra

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeJSONMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(data), &payload))
	return payload
}

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

func TestAgentRestartCmd_JSONRequiresCheckpointOrHistory_EmitsEnvelope(t *testing.T) {
	t.Parallel()

	cmd := newAgentRestartCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"inv-123", "--json"})

	err := cmd.Execute()
	require.NoError(t, err, "json mode should emit envelope instead of returning plain error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EUsage), payload["error_code"])
	_, hasMessage := payload["message"]
	assert.True(t, hasMessage)
}

func TestAgentRestartCmd_JSONInvalidEnvAssignment_EmitsEnvelope(t *testing.T) {
	t.Parallel()

	cmd := newAgentRestartCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"inv-123", "--checkpoint", "1", "--env", "INVALID", "--json"})

	err := cmd.Execute()
	require.NoError(t, err, "json mode should emit envelope instead of returning plain error")

	payload := decodeJSONMap(t, stdout.Bytes())
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EUsage), payload["error_code"])
	assert.Contains(t, payload["message"], "invalid --env value")
}
