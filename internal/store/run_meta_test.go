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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunDirPath verifies run directory path construction.
func TestRunDirPath(t *testing.T) {
	t.Parallel()
	s := NewStore(nil, "/data/agency", nil)
	got := s.RunDir("repo123", "run456")
	want := "/data/agency/repos/repo123/runs/run456"
	assert.Equal(t, want, got, "RunDir()")
}

// TestRunMetaPath verifies run meta.json path construction.
func TestRunMetaPath(t *testing.T) {
	t.Parallel()
	s := NewStore(nil, "/data/agency", nil)
	got := s.RunMetaPath("repo123", "run456")
	want := "/data/agency/repos/repo123/runs/run456/meta.json"
	assert.Equal(t, want, got, "RunMetaPath()")
}

// TestRunLogsDir verifies run logs directory path construction.
func TestRunLogsDir(t *testing.T) {
	t.Parallel()
	s := NewStore(nil, "/data/agency", nil)
	got := s.RunLogsDir("repo123", "run456")
	want := "/data/agency/repos/repo123/runs/run456/logs"
	assert.Equal(t, want, got, "RunLogsDir()")
}

// TestRunsDir verifies runs directory path construction.
func TestRunsDir(t *testing.T) {
	t.Parallel()
	s := NewStore(nil, "/data/agency", nil)
	got := s.RunsDir("repo123")
	want := "/data/agency/repos/repo123/runs"
	assert.Equal(t, want, got, "RunsDir()")
}

// TestEnsureRunDir_Success verifies run directory creation.
func TestEnsureRunDir_Success(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	runDir, err := s.EnsureRunDir("repo123", "run456")
	require.NoError(t, err)

	wantDir := filepath.Join(dataDir, "repos", "repo123", "runs", "run456")
	assert.Equal(t, wantDir, runDir)

	// Verify directory exists
	_, err = os.Stat(runDir)
	assert.False(t, os.IsNotExist(err), "run directory should exist")

	// Verify logs directory exists
	logsDir := filepath.Join(runDir, "logs")
	_, err = os.Stat(logsDir)
	assert.False(t, os.IsNotExist(err), "logs directory should exist")
}

// TestEnsureRunDir_Collision verifies E_RUN_DIR_EXISTS on collision.
func TestEnsureRunDir_Collision(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// First call should succeed
	_, err := s.EnsureRunDir("repo123", "run456")
	require.NoError(t, err)

	// Second call should fail with E_RUN_DIR_EXISTS
	_, err = s.EnsureRunDir("repo123", "run456")
	require.Error(t, err, "want E_RUN_DIR_EXISTS")
	assert.Equal(t, errors.ERunDirExists, errors.GetCode(err))
}

// TestWriteInitialMeta_RequiredFields verifies meta.json has all required fields.
func TestWriteInitialMeta_RequiredFields(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create run directory first
	_, err := s.EnsureRunDir("repo123", "run456")
	require.NoError(t, err)

	// Write meta
	meta := NewRunMeta(
		"run456",
		"repo123",
		"test-name",
		"claude",
		"claude --model opus",
		"main",
		"agency/test-name-a3f2",
		"/path/to/worktree",
		now,
	)

	err = s.WriteInitialMeta("repo123", "run456", meta)
	require.NoError(t, err)

	// Read and verify
	metaPath := s.RunMetaPath("repo123", "run456")
	data, err := os.ReadFile(metaPath)
	require.NoError(t, err)

	// Verify JSON is valid
	assert.True(t, json.Valid(data), "meta.json is not valid JSON")

	// Parse and verify fields
	var parsed RunMeta
	require.NoError(t, json.Unmarshal(data, &parsed))

	// Required fields
	assert.Equal(t, "1.0", parsed.SchemaVersion)
	assert.Equal(t, "run456", parsed.RunID)
	assert.Equal(t, "repo123", parsed.RepoID)
	assert.Equal(t, "test-name", parsed.Name)
	assert.Equal(t, "claude", parsed.Runner)
	assert.Equal(t, "claude --model opus", parsed.RunnerCmd)
	assert.Equal(t, "main", parsed.ParentBranch)
	assert.Equal(t, "agency/test-name-a3f2", parsed.Branch)
	assert.Equal(t, "/path/to/worktree", parsed.WorktreePath)

	// Timestamp should be RFC3339 UTC (ends with Z)
	assert.Equal(t, "2026-01-10T12:00:00Z", parsed.CreatedAt)
	assert.True(t, strings.HasSuffix(parsed.CreatedAt, "Z"), "created_at should end with Z (UTC)")

	// tmux_session_name should be absent (empty string in Go)
	assert.Empty(t, parsed.TmuxSessionName, "tmux_session_name should be absent")
}

