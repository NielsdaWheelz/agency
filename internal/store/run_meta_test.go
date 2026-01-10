package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// TestRunDirPath verifies run directory path construction.
func TestRunDirPath(t *testing.T) {
	s := NewStore(nil, "/data/agency", nil)
	got := s.RunDir("repo123", "run456")
	want := "/data/agency/repos/repo123/runs/run456"
	if got != want {
		t.Errorf("RunDir() = %q, want %q", got, want)
	}
}

// TestRunMetaPath verifies run meta.json path construction.
func TestRunMetaPath(t *testing.T) {
	s := NewStore(nil, "/data/agency", nil)
	got := s.RunMetaPath("repo123", "run456")
	want := "/data/agency/repos/repo123/runs/run456/meta.json"
	if got != want {
		t.Errorf("RunMetaPath() = %q, want %q", got, want)
	}
}

// TestRunLogsDir verifies run logs directory path construction.
func TestRunLogsDir(t *testing.T) {
	s := NewStore(nil, "/data/agency", nil)
	got := s.RunLogsDir("repo123", "run456")
	want := "/data/agency/repos/repo123/runs/run456/logs"
	if got != want {
		t.Errorf("RunLogsDir() = %q, want %q", got, want)
	}
}

// TestRunsDir verifies runs directory path construction.
func TestRunsDir(t *testing.T) {
	s := NewStore(nil, "/data/agency", nil)
	got := s.RunsDir("repo123")
	want := "/data/agency/repos/repo123/runs"
	if got != want {
		t.Errorf("RunsDir() = %q, want %q", got, want)
	}
}

// TestEnsureRunDir_Success verifies run directory creation.
func TestEnsureRunDir_Success(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	runDir, err := s.EnsureRunDir("repo123", "run456")
	if err != nil {
		t.Fatalf("EnsureRunDir() error = %v, want nil", err)
	}

	wantDir := filepath.Join(dataDir, "repos", "repo123", "runs", "run456")
	if runDir != wantDir {
		t.Errorf("EnsureRunDir() = %q, want %q", runDir, wantDir)
	}

	// Verify directory exists
	if _, err := os.Stat(runDir); os.IsNotExist(err) {
		t.Error("run directory should exist")
	}

	// Verify logs directory exists
	logsDir := filepath.Join(runDir, "logs")
	if _, err := os.Stat(logsDir); os.IsNotExist(err) {
		t.Error("logs directory should exist")
	}
}

// TestEnsureRunDir_Collision verifies E_RUN_DIR_EXISTS on collision.
func TestEnsureRunDir_Collision(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// First call should succeed
	_, err := s.EnsureRunDir("repo123", "run456")
	if err != nil {
		t.Fatalf("first EnsureRunDir() error = %v, want nil", err)
	}

	// Second call should fail with E_RUN_DIR_EXISTS
	_, err = s.EnsureRunDir("repo123", "run456")
	if err == nil {
		t.Fatal("second EnsureRunDir() error = nil, want E_RUN_DIR_EXISTS")
	}
	if errors.GetCode(err) != errors.ERunDirExists {
		t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.ERunDirExists)
	}
}

