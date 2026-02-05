package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/render"
	"github.com/NielsdaWheelz/agency/internal/status"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// JSON output tests
// ============================================================

func TestWriteShowJSON_SchemaVersion(t *testing.T) {
	t.Parallel()
	detail := &render.RunDetail{
		RepoID: "abc123",
		Broken: false,
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteShowJSON(&buf, detail))

	var env render.ShowJSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))

	assert.Equal(t, "1.0", env.SchemaVersion)
	require.NotNil(t, env.Data)
}

func TestWriteShowJSON_NullData(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, render.WriteShowJSON(&buf, nil))

	var env render.ShowJSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))

	assert.Equal(t, "1.0", env.SchemaVersion)
	assert.Nil(t, env.Data)
}

func TestWriteShowJSON_AllFields(t *testing.T) {
	t.Parallel()
	repoKey := "github:owner/repo"
	originURL := "git@github.com:owner/repo.git"
	repoRoot := "/path/to/repo"

	meta := &store.RunMeta{
		SchemaVersion: "1.0",
		RunID:         "20260110-a3f2",
		RepoID:        "abc123",
		Name:          "test run",
		Runner:        "claude",
		RunnerCmd:     "claude",
		ParentBranch:  "main",
		Branch:        "agency/test-a3f2",
		WorktreePath:  "/path/to/worktree",
		CreatedAt:     "2026-01-10T12:00:00Z",
	}

	detail := &render.RunDetail{
		Meta:      meta,
		RepoID:    "abc123",
		RepoKey:   &repoKey,
		OriginURL: &originURL,
		Archived:  false,
		Derived: render.DerivedJSON{
			DerivedStatus:   "active",
			TmuxActive:      true,
			WorktreePresent: true,
			Report: render.ReportJSON{
				Exists: true,
				Bytes:  256,
				Path:   "/path/to/worktree/.agency/report.md",
			},
			Logs: render.LogsJSON{
				SetupLogPath:   "/path/to/logs/setup.log",
				VerifyLogPath:  "/path/to/logs/verify.log",
				ArchiveLogPath: "/path/to/logs/archive.log",
			},
		},
		Paths: render.PathsJSON{
			RepoRoot:       &repoRoot,
			WorktreeRoot:   "/path/to/worktree",
			RunDir:         "/path/to/run",
			EventsPath:     "/path/to/run/events.jsonl",
			TranscriptPath: "/path/to/run/transcript.txt",
		},
		Broken: false,
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteShowJSON(&buf, detail))

	var env render.ShowJSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))

	d := env.Data
	require.NotNil(t, d)

	// Check top-level fields
	assert.Equal(t, "abc123", d.RepoID)
	require.NotNil(t, d.RepoKey)
	assert.Equal(t, repoKey, *d.RepoKey)
	require.NotNil(t, d.OriginURL)
	assert.Equal(t, originURL, *d.OriginURL)
	assert.False(t, d.Archived, "Archived = true, want false")
	assert.False(t, d.Broken, "Broken = true, want false")

	// Check meta fields
	require.NotNil(t, d.Meta)
	assert.Equal(t, "20260110-a3f2", d.Meta.RunID)
	assert.Equal(t, "test run", d.Meta.Name)

	// Check derived fields
	assert.Equal(t, "active", d.Derived.DerivedStatus)
	assert.True(t, d.Derived.TmuxActive, "Derived.TmuxActive = false, want true")
	assert.True(t, d.Derived.WorktreePresent, "Derived.WorktreePresent = false, want true")
	assert.True(t, d.Derived.Report.Exists, "Derived.Report.Exists = false, want true")
	assert.Equal(t, 256, d.Derived.Report.Bytes)

	// Check paths
	require.NotNil(t, d.Paths.RepoRoot)
	assert.Equal(t, repoRoot, *d.Paths.RepoRoot)
	assert.Equal(t, "/path/to/worktree", d.Paths.WorktreeRoot)
}