// TestWriteInitialMeta_IsAtomic verifies atomic write behavior.
func TestWriteInitialMeta_IsAtomic(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create run directory
	_, err := s.EnsureRunDir("repo123", "run456")
	require.NoError(t, err)

	// Write initial meta
	meta1 := NewRunMeta("run456", "repo123", "first-name", "claude", "claude", "main", "agency/first-a3f2", "/path/first", now)
	require.NoError(t, s.WriteInitialMeta("repo123", "run456", meta1))

	// Write again with different name
	meta2 := NewRunMeta("run456", "repo123", "second-name", "claude", "claude", "main", "agency/second-a3f2", "/path/second", now)
	require.NoError(t, s.WriteInitialMeta("repo123", "run456", meta2))

	// Read and verify it's the second version
	loaded, err := s.ReadMeta("repo123", "run456")
	require.NoError(t, err)

	assert.Equal(t, "second-name", loaded.Name, "second write")
}

// TestReadMeta_NotFound verifies E_RUN_NOT_FOUND for missing meta.
func TestReadMeta_NotFound(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	_, err := s.ReadMeta("nonexistent", "run")
	require.Error(t, err, "want E_RUN_NOT_FOUND")
	assert.Equal(t, errors.ERunNotFound, errors.GetCode(err))
}

// TestReadMeta_CorruptJSON verifies E_STORE_CORRUPT for invalid JSON.
func TestReadMeta_CorruptJSON(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Create run directory and corrupt meta.json
	runDir := s.RunDir("repo123", "run456")
	require.NoError(t, os.MkdirAll(runDir, 0755))
	metaPath := s.RunMetaPath("repo123", "run456")
	require.NoError(t, os.WriteFile(metaPath, []byte("{invalid json"), 0644))

	_, err := s.ReadMeta("repo123", "run456")
	require.Error(t, err, "want E_STORE_CORRUPT")
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

// TestUpdateMeta verifies the update function.
func TestUpdateMeta(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create run directory and initial meta
	_, err := s.EnsureRunDir("repo123", "run456")
	require.NoError(t, err)

	meta := NewRunMeta("run456", "repo123", "test-name", "claude", "claude", "main", "agency/test-a3f2", "/path/to/worktree", now)
	require.NoError(t, s.WriteInitialMeta("repo123", "run456", meta))

	// Update to add tmux session name
	err = s.UpdateMeta("repo123", "run456", func(m *RunMeta) {
		m.TmuxSessionName = "agency:run456"
	})
	require.NoError(t, err)

	// Read and verify
	loaded, err := s.ReadMeta("repo123", "run456")
	require.NoError(t, err)

	assert.Equal(t, "agency:run456", loaded.TmuxSessionName)
	// Original fields should be preserved
	assert.Equal(t, "test-name", loaded.Name)
}

// TestNewRunMeta verifies the constructor sets all fields correctly.
func TestNewRunMeta(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 10, 15, 30, 45, 0, time.FixedZone("EST", -5*3600))

	meta := NewRunMeta(
		"20260110153045-a3f2",
		"abc123def456",
		"my-test-run",
		"codex",
		"codex --full-auto",
		"develop",
		"agency/my-test-run-a3f2",
		"/path/to/worktree/directory",
		now,
	)

	assert.Equal(t, "1.0", meta.SchemaVersion)
	assert.Equal(t, "20260110153045-a3f2", meta.RunID)
	assert.Equal(t, "abc123def456", meta.RepoID)
	assert.Equal(t, "my-test-run", meta.Name)
	assert.Equal(t, "codex", meta.Runner)
	assert.Equal(t, "codex --full-auto", meta.RunnerCmd)
	assert.Equal(t, "develop", meta.ParentBranch)
	assert.Equal(t, "agency/my-test-run-a3f2", meta.Branch)
	assert.Equal(t, "/path/to/worktree/directory", meta.WorktreePath)
	// Should convert to UTC
	assert.Equal(t, "2026-01-10T20:30:45Z", meta.CreatedAt, "converted to UTC")
}