// TestWriteInitialMeta_RequiredFields verifies meta.json has all required fields.
func TestWriteInitialMeta_RequiredFields(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create run directory first
	_, err := s.EnsureRunDir("repo123", "run456")
	if err != nil {
		t.Fatalf("EnsureRunDir() error = %v", err)
	}

	// Write meta
	meta := NewRunMeta(
		"run456",
		"repo123",
		"Test Title",
		"claude",
		"claude --model opus",
		"main",
		"agency/test-title-a3f2",
		"/path/to/worktree",
		now,
	)

	err = s.WriteInitialMeta("repo123", "run456", meta)
	if err != nil {
		t.Fatalf("WriteInitialMeta() error = %v", err)
	}

	// Read and verify
	metaPath := s.RunMetaPath("repo123", "run456")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read meta.json: %v", err)
	}

	// Verify JSON is valid
	if !json.Valid(data) {
		t.Error("meta.json is not valid JSON")
	}

	// Parse and verify fields
	var parsed RunMeta
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse meta.json: %v", err)
	}

	// Required fields
	if parsed.SchemaVersion != "1.0" {
		t.Errorf("schema_version = %q, want %q", parsed.SchemaVersion, "1.0")
	}
	if parsed.RunID != "run456" {
		t.Errorf("run_id = %q, want %q", parsed.RunID, "run456")
	}
	if parsed.RepoID != "repo123" {
		t.Errorf("repo_id = %q, want %q", parsed.RepoID, "repo123")
	}
	if parsed.Title != "Test Title" {
		t.Errorf("title = %q, want %q", parsed.Title, "Test Title")
	}
	if parsed.Runner != "claude" {
		t.Errorf("runner = %q, want %q", parsed.Runner, "claude")
	}
	if parsed.RunnerCmd != "claude --model opus" {
		t.Errorf("runner_cmd = %q, want %q", parsed.RunnerCmd, "claude --model opus")
	}
	if parsed.ParentBranch != "main" {
		t.Errorf("parent_branch = %q, want %q", parsed.ParentBranch, "main")
	}
	if parsed.Branch != "agency/test-title-a3f2" {
		t.Errorf("branch = %q, want %q", parsed.Branch, "agency/test-title-a3f2")
	}
	if parsed.WorktreePath != "/path/to/worktree" {
		t.Errorf("worktree_path = %q, want %q", parsed.WorktreePath, "/path/to/worktree")
	}

	// Timestamp should be RFC3339 UTC (ends with Z)
	if parsed.CreatedAt != "2026-01-10T12:00:00Z" {
		t.Errorf("created_at = %q, want %q", parsed.CreatedAt, "2026-01-10T12:00:00Z")
	}
	if !strings.HasSuffix(parsed.CreatedAt, "Z") {
		t.Errorf("created_at should end with Z (UTC), got %q", parsed.CreatedAt)
	}

	// tmux_session_name should be absent (empty string in Go)
	if parsed.TmuxSessionName != "" {
		t.Errorf("tmux_session_name = %q, should be absent (empty)", parsed.TmuxSessionName)
	}
}

// TestWriteInitialMeta_IsAtomic verifies atomic write behavior.
func TestWriteInitialMeta_IsAtomic(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create run directory
	_, err := s.EnsureRunDir("repo123", "run456")
	if err != nil {
		t.Fatalf("EnsureRunDir() error = %v", err)
	}

	// Write initial meta
	meta1 := NewRunMeta("run456", "repo123", "First Title", "claude", "claude", "main", "agency/first-a3f2", "/path/first", now)
	err = s.WriteInitialMeta("repo123", "run456", meta1)
	if err != nil {
		t.Fatalf("first WriteInitialMeta() error = %v", err)
	}

	// Write again with different title
	meta2 := NewRunMeta("run456", "repo123", "Second Title", "claude", "claude", "main", "agency/second-a3f2", "/path/second", now)
	err = s.WriteInitialMeta("repo123", "run456", meta2)
	if err != nil {
		t.Fatalf("second WriteInitialMeta() error = %v", err)
	}

	// Read and verify it's the second version
	loaded, err := s.ReadMeta("repo123", "run456")
	if err != nil {
		t.Fatalf("ReadMeta() error = %v", err)
	}

	if loaded.Title != "Second Title" {
		t.Errorf("title = %q, want %q (second write)", loaded.Title, "Second Title")
	}
}

// TestReadMeta_NotFound verifies E_RUN_NOT_FOUND for missing meta.
func TestReadMeta_NotFound(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	_, err := s.ReadMeta("nonexistent", "run")
	if err == nil {
		t.Fatal("ReadMeta() error = nil, want E_RUN_NOT_FOUND")
	}
	if errors.GetCode(err) != errors.ERunNotFound {
		t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.ERunNotFound)
	}
}

// TestReadMeta_CorruptJSON verifies E_STORE_CORRUPT for invalid JSON.
func TestReadMeta_CorruptJSON(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Create run directory and corrupt meta.json
	runDir := s.RunDir("repo123", "run456")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}
	metaPath := s.RunMetaPath("repo123", "run456")
	if err := os.WriteFile(metaPath, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := s.ReadMeta("repo123", "run456")
	if err == nil {
		t.Fatal("ReadMeta() error = nil, want E_STORE_CORRUPT")
	}
	if errors.GetCode(err) != errors.EStoreCorrupt {
		t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.EStoreCorrupt)
	}
}