func TestWriteShowJSON_BrokenRun(t *testing.T) {
	t.Parallel()
	detail := &render.RunDetail{
		Meta:     nil, // broken
		RepoID:   "abc123",
		Archived: true,
		Derived: render.DerivedJSON{
			DerivedStatus:   status.StatusBroken,
			TmuxActive:      false,
			WorktreePresent: false,
		},
		Paths: render.PathsJSON{
			RepoRoot:       nil,
			WorktreeRoot:   "",
			RunDir:         "/path/to/run",
			EventsPath:     "/path/to/run/events.jsonl",
			TranscriptPath: "/path/to/run/transcript.txt",
		},
		Broken: true,
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteShowJSON(&buf, detail))

	var env render.ShowJSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))

	d := env.Data
	require.NotNil(t, d)

	assert.True(t, d.Broken, "Broken = false, want true")
	assert.Nil(t, d.Meta)
	assert.Equal(t, status.StatusBroken, d.Derived.DerivedStatus)
}

// ============================================================
// Path output tests
// ============================================================

func TestWriteShowPaths_AllFields(t *testing.T) {
	t.Parallel()
	data := render.ShowPathsData{
		RepoRoot:       "/path/to/repo",
		WorktreeRoot:   "/path/to/worktree",
		RunDir:         "/path/to/run",
		LogsDir:        "/path/to/run/logs",
		EventsPath:     "/path/to/run/events.jsonl",
		TranscriptPath: "/path/to/run/transcript.txt",
		ReportPath:     "/path/to/worktree/.agency/report.md",
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteShowPaths(&buf, data))

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	expectedLines := []string{
		"repo_root: /path/to/repo",
		"worktree_root: /path/to/worktree",
		"run_dir: /path/to/run",
		"logs_dir: /path/to/run/logs",
		"events_path: /path/to/run/events.jsonl",
		"transcript_path: /path/to/run/transcript.txt",
		"report_path: /path/to/worktree/.agency/report.md",
	}

	require.Len(t, lines, len(expectedLines))

	for i, expected := range expectedLines {
		assert.Equal(t, expected, lines[i], "line[%d]", i)
	}
}

func TestWriteShowPaths_EmptyRepoRoot(t *testing.T) {
	t.Parallel()
	data := render.ShowPathsData{
		RepoRoot:       "", // empty when unknown
		WorktreeRoot:   "/path/to/worktree",
		RunDir:         "/path/to/run",
		LogsDir:        "/path/to/run/logs",
		EventsPath:     "/path/to/run/events.jsonl",
		TranscriptPath: "/path/to/run/transcript.txt",
		ReportPath:     "/path/to/worktree/.agency/report.md",
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteShowPaths(&buf, data))

	output := buf.String()
	assert.Contains(t, output, "repo_root: \n")
}

func TestWriteShowPaths_BrokenRun(t *testing.T) {
	t.Parallel()
	// Broken run paths: repo_root, worktree_root, report_path empty
	data := render.ShowPathsData{
		RepoRoot:       "",
		WorktreeRoot:   "",
		RunDir:         "/path/to/run",
		LogsDir:        "/path/to/run/logs",
		EventsPath:     "/path/to/run/events.jsonl",
		TranscriptPath: "/path/to/run/transcript.txt",
		ReportPath:     "",
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteShowPaths(&buf, data))

	output := buf.String()

	// Verify paths are still printed even if empty
	expectedKeyCount := 7 // all path keys must be present
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.Len(t, lines, expectedKeyCount)
}

// ============================================================
// Human output tests
// ============================================================