// TestJSONOmitEmptyFields verifies optional fields are omitted when empty.
func TestJSONOmitEmptyFields(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create run directory
	_, err := s.EnsureRunDir("repo123", "run456")
	require.NoError(t, err)

	// Write meta without optional fields
	meta := NewRunMeta("run456", "repo123", "test", "claude", "claude", "main", "agency/test-a3f2", "/path", now)
	require.NoError(t, s.WriteInitialMeta("repo123", "run456", meta))

	// Read raw JSON
	metaPath := s.RunMetaPath("repo123", "run456")
	data, err := os.ReadFile(metaPath)
	require.NoError(t, err)

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
		`"last_report_sync_at"`,
		`"last_report_hash"`,
		`"archive"`,
	}

	for _, field := range shouldBeAbsent {
		assert.NotContains(t, content, field, "field %s should be omitted when empty", field)
	}
}

// TestMetaSlice3PushFields verifies slice 03 push fields roundtrip correctly.
func TestMetaSlice3PushFields(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create run directory
	_, err := s.EnsureRunDir("repo123", "run456")
	require.NoError(t, err)

	// Write initial meta
	meta := NewRunMeta("run456", "repo123", "test", "claude", "claude", "main", "agency/test-a3f2", "/path", now)
	require.NoError(t, s.WriteInitialMeta("repo123", "run456", meta))

	// Update with slice 03 push fields
	pushTime := "2026-01-10T14:00:00Z"
	reportSyncTime := "2026-01-10T14:00:05Z"
	reportHash := "abcd1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab"

	err = s.UpdateMeta("repo123", "run456", func(m *RunMeta) {
		m.PRNumber = 42
		m.PRURL = "https://github.com/owner/repo/pull/42"
		m.LastPushAt = pushTime
		m.LastReportSyncAt = reportSyncTime
		m.LastReportHash = reportHash
	})
	require.NoError(t, err)

	// Read and verify
	loaded, err := s.ReadMeta("repo123", "run456")
	require.NoError(t, err)

	assert.Equal(t, 42, loaded.PRNumber)
	assert.Equal(t, "https://github.com/owner/repo/pull/42", loaded.PRURL)
	assert.Equal(t, pushTime, loaded.LastPushAt)
	assert.Equal(t, reportSyncTime, loaded.LastReportSyncAt)
	assert.Equal(t, reportHash, loaded.LastReportHash)

	// Verify original fields preserved
	assert.Equal(t, "test", loaded.Name)
}

// TestMetaSlice3FieldsInJSON verifies new fields appear correctly in JSON when set.
func TestMetaSlice3FieldsInJSON(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create run directory
	_, err := s.EnsureRunDir("repo123", "run456")
	require.NoError(t, err)

	// Write meta with all push fields set
	meta := NewRunMeta("run456", "repo123", "test", "claude", "claude", "main", "agency/test-a3f2", "/path", now)
	meta.LastReportSyncAt = "2026-01-10T14:00:00Z"
	meta.LastReportHash = "abc123"

	require.NoError(t, s.WriteInitialMeta("repo123", "run456", meta))

	// Read raw JSON
	metaPath := s.RunMetaPath("repo123", "run456")
	data, err := os.ReadFile(metaPath)
	require.NoError(t, err)

	content := string(data)

	// These fields should be present when set
	shouldBePresent := []string{
		`"last_report_sync_at"`,
		`"last_report_hash"`,
	}

	for _, field := range shouldBePresent {
		assert.Contains(t, content, field, "field %s should be present when set", field)
	}
}

// ---------------------------------------------------------------------------
// Error code coverage tests
// ---------------------------------------------------------------------------