// TestUpdateMeta verifies the update function.
func TestUpdateMeta(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create run directory and initial meta
	_, err := s.EnsureRunDir("repo123", "run456")
	if err != nil {
		t.Fatalf("EnsureRunDir() error = %v", err)
	}

	meta := NewRunMeta("run456", "repo123", "Test Title", "claude", "claude", "main", "agency/test-a3f2", "/path/to/worktree", now)
	err = s.WriteInitialMeta("repo123", "run456", meta)
	if err != nil {
		t.Fatalf("WriteInitialMeta() error = %v", err)
	}

	// Update to add tmux session name
	err = s.UpdateMeta("repo123", "run456", func(m *RunMeta) {
		m.TmuxSessionName = "agency:run456"
	})
	if err != nil {
		t.Fatalf("UpdateMeta() error = %v", err)
	}

	// Read and verify
	loaded, err := s.ReadMeta("repo123", "run456")
	if err != nil {
		t.Fatalf("ReadMeta() error = %v", err)
	}

	if loaded.TmuxSessionName != "agency:run456" {
		t.Errorf("tmux_session_name = %q, want %q", loaded.TmuxSessionName, "agency:run456")
	}
	// Original fields should be preserved
	if loaded.Title != "Test Title" {
		t.Errorf("title = %q, want %q", loaded.Title, "Test Title")
	}
}

// TestNewRunMeta verifies the constructor sets all fields correctly.
func TestNewRunMeta(t *testing.T) {
	now := time.Date(2026, 1, 10, 15, 30, 45, 0, time.FixedZone("EST", -5*3600))

	meta := NewRunMeta(
		"20260110153045-a3f2",
		"abc123def456",
		"My Test Run",
		"codex",
		"codex --full-auto",
		"develop",
		"agency/my-test-run-a3f2",
		"/path/to/worktree/directory",
		now,
	)

	if meta.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q", meta.SchemaVersion, "1.0")
	}
	if meta.RunID != "20260110153045-a3f2" {
		t.Errorf("RunID = %q, want %q", meta.RunID, "20260110153045-a3f2")
	}
	if meta.RepoID != "abc123def456" {
		t.Errorf("RepoID = %q, want %q", meta.RepoID, "abc123def456")
	}
	if meta.Title != "My Test Run" {
		t.Errorf("Title = %q, want %q", meta.Title, "My Test Run")
	}
	if meta.Runner != "codex" {
		t.Errorf("Runner = %q, want %q", meta.Runner, "codex")
	}
	if meta.RunnerCmd != "codex --full-auto" {
		t.Errorf("RunnerCmd = %q, want %q", meta.RunnerCmd, "codex --full-auto")
	}
	if meta.ParentBranch != "develop" {
		t.Errorf("ParentBranch = %q, want %q", meta.ParentBranch, "develop")
	}
	if meta.Branch != "agency/my-test-run-a3f2" {
		t.Errorf("Branch = %q, want %q", meta.Branch, "agency/my-test-run-a3f2")
	}
	if meta.WorktreePath != "/path/to/worktree/directory" {
		t.Errorf("WorktreePath = %q, want %q", meta.WorktreePath, "/path/to/worktree/directory")
	}
	// Should convert to UTC
	if meta.CreatedAt != "2026-01-10T20:30:45Z" {
		t.Errorf("CreatedAt = %q, want %q (converted to UTC)", meta.CreatedAt, "2026-01-10T20:30:45Z")
	}
}

// TestJSONOmitEmptyFields verifies optional fields are omitted when empty.
func TestJSONOmitEmptyFields(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create run directory
	_, err := s.EnsureRunDir("repo123", "run456")
	if err != nil {
		t.Fatalf("EnsureRunDir() error = %v", err)
	}

	// Write meta without optional fields
	meta := NewRunMeta("run456", "repo123", "Test", "claude", "claude", "main", "agency/test-a3f2", "/path", now)
	err = s.WriteInitialMeta("repo123", "run456", meta)
	if err != nil {
		t.Fatalf("WriteInitialMeta() error = %v", err)
	}

	// Read raw JSON
	metaPath := s.RunMetaPath("repo123", "run456")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)

	// These fields should NOT be present when empty/zero
	shouldBeAbsent := []string{
		`"tmux_session_name"`,
		`"flags"`,
		`"setup"`,
		`"pr_number"`,
		`"pr_url"`,
		`"last_push_at"`,
		`"last_verify_at"`,
		`"archive"`,
	}

	for _, field := range shouldBeAbsent {
		if strings.Contains(content, field) {
			t.Errorf("field %s should be omitted when empty, but found in JSON", field)
		}
	}
}