func TestWriteShowHuman_BasicOutput(t *testing.T) {
	t.Parallel()
	data := render.ShowHumanData{
		RunID:            "20260110-a3f2",
		Name:             "test run",
		Runner:           "claude",
		CreatedAt:        "2026-01-10T12:00:00Z",
		RepoID:           "abc123",
		RepoKey:          "github:owner/repo",
		OriginURL:        "git@github.com:owner/repo.git",
		ParentBranch:     "main",
		Branch:           "agency/test-a3f2",
		WorktreePath:     "/path/to/worktree",
		WorktreePresent:  true,
		TmuxSessionName:  "agency_20260110-a3f2",
		TmuxActive:       true,
		PRNumber:         123,
		PRURL:            "https://github.com/owner/repo/pull/123",
		LastPushAt:       "2026-01-10T14:00:00Z",
		LastReportSyncAt: "2026-01-10T14:00:00Z",
		LastReportHash:   "abc123def456",
		ReportPath:       "/path/to/worktree/.agency/report.md",
		ReportExists:     true,
		ReportBytes:      256,
		SetupLogPath:     "/path/to/logs/setup.log",
		VerifyLogPath:    "/path/to/logs/verify.log",
		ArchiveLogPath:   "/path/to/logs/archive.log",
		DerivedStatus:    "active",
		Archived:         false,
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteShowHuman(&buf, data))

	output := buf.String()

	// Check spec-required fields exist in order
	assert.Contains(t, output, "run: 20260110-a3f2", "missing run field")
	assert.Contains(t, output, "name: test run", "missing name field")
	assert.Contains(t, output, "repo: abc123", "missing repo field")
	assert.Contains(t, output, "runner: claude", "missing runner field")
	assert.Contains(t, output, "parent: main", "missing parent field")
	assert.Contains(t, output, "branch: agency/test-a3f2", "missing branch field")
	assert.Contains(t, output, "worktree: /path/to/worktree", "missing worktree field")
	assert.Contains(t, output, "tmux: agency_20260110-a3f2", "missing tmux field")
	assert.Contains(t, output, "pr: https://github.com/owner/repo/pull/123 (#123)", "missing pr field with correct format")
	assert.Contains(t, output, "status: active", "missing status field")

	// Check blank line between worktree and tmux
	lines := strings.Split(output, "\n")
	worktreeIdx := -1
	tmuxIdx := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "worktree: ") {
			worktreeIdx = i
		}
		if strings.HasPrefix(line, "tmux: ") {
			tmuxIdx = i
		}
	}
	if worktreeIdx >= 0 && tmuxIdx >= 0 {
		assert.Equal(t, 2, tmuxIdx-worktreeIdx, "expected blank line between worktree and tmux")
	}
}

func TestWriteShowHuman_UntitledRun(t *testing.T) {
	t.Parallel()
	data := render.ShowHumanData{
		RunID:           "20260110-a3f2",
		Name:            "", // empty title
		Runner:          "claude",
		CreatedAt:       "2026-01-10T12:00:00Z",
		RepoID:          "abc123",
		ParentBranch:    "main",
		Branch:          "agency/test-a3f2",
		WorktreePath:    "/path/to/worktree",
		WorktreePresent: true,
		TmuxSessionName: "agency_20260110-a3f2",
		DerivedStatus:   "idle",
		Archived:        false,
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteShowHuman(&buf, data))

	output := buf.String()
	assert.Contains(t, output, "name: <untitled>")
}

func TestWriteShowHuman_ArchivedStatus(t *testing.T) {
	t.Parallel()
	data := render.ShowHumanData{
		RunID:           "20260110-a3f2",
		Name:            "test run",
		Runner:          "claude",
		CreatedAt:       "2026-01-10T12:00:00Z",
		RepoID:          "abc123",
		ParentBranch:    "main",
		Branch:          "agency/test-a3f2",
		WorktreePath:    "/path/to/worktree",
		WorktreePresent: false, // missing worktree
		TmuxSessionName: "agency_20260110-a3f2",
		DerivedStatus:   "idle",
		Archived:        true,
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteShowHuman(&buf, data))

	output := buf.String()
	// Status should include archived suffix
	assert.Contains(t, output, "status: idle (archived)")
}

func TestWriteShowHuman_WithPR(t *testing.T) {
	t.Parallel()
	data := render.ShowHumanData{
		RunID:            "20260110-a3f2",
		Name:             "test run",
		Runner:           "claude",
		CreatedAt:        "2026-01-10T12:00:00Z",
		RepoID:           "abc123",
		ParentBranch:     "main",
		Branch:           "agency/test-a3f2",
		WorktreePath:     "/path/to/worktree",
		WorktreePresent:  true,
		TmuxSessionName:  "agency_20260110-a3f2",
		TmuxActive:       true,
		PRNumber:         123,
		PRURL:            "https://github.com/owner/repo/pull/123",
		LastPushAt:       "2026-01-10T14:00:00Z",
		LastReportSyncAt: "2026-01-10T14:00:00Z",
		LastReportHash:   "abc123def456",
		DerivedStatus:    "ready for review",
		Archived:         false,
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteShowHuman(&buf, data))

	output := buf.String()

	// PR should be displayed in spec format: pr: <url> (#<number>)
	assert.Contains(t, output, "pr: https://github.com/owner/repo/pull/123 (#123)")
	assert.Contains(t, output, "last_push_at: 2026-01-10T14:00:00Z")
	assert.Contains(t, output, "last_report_sync_at: 2026-01-10T14:00:00Z")
	assert.Contains(t, output, "report_hash: abc123def456")
}