// TestEnsureRunDir_MkdirFails verifies E_RUN_DIR_CREATE_FAILED when MkdirAll
// cannot create the runs directory (parent is read-only).
func TestEnsureRunDir_MkdirFails(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Create the repos directory, then make it read-only so MkdirAll for
	// repos/<repo_id>/runs/ will fail.
	reposDir := filepath.Join(dataDir, "repos")
	require.NoError(t, os.MkdirAll(reposDir, 0o700))
	require.NoError(t, os.Chmod(reposDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(reposDir, 0o700) })

	_, err := s.EnsureRunDir("repo123", "run456")
	require.Error(t, err, "expected E_RUN_DIR_CREATE_FAILED")
	assert.Equal(t, errors.ERunDirCreateFailed, errors.GetCode(err))
}

// TestEnsureRunDir_PathIsFile verifies E_RUN_DIR_CREATE_FAILED when a
// path component that should be a directory is actually a regular file.
// This triggers ENOTDIR from os.Mkdir, which is distinct from EEXIST.
func TestEnsureRunDir_PathIsFile(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Create the runs directory normally so MkdirAll for the parent succeeds.
	runsDir := s.RunsDir("repo123")
	require.NoError(t, os.MkdirAll(runsDir, 0o700))

	// Place a regular file at the run_id level, then try to create a
	// sub-path through it. os.Mkdir(".../<file>/<child>") returns ENOTDIR,
	// not EEXIST, so the code falls through to E_RUN_DIR_CREATE_FAILED.
	//
	// We achieve this by making the runID directory a file, then asking
	// EnsureRunDir to create a deeper path where runID contains a slash
	// is not possible, so instead we make the runs dir itself a file for
	// a different repoID.
	//
	// Simpler approach: create a file where the runs/ dir would be for a
	// fresh repoID, so MkdirAll for the runs dir fails.
	repoDir2 := filepath.Join(dataDir, "repos", "repo999")
	require.NoError(t, os.MkdirAll(repoDir2, 0o700))
	// Place a file where "runs" directory should be
	runsAsFile := filepath.Join(repoDir2, "runs")
	require.NoError(t, os.WriteFile(runsAsFile, []byte("blocker"), 0o644))

	_, err := s.EnsureRunDir("repo999", "run456")
	require.Error(t, err, "expected E_RUN_DIR_CREATE_FAILED")
	assert.Equal(t, errors.ERunDirCreateFailed, errors.GetCode(err))
}

// TestWriteInitialMeta_ReadOnlyDir verifies E_META_WRITE_FAILED when the
// run directory is read-only and WriteJSONAtomic cannot create a temp file.
func TestWriteInitialMeta_ReadOnlyDir(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create the run directory, then make it read-only.
	runDir, err := s.EnsureRunDir("repo123", "run456")
	require.NoError(t, err)
	require.NoError(t, os.Chmod(runDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(runDir, 0o700) })

	meta := NewRunMeta("run456", "repo123", "test", "claude", "claude", "main", "agency/test-a3f2", "/path", now)
	err = s.WriteInitialMeta("repo123", "run456", meta)
	require.Error(t, err, "expected E_META_WRITE_FAILED")
	assert.Equal(t, errors.EMetaWriteFailed, errors.GetCode(err))
}

// TestUpdateMeta_ReadOnlyDir verifies E_META_WRITE_FAILED when UpdateMeta
// reads the current meta successfully but fails to write back because the
// directory is read-only.
func TestUpdateMeta_ReadOnlyDir(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Create run directory and write initial meta while the dir is writable.
	_, err := s.EnsureRunDir("repo123", "run456")
	require.NoError(t, err)
	meta := NewRunMeta("run456", "repo123", "test", "claude", "claude", "main", "agency/test-a3f2", "/path", now)
	require.NoError(t, s.WriteInitialMeta("repo123", "run456", meta))

	// Make the run directory read-only so the atomic write (temp file
	// creation) inside UpdateMeta will fail.
	runDir := s.RunDir("repo123", "run456")
	require.NoError(t, os.Chmod(runDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(runDir, 0o700) })

	err = s.UpdateMeta("repo123", "run456", func(m *RunMeta) {
		m.TmuxSessionName = "should-fail"
	})
	require.Error(t, err, "expected E_META_WRITE_FAILED")
	assert.Equal(t, errors.EMetaWriteFailed, errors.GetCode(err))
}
