package runnerstatus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Missing(t *testing.T) {
	t.Parallel()

	status, err := Load(t.TempDir())
	assert.NoError(t, err)
	assert.Nil(t, status)
}

func TestLoad_Valid(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o755))

	content := `{
		"schema_version": "2.0",
		"state": "waiting",
		"updated_at": "2026-01-19T12:00:00Z",
		"reason": "awaiting_user_input",
		"summary": "Need clarification",
		"questions": ["Which library should I use?"]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "runner_status.json"), []byte(content), 0o644))

	status, err := Load(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, StateWaiting, status.State)
	assert.Equal(t, ReasonAwaitingUserInput, status.Reason)
	assert.Equal(t, "Need clarification", status.Summary)
}

func TestLoad_Invalid(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "runner_status.json"), []byte("not valid json"), 0o644))

	status, err := Load(tmpDir)
	require.Error(t, err)
	assert.Nil(t, status)
}

func TestRunnerStatus_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  *RunnerStatus
		wantErr bool
	}{
		{
			name:    "nil status",
			status:  nil,
			wantErr: true,
		},
		{
			name: "missing state",
			status: &RunnerStatus{
				Summary: "test",
			},
			wantErr: true,
		},
		{
			name: "invalid state",
			status: &RunnerStatus{
				State:   "invalid",
				Summary: "test",
			},
			wantErr: true,
		},
		{
			name: "running requires summary only",
			status: &RunnerStatus{
				State:   StateRunning,
				Summary: "Working on feature",
			},
			wantErr: false,
		},
		{
			name: "waiting user input requires questions",
			status: &RunnerStatus{
				State:   StateWaiting,
				Reason:  ReasonAwaitingUserInput,
				Summary: "Need clarification",
			},
			wantErr: true,
		},
		{
			name: "waiting questions require awaiting_user_input",
			status: &RunnerStatus{
				State:     StateWaiting,
				Reason:    ReasonTurnComplete,
				Summary:   "Done for now",
				Questions: []string{"Continue?"},
			},
			wantErr: true,
		},
		{
			name: "waiting user input valid",
			status: &RunnerStatus{
				State:     StateWaiting,
				Reason:    ReasonAwaitingUserInput,
				Summary:   "Need clarification",
				Questions: []string{"Which library should I use?"},
			},
			wantErr: false,
		},
		{
			name: "succeeded requires how_to_test",
			status: &RunnerStatus{
				State:   StateSucceeded,
				Summary: "Work complete",
			},
			wantErr: true,
		},
		{
			name: "succeeded valid",
			status: &RunnerStatus{
				State:     StateSucceeded,
				Summary:   "Work complete",
				HowToTest: "go test ./...",
			},
			wantErr: false,
		},
		{
			name: "failed valid",
			status: &RunnerStatus{
				State:   StateFailed,
				Reason:  "dependency_missing",
				Summary: "Cannot finish successfully",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.status.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