func TestWriteShowHuman_NoPR(t *testing.T) {
	t.Parallel()
	// Test that when PR fields are missing, the output shows "none" and "-"
	data := render.ShowHumanData{
		RunID:           "20260110-a3f2",
		Name:            "test run",
		Runner:          "claude",
		CreatedAt:       "2026-01-10T12:00:00Z",
		RepoID:          "abc123",
		ParentBranch:    "main",
		Branch:          "agency/test-a3f2",
		WorktreePath:    "/path/to/worktree",
		WorktreePresent: true,
		TmuxSessionName: "agency_20260110-a3f2",
		TmuxActive:      true,
		PRNumber:        0,  // no PR
		PRURL:           "", // no PR
		DerivedStatus:   "active",
		Archived:        false,
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteShowHuman(&buf, data))

	output := buf.String()

	// When no PR: pr: none (#-)
	assert.Contains(t, output, "pr: none (#-)")
	assert.Contains(t, output, "last_push_at: none")
	assert.Contains(t, output, "report_hash: none")
}

// ============================================================
// ResolveScriptLogPaths tests
// ============================================================

func TestResolveScriptLogPaths(t *testing.T) {
	t.Parallel()
	runDir := "/path/to/run"
	setup, verify, archive := render.ResolveScriptLogPaths(runDir)

	assert.Equal(t, "/path/to/run/logs/setup.log", setup)
	assert.Equal(t, "/path/to/run/logs/verify.log", verify)
	assert.Equal(t, "/path/to/run/logs/archive.log", archive)
}

// ============================================================
// Integration-ish tests
// ============================================================

func TestShow_IntegrationWithFakeData_ValidRun(t *testing.T) {
	t.Parallel()
	// Create temp data directory
	dataDir := t.TempDir()

	// Create a valid run
	runID := "20260110-a3f2"
	repoID := "abc123"
	worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
	createValidMetaForShow(t, dataDir, repoID, runID, worktreePath, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))

	// Create worktree directory and report
	reportDir := filepath.Join(worktreePath, ".agency")
	require.NoError(t, os.MkdirAll(reportDir, 0755))
	reportContent := "# Test Report\n\nThis is a test report with enough content to exceed the 64 byte threshold for non-empty."
	require.NoError(t, os.WriteFile(filepath.Join(reportDir, "report.md"), []byte(reportContent), 0644))

	// Scan and verify
	records, err := store.ScanAllRuns(dataDir)
	require.NoError(t, err)

	require.Len(t, records, 1)

	rec := records[0]
	assert.False(t, rec.Broken, "run should not be broken")
	require.NotNil(t, rec.Meta)
	assert.Equal(t, runID, rec.Meta.RunID)
}

func TestShow_IntegrationWithFakeData_BrokenRun(t *testing.T) {
	t.Parallel()
	// Create temp data directory
	dataDir := t.TempDir()

	// Create a broken run
	runID := "20260110-bad1"
	repoID := "abc123"
	createCorruptMetaForShow(t, dataDir, repoID, runID)

	// Scan and verify
	records, err := store.ScanAllRuns(dataDir)
	require.NoError(t, err)

	require.Len(t, records, 1)

	rec := records[0]
	assert.True(t, rec.Broken, "run should be broken")
	assert.Nil(t, rec.Meta, "Meta should be nil for broken run")
	assert.Equal(t, runID, rec.RunID)
}

