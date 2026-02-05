package runnerstatus

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusPath(t *testing.T) {
	t.Parallel()

	worktreePath := "/path/to/worktree"
	got := StatusPath(worktreePath)
	want := "/path/to/worktree/.agency/state/runner_status.json"
	assert.Equal(t, want, got)
}

func TestLoad_Missing(t *testing.T) {
	t.Parallel()

	// Create a temp dir without the status file
	tmpDir := t.TempDir()

	status, err := Load(tmpDir)
	assert.NoError(t, err, "Load() should not error for missing file")
	assert.Nil(t, status, "Load() should return nil for missing file")
}

func TestLoad_Valid(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0755))

	content := `{
		"schema_version": "1.0",
		"status": "working",
		"updated_at": "2026-01-19T12:00:00Z",
		"summary": "Test summary",
		"questions": [],
		"blockers": [],
		"how_to_test": "",
		"risks": []
	}`
	statusPath := filepath.Join(stateDir, "runner_status.json")
	require.NoError(t, os.WriteFile(statusPath, []byte(content), 0644))

	status, err := Load(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, StatusWorking, status.Status)
	assert.Equal(t, "Test summary", status.Summary)
}

func TestLoad_Invalid(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0755))

	content := `not valid json`
	statusPath := filepath.Join(stateDir, "runner_status.json")
	require.NoError(t, os.WriteFile(statusPath, []byte(content), 0644))

	status, err := Load(tmpDir)
	require.Error(t, err, "Load() should error for invalid JSON")
	assert.Nil(t, status, "Load() should return nil for invalid JSON")
}

func TestLoadWithModTime(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0755))

	content := `{
		"schema_version": "1.0",
		"status": "working",
		"updated_at": "2026-01-19T12:00:00Z",
		"summary": "Test summary"
	}`
	statusPath := filepath.Join(stateDir, "runner_status.json")
	require.NoError(t, os.WriteFile(statusPath, []byte(content), 0644))

	status, modTime, err := LoadWithModTime(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.False(t, modTime.IsZero(), "LoadWithModTime() modTime should be non-zero")
}

func TestLoadWithModTime_Missing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	status, modTime, err := LoadWithModTime(tmpDir)
	assert.NoError(t, err, "LoadWithModTime() should not error for missing file")
	assert.Nil(t, status, "LoadWithModTime() status should be nil for missing file")
	assert.True(t, modTime.IsZero(), "LoadWithModTime() modTime should be zero for missing file")
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
			name: "empty status value",
			status: &RunnerStatus{
				Status:  "",
				Summary: "test",
			},
			wantErr: true,
		},
		{
			name: "invalid status value",
			status: &RunnerStatus{
				Status:  "invalid",
				Summary: "test",
			},
			wantErr: true,
		},
		{
			name: "missing summary",
			status: &RunnerStatus{
				Status:  StatusWorking,
				Summary: "",
			},
			wantErr: true,
		},
		{
			name: "valid working status",
			status: &RunnerStatus{
				Status:  StatusWorking,
				Summary: "Working on feature",
			},
			wantErr: false,
		},
		{
			name: "needs_input without questions",
			status: &RunnerStatus{
				Status:    StatusNeedsInput,
				Summary:   "Need clarification",
				Questions: []string{},
			},
			wantErr: true,
		},
		{
			name: "valid needs_input",
			status: &RunnerStatus{
				Status:    StatusNeedsInput,
				Summary:   "Need clarification",
				Questions: []string{"What library?"},
			},
			wantErr: false,
		},
		{
			name: "blocked without blockers",
			status: &RunnerStatus{
				Status:   StatusBlocked,
				Summary:  "Cannot proceed",
				Blockers: []string{},
			},
			wantErr: true,
		},
		{
			name: "valid blocked",
			status: &RunnerStatus{
				Status:   StatusBlocked,
				Summary:  "Cannot proceed",
				Blockers: []string{"Dependency unavailable"},
			},
			wantErr: false,
		},
		{
			name: "ready_for_review without how_to_test",
			status: &RunnerStatus{
				Status:    StatusReadyForReview,
				Summary:   "Work complete",
				HowToTest: "",
			},
			wantErr: true,
		},
		{
			name: "valid ready_for_review",
			status: &RunnerStatus{
				Status:    StatusReadyForReview,
				Summary:   "Work complete",
				HowToTest: "Run npm test",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.status.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRunnerStatus_Age(t *testing.T) {
	t.Parallel()

	t.Run("nil status", func(t *testing.T) {
		t.Parallel()

		var s *RunnerStatus
		assert.Equal(t, time.Duration(0), s.Age())
	})

	t.Run("empty updated_at", func(t *testing.T) {
		t.Parallel()

		s := &RunnerStatus{UpdatedAt: ""}
		assert.Equal(t, time.Duration(0), s.Age())
	})

	t.Run("invalid updated_at", func(t *testing.T) {
		t.Parallel()

		s := &RunnerStatus{UpdatedAt: "not-a-timestamp"}
		assert.Equal(t, time.Duration(0), s.Age())
	})

	t.Run("valid updated_at", func(t *testing.T) {
		t.Parallel()

		// Set updated_at to 5 minutes ago
		fiveMinutesAgo := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
		s := &RunnerStatus{UpdatedAt: fiveMinutesAgo}
		age := s.Age()
		// Allow some tolerance
		assert.True(t, age >= 4*time.Minute && age <= 6*time.Minute, "Age() = %v, want ~5m", age)
	})
}

func TestNewInitial(t *testing.T) {
	t.Parallel()

	s := NewInitial()
	assert.Equal(t, SchemaVersion, s.SchemaVersion)
	assert.Equal(t, StatusWorking, s.Status)
	assert.Equal(t, "Starting work", s.Summary)
	assert.NotEmpty(t, s.UpdatedAt, "UpdatedAt should not be empty")
	// Verify it parses as RFC3339
	_, err := time.Parse(time.RFC3339, s.UpdatedAt)
	assert.NoError(t, err, "UpdatedAt is not valid RFC3339")
}

func TestStatus_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status Status
		want   bool
	}{
		{StatusWorking, true},
		{StatusNeedsInput, true},
		{StatusBlocked, true},
		{StatusReadyForReview, true},
		{"", false},
		{"invalid", false},
		{"Working", false}, // case sensitive
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.status.IsValid())
		})
	}
}
