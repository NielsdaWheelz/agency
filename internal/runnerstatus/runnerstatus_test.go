package runnerstatus

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatusPath(t *testing.T) {
	worktreePath := "/path/to/worktree"
	got := StatusPath(worktreePath)
	want := "/path/to/worktree/.agency/state/runner_status.json"
	if got != want {
		t.Errorf("StatusPath() = %q, want %q", got, want)
	}
}

func TestLoad_Missing(t *testing.T) {
	// Create a temp dir without the status file
	tmpDir := t.TempDir()

	status, err := Load(tmpDir)
	if err != nil {
		t.Errorf("Load() error = %v, want nil for missing file", err)
	}
	if status != nil {
		t.Errorf("Load() = %v, want nil for missing file", status)
	}
}

func TestLoad_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".agency", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}

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
	if err := os.WriteFile(statusPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	status, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if status == nil {
		t.Fatal("Load() = nil, want non-nil")
	}
	if status.Status != StatusWorking {
		t.Errorf("status.Status = %q, want %q", status.Status, StatusWorking)
	}
	if status.Summary != "Test summary" {
		t.Errorf("status.Summary = %q, want %q", status.Summary, "Test summary")
	}
}

func TestLoad_Invalid(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".agency", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `not valid json`
	statusPath := filepath.Join(stateDir, "runner_status.json")
	if err := os.WriteFile(statusPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	status, err := Load(tmpDir)
	if err == nil {
		t.Error("Load() error = nil, want error for invalid JSON")
	}
	if status != nil {
		t.Errorf("Load() = %v, want nil for invalid JSON", status)
	}
}

func TestLoadWithModTime(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".agency", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `{
		"schema_version": "1.0",
		"status": "working",
		"updated_at": "2026-01-19T12:00:00Z",
		"summary": "Test summary"
	}`
	statusPath := filepath.Join(stateDir, "runner_status.json")
	if err := os.WriteFile(statusPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	status, modTime, err := LoadWithModTime(tmpDir)
	if err != nil {
		t.Fatalf("LoadWithModTime() error = %v", err)
	}
	if status == nil {
		t.Fatal("LoadWithModTime() status = nil, want non-nil")
	}
	if modTime.IsZero() {
		t.Error("LoadWithModTime() modTime is zero, want non-zero")
	}
}

func TestLoadWithModTime_Missing(t *testing.T) {
	tmpDir := t.TempDir()

	status, modTime, err := LoadWithModTime(tmpDir)
	if err != nil {
		t.Errorf("LoadWithModTime() error = %v, want nil for missing file", err)
	}
	if status != nil {
		t.Errorf("LoadWithModTime() status = %v, want nil for missing file", status)
	}
	if !modTime.IsZero() {
		t.Errorf("LoadWithModTime() modTime = %v, want zero for missing file", modTime)
	}
}

func TestRunnerStatus_Validate(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			err := tt.status.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunnerStatus_Age(t *testing.T) {
	t.Run("nil status", func(t *testing.T) {
		var s *RunnerStatus
		if got := s.Age(); got != 0 {
			t.Errorf("Age() = %v, want 0", got)
		}
	})

	t.Run("empty updated_at", func(t *testing.T) {
		s := &RunnerStatus{UpdatedAt: ""}
		if got := s.Age(); got != 0 {
			t.Errorf("Age() = %v, want 0", got)
		}
	})

	t.Run("invalid updated_at", func(t *testing.T) {
		s := &RunnerStatus{UpdatedAt: "not-a-timestamp"}
		if got := s.Age(); got != 0 {
			t.Errorf("Age() = %v, want 0", got)
		}
	})

	t.Run("valid updated_at", func(t *testing.T) {
		// Set updated_at to 5 minutes ago
		fiveMinutesAgo := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
		s := &RunnerStatus{UpdatedAt: fiveMinutesAgo}
		age := s.Age()
		// Allow some tolerance
		if age < 4*time.Minute || age > 6*time.Minute {
			t.Errorf("Age() = %v, want ~5m", age)
		}
	})
}

func TestNewInitial(t *testing.T) {
	s := NewInitial()
	if s.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", s.SchemaVersion, SchemaVersion)
	}
	if s.Status != StatusWorking {
		t.Errorf("Status = %q, want %q", s.Status, StatusWorking)
	}
	if s.Summary != "Starting work" {
		t.Errorf("Summary = %q, want %q", s.Summary, "Starting work")
	}
	if s.UpdatedAt == "" {
		t.Error("UpdatedAt should not be empty")
	}
	// Verify it parses as RFC3339
	_, err := time.Parse(time.RFC3339, s.UpdatedAt)
	if err != nil {
		t.Errorf("UpdatedAt is not valid RFC3339: %v", err)
	}
}

func TestStatus_IsValid(t *testing.T) {
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
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}