func TestShow_IDResolutionErrors(t *testing.T) {
	t.Parallel()
	// Create temp data directory
	dataDir := t.TempDir()

	// Create two runs with similar prefixes
	createValidMetaForShow(t, dataDir, "r1", "20260110-a3f2", "/path/wt1", time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))
	createValidMetaForShow(t, dataDir, "r2", "20260110-a3ff", "/path/wt2", time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))

	// Test ambiguous resolution
	records, err := store.ScanAllRuns(dataDir)
	require.NoError(t, err)

	refs := make([]struct {
		RunID string
	}, len(records))
	for i, rec := range records {
		refs[i].RunID = rec.RunID
	}

	// Verify we have both runs
	require.Len(t, records, 2)

	// Prefix "20260110-a3f" should match both
	foundA3f2 := false
	foundA3ff := false
	for _, rec := range records {
		if strings.HasPrefix(rec.RunID, "20260110-a3f") {
			if rec.RunID == "20260110-a3f2" {
				foundA3f2 = true
			}
			if rec.RunID == "20260110-a3ff" {
				foundA3ff = true
			}
		}
	}

	assert.True(t, foundA3f2, "expected run 20260110-a3f2 to be found")
	assert.True(t, foundA3ff, "expected run 20260110-a3ff to be found")
}

// ============================================================
// Error handling tests
// ============================================================

func TestHandleResolveError_Ambiguous(t *testing.T) {
	t.Parallel()
	opts := ShowOpts{RunID: "test", JSON: false}
	var stdout, stderr bytes.Buffer

	// Use real ids.ErrAmbiguous type
	ambErr := &ids.ErrAmbiguous{
		Input: "20260110-a3f",
		Candidates: []ids.RunRef{
			{RunID: "20260110-a3f2", RepoID: "r1"},
			{RunID: "20260110-a3ff", RepoID: "r2"},
		},
	}

	err := handleResolveError(ambErr, opts, &stdout, &stderr)
	require.Error(t, err)

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError")

	assert.Equal(t, errors.ERunIDAmbiguous, ae.Code)

	// Error message should contain both candidates
	assert.Contains(t, ae.Msg, "20260110-a3f2")
	assert.Contains(t, ae.Msg, "20260110-a3ff")
}

func TestHandleResolveError_NotFound(t *testing.T) {
	t.Parallel()
	opts := ShowOpts{RunID: "nonexistent", JSON: false}
	var stdout, stderr bytes.Buffer

	// Use real ids.ErrNotFound type
	notFoundErr := &ids.ErrNotFound{Input: "nonexistent"}
	err := handleResolveError(notFoundErr, opts, &stdout, &stderr)
	require.Error(t, err)

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError")

	assert.Equal(t, errors.ERunNotFound, ae.Code)
}

func TestHandleResolveError_JSONMode(t *testing.T) {
	t.Parallel()
	opts := ShowOpts{RunID: "nonexistent", JSON: true}
	var stdout, stderr bytes.Buffer

	// Use real ids.ErrNotFound type
	notFoundErr := &ids.ErrNotFound{Input: "nonexistent"}
	_ = handleResolveError(notFoundErr, opts, &stdout, &stderr)

	// In JSON mode, should output JSON envelope to stdout
	output := stdout.String()
	assert.NotEmpty(t, output, "expected JSON output to stdout")

	var env render.ShowJSONEnvelope
	require.NoError(t, json.Unmarshal([]byte(output), &env))

	assert.Equal(t, "1.0", env.SchemaVersion)
	assert.Nil(t, env.Data)
}

// Helper functions

func createValidMetaForShow(t *testing.T, dataDir, repoID, runID, worktreePath string, createdAt time.Time) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(runDir, 0755))

	// Create logs directory
	logsDir := filepath.Join(runDir, "logs")
	require.NoError(t, os.MkdirAll(logsDir, 0755))

	meta := store.RunMeta{
		SchemaVersion:   "1.0",
		RunID:           runID,
		RepoID:          repoID,
		Name:            "Test Run " + runID,
		Runner:          "claude",
		RunnerCmd:       "claude",
		ParentBranch:    "main",
		Branch:          "agency/test-" + runID,
		WorktreePath:    worktreePath,
		CreatedAt:       createdAt.UTC().Format(time.RFC3339),
		TmuxSessionName: "agency_" + runID,
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), data, 0644))
}

func createCorruptMetaForShow(t *testing.T, dataDir, repoID, runID string) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(runDir, 0755))

	// Create logs directory
	logsDir := filepath.Join(runDir, "logs")
	require.NoError(t, os.MkdirAll(logsDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), []byte("{invalid json"), 0644))
}
