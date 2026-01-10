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
)

// ============================================================
// JSON output tests
// ============================================================

func TestWriteShowJSON_SchemaVersion(t *testing.T) {
	detail := &render.RunDetail{
		RepoID: "abc123",
		Broken: false,
	}

	var buf bytes.Buffer
	if err := render.WriteShowJSON(&buf, detail); err != nil {
		t.Fatalf("WriteShowJSON() error = %v", err)
	}

	var env render.ShowJSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if env.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q", env.SchemaVersion, "1.0")
	}

	if env.Data == nil {
		t.Error("Data is nil, want non-nil")
	}
}

func TestWriteShowJSON_NullData(t *testing.T) {
	var buf bytes.Buffer
	if err := render.WriteShowJSON(&buf, nil); err != nil {
		t.Fatalf("WriteShowJSON() error = %v", err)
	}

	var env render.ShowJSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if env.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q", env.SchemaVersion, "1.0")
	}

	if env.Data != nil {
		t.Errorf("Data = %v, want nil", env.Data)
	}
}

func TestWriteShowJSON_AllFields(t *testing.T) {
	repoKey := "github:owner/repo"
	originURL := "git@github.com:owner/repo.git"
	repoRoot := "/path/to/repo"

	meta := &store.RunMeta{
		SchemaVersion: "1.0",
		RunID:         "20260110-a3f2",
		RepoID:        "abc123",
		Title:         "test run",
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
	if err := render.WriteShowJSON(&buf, detail); err != nil {
		t.Fatalf("WriteShowJSON() error = %v", err)
	}

	var env render.ShowJSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	d := env.Data
	if d == nil {
		t.Fatal("Data is nil")
	}

	// Check top-level fields
	if d.RepoID != "abc123" {
		t.Errorf("RepoID = %q, want %q", d.RepoID, "abc123")
	}
	if d.RepoKey == nil || *d.RepoKey != repoKey {
		t.Errorf("RepoKey = %v, want %q", d.RepoKey, repoKey)
	}
	if d.OriginURL == nil || *d.OriginURL != originURL {
		t.Errorf("OriginURL = %v, want %q", d.OriginURL, originURL)
	}
	if d.Archived {
		t.Error("Archived = true, want false")
	}
	if d.Broken {
		t.Error("Broken = true, want false")
	}

	// Check meta fields
	if d.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if d.Meta.RunID != "20260110-a3f2" {
		t.Errorf("Meta.RunID = %q, want %q", d.Meta.RunID, "20260110-a3f2")
	}
	if d.Meta.Title != "test run" {
		t.Errorf("Meta.Title = %q, want %q", d.Meta.Title, "test run")
	}

	// Check derived fields
	if d.Derived.DerivedStatus != "active" {
		t.Errorf("Derived.DerivedStatus = %q, want %q", d.Derived.DerivedStatus, "active")
	}
	if !d.Derived.TmuxActive {
		t.Error("Derived.TmuxActive = false, want true")
	}
	if !d.Derived.WorktreePresent {
		t.Error("Derived.WorktreePresent = false, want true")
	}
	if !d.Derived.Report.Exists {
		t.Error("Derived.Report.Exists = false, want true")
	}
	if d.Derived.Report.Bytes != 256 {
		t.Errorf("Derived.Report.Bytes = %d, want 256", d.Derived.Report.Bytes)
	}

	// Check paths
	if d.Paths.RepoRoot == nil || *d.Paths.RepoRoot != repoRoot {
		t.Errorf("Paths.RepoRoot = %v, want %q", d.Paths.RepoRoot, repoRoot)
	}
	if d.Paths.WorktreeRoot != "/path/to/worktree" {
		t.Errorf("Paths.WorktreeRoot = %q, want %q", d.Paths.WorktreeRoot, "/path/to/worktree")
	}
}

func TestWriteShowJSON_BrokenRun(t *testing.T) {
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
	if err := render.WriteShowJSON(&buf, detail); err != nil {
		t.Fatalf("WriteShowJSON() error = %v", err)
	}

	var env render.ShowJSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	d := env.Data
	if d == nil {
		t.Fatal("Data is nil")
	}

	if !d.Broken {
		t.Error("Broken = false, want true")
	}
	if d.Meta != nil {
		t.Errorf("Meta = %v, want nil", d.Meta)
	}
	if d.Derived.DerivedStatus != status.StatusBroken {
		t.Errorf("DerivedStatus = %q, want %q", d.Derived.DerivedStatus, status.StatusBroken)
	}
}

// ============================================================
// Path output tests
// ============================================================

func TestWriteShowPaths_AllFields(t *testing.T) {
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
	if err := render.WriteShowPaths(&buf, data); err != nil {
		t.Fatalf("WriteShowPaths() error = %v", err)
	}

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

	if len(lines) != len(expectedLines) {
		t.Fatalf("got %d lines, want %d", len(lines), len(expectedLines))
	}

	for i, expected := range expectedLines {
		if lines[i] != expected {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], expected)
		}
	}
}

func TestWriteShowPaths_EmptyRepoRoot(t *testing.T) {
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
	if err := render.WriteShowPaths(&buf, data); err != nil {
		t.Fatalf("WriteShowPaths() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "repo_root: \n") {
		t.Errorf("expected empty repo_root value, got: %s", output)
	}
}

func TestWriteShowPaths_BrokenRun(t *testing.T) {
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
	if err := render.WriteShowPaths(&buf, data); err != nil {
		t.Fatalf("WriteShowPaths() error = %v", err)
	}

	output := buf.String()

	// Verify paths are still printed even if empty
	expectedKeyCount := 7 // all path keys must be present
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != expectedKeyCount {
		t.Errorf("got %d lines, want %d", len(lines), expectedKeyCount)
	}
}

// ============================================================
// Human output tests
// ============================================================

func TestWriteShowHuman_BasicOutput(t *testing.T) {
	data := render.ShowHumanData{
		RunID:           "20260110-a3f2",
		Title:           "test run",
		Runner:          "claude",
		CreatedAt:       "2026-01-10T12:00:00Z",
		RepoID:          "abc123",
		RepoKey:         "github:owner/repo",
		OriginURL:       "git@github.com:owner/repo.git",
		ParentBranch:    "main",
		Branch:          "agency/test-a3f2",
		WorktreePath:    "/path/to/worktree",
		WorktreePresent: true,
		TmuxSessionName: "agency_20260110-a3f2",
		TmuxActive:      true,
		ReportPath:      "/path/to/worktree/.agency/report.md",
		ReportExists:    true,
		ReportBytes:     256,
		SetupLogPath:    "/path/to/logs/setup.log",
		VerifyLogPath:   "/path/to/logs/verify.log",
		ArchiveLogPath:  "/path/to/logs/archive.log",
		DerivedStatus:   "active",
		Archived:        false,
	}

	var buf bytes.Buffer
	if err := render.WriteShowHuman(&buf, data); err != nil {
		t.Fatalf("WriteShowHuman() error = %v", err)
	}

	output := buf.String()

	// Check sections exist
	if !strings.Contains(output, "=== run ===") {
		t.Error("missing run section")
	}
	if !strings.Contains(output, "=== workspace ===") {
		t.Error("missing workspace section")
	}
	if !strings.Contains(output, "=== report ===") {
		t.Error("missing report section")
	}
	if !strings.Contains(output, "=== logs ===") {
		t.Error("missing logs section")
	}
	if !strings.Contains(output, "=== status ===") {
		t.Error("missing status section")
	}

	// Check key fields
	if !strings.Contains(output, "run_id: 20260110-a3f2") {
		t.Error("missing run_id")
	}
	if !strings.Contains(output, "title: test run") {
		t.Error("missing title")
	}
	if !strings.Contains(output, "derived_status: active") {
		t.Error("missing derived_status")
	}
}

func TestWriteShowHuman_UntitledRun(t *testing.T) {
	data := render.ShowHumanData{
		RunID:           "20260110-a3f2",
		Title:           "", // empty title
		Runner:          "claude",
		CreatedAt:       "2026-01-10T12:00:00Z",
		RepoID:          "abc123",
		ParentBranch:    "main",
		Branch:          "agency/test-a3f2",
		WorktreePath:    "/path/to/worktree",
		WorktreePresent: true,
		DerivedStatus:   "idle",
		Archived:        false,
	}

	var buf bytes.Buffer
	if err := render.WriteShowHuman(&buf, data); err != nil {
		t.Fatalf("WriteShowHuman() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "title: <untitled>") {
		t.Errorf("expected untitled placeholder, got: %s", output)
	}
}

func TestWriteShowHuman_ArchivedStatus(t *testing.T) {
	data := render.ShowHumanData{
		RunID:           "20260110-a3f2",
		Title:           "test run",
		Runner:          "claude",
		CreatedAt:       "2026-01-10T12:00:00Z",
		RepoID:          "abc123",
		ParentBranch:    "main",
		Branch:          "agency/test-a3f2",
		WorktreePath:    "/path/to/worktree",
		WorktreePresent: false, // missing worktree
		DerivedStatus:   "idle",
		Archived:        true,
	}

	var buf bytes.Buffer
	if err := render.WriteShowHuman(&buf, data); err != nil {
		t.Fatalf("WriteShowHuman() error = %v", err)
	}

	output := buf.String()
	// Status should include archived suffix
	if !strings.Contains(output, "derived_status: idle (archived)") {
		t.Errorf("expected archived suffix in status, got: %s", output)
	}
}

func TestWriteShowHuman_WithPR(t *testing.T) {
	data := render.ShowHumanData{
		RunID:           "20260110-a3f2",
		Title:           "test run",
		Runner:          "claude",
		CreatedAt:       "2026-01-10T12:00:00Z",
		RepoID:          "abc123",
		ParentBranch:    "main",
		Branch:          "agency/test-a3f2",
		WorktreePath:    "/path/to/worktree",
		WorktreePresent: true,
		PRNumber:        123,
		PRURL:           "https://github.com/owner/repo/pull/123",
		LastPushAt:      "2026-01-10T14:00:00Z",
		DerivedStatus:   "ready for review",
		Archived:        false,
	}

	var buf bytes.Buffer
	if err := render.WriteShowHuman(&buf, data); err != nil {
		t.Fatalf("WriteShowHuman() error = %v", err)
	}

	output := buf.String()

	// PR section should exist
	if !strings.Contains(output, "=== pr ===") {
		t.Error("missing pr section")
	}
	if !strings.Contains(output, "pr_number: 123") {
		t.Error("missing pr_number")
	}
	if !strings.Contains(output, "pr_url: https://github.com/owner/repo/pull/123") {
		t.Error("missing pr_url")
	}
}

func TestWriteShowHuman_WithWarnings(t *testing.T) {
	data := render.ShowHumanData{
		RunID:                  "20260110-a3f2",
		Title:                  "test run",
		Runner:                 "claude",
		CreatedAt:              "2026-01-10T12:00:00Z",
		RepoID:                 "abc123",
		ParentBranch:           "main",
		Branch:                 "agency/test-a3f2",
		WorktreePath:           "/path/to/worktree",
		WorktreePresent:        false,
		DerivedStatus:          "idle",
		Archived:               true,
		RepoNotFoundWarning:    true,
		WorktreeMissingWarning: true,
	}

	var buf bytes.Buffer
	if err := render.WriteShowHuman(&buf, data); err != nil {
		t.Fatalf("WriteShowHuman() error = %v", err)
	}

	output := buf.String()

	// Warnings section should exist
	if !strings.Contains(output, "=== warnings ===") {
		t.Error("missing warnings section")
	}
	if !strings.Contains(output, "warning: repo not found on disk") {
		t.Error("missing repo not found warning")
	}
	if !strings.Contains(output, "warning: worktree archived/missing") {
		t.Error("missing worktree missing warning")
	}
}

// ============================================================
// ResolveScriptLogPaths tests
// ============================================================

func TestResolveScriptLogPaths(t *testing.T) {
	runDir := "/path/to/run"
	setup, verify, archive := render.ResolveScriptLogPaths(runDir)

	if setup != "/path/to/run/logs/setup.log" {
		t.Errorf("setup = %q, want %q", setup, "/path/to/run/logs/setup.log")
	}
	if verify != "/path/to/run/logs/verify.log" {
		t.Errorf("verify = %q, want %q", verify, "/path/to/run/logs/verify.log")
	}
	if archive != "/path/to/run/logs/archive.log" {
		t.Errorf("archive = %q, want %q", archive, "/path/to/run/logs/archive.log")
	}
}

// ============================================================
// Integration-ish tests
// ============================================================

func TestShow_IntegrationWithFakeData_ValidRun(t *testing.T) {
	// Create temp data directory
	dataDir := t.TempDir()

	// Set AGENCY_DATA_DIR to our temp dir
	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer func() {
		if oldDataDir == "" {
			os.Unsetenv("AGENCY_DATA_DIR")
		} else {
			os.Setenv("AGENCY_DATA_DIR", oldDataDir)
		}
	}()

	// Create a valid run
	runID := "20260110-a3f2"
	repoID := "abc123"
	worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
	createValidMetaForShow(t, dataDir, repoID, runID, worktreePath, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))

	// Create worktree directory and report
	reportDir := filepath.Join(worktreePath, ".agency")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		t.Fatal(err)
	}
	reportContent := "# Test Report\n\nThis is a test report with enough content to exceed the 64 byte threshold for non-empty."
	if err := os.WriteFile(filepath.Join(reportDir, "report.md"), []byte(reportContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Scan and verify
	records, err := store.ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}

	rec := records[0]
	if rec.Broken {
		t.Error("run should not be broken")
	}
	if rec.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if rec.Meta.RunID != runID {
		t.Errorf("RunID = %q, want %q", rec.Meta.RunID, runID)
	}
}

func TestShow_IntegrationWithFakeData_BrokenRun(t *testing.T) {
	// Create temp data directory
	dataDir := t.TempDir()

	// Set AGENCY_DATA_DIR to our temp dir
	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer func() {
		if oldDataDir == "" {
			os.Unsetenv("AGENCY_DATA_DIR")
		} else {
			os.Setenv("AGENCY_DATA_DIR", oldDataDir)
		}
	}()

	// Create a broken run
	runID := "20260110-bad1"
	repoID := "abc123"
	createCorruptMetaForShow(t, dataDir, repoID, runID)

	// Scan and verify
	records, err := store.ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}

	rec := records[0]
	if !rec.Broken {
		t.Error("run should be broken")
	}
	if rec.Meta != nil {
		t.Error("Meta should be nil for broken run")
	}
	if rec.RunID != runID {
		t.Errorf("RunID = %q, want %q", rec.RunID, runID)
	}
}

func TestShow_IDResolutionErrors(t *testing.T) {
	// Create temp data directory
	dataDir := t.TempDir()

	// Set AGENCY_DATA_DIR to our temp dir
	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer func() {
		if oldDataDir == "" {
			os.Unsetenv("AGENCY_DATA_DIR")
		} else {
			os.Setenv("AGENCY_DATA_DIR", oldDataDir)
		}
	}()

	// Create two runs with similar prefixes
	createValidMetaForShow(t, dataDir, "r1", "20260110-a3f2", "/path/wt1", time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))
	createValidMetaForShow(t, dataDir, "r2", "20260110-a3ff", "/path/wt2", time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))

	// Test ambiguous resolution
	records, err := store.ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v", err)
	}

	refs := make([]struct {
		RunID string
	}, len(records))
	for i, rec := range records {
		refs[i].RunID = rec.RunID
	}

	// Verify we have both runs
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}

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

	if !foundA3f2 || !foundA3ff {
		t.Errorf("expected both runs with prefix '20260110-a3f', got: a3f2=%v, a3ff=%v", foundA3f2, foundA3ff)
	}
}

// ============================================================
// Error handling tests
// ============================================================

func TestHandleResolveError_Ambiguous(t *testing.T) {
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

	if err == nil {
		t.Fatal("expected error")
	}

	ae, ok := errors.AsAgencyError(err)
	if !ok {
		t.Fatal("expected AgencyError")
	}

	if ae.Code != errors.ERunIDAmbiguous {
		t.Errorf("Code = %v, want %v", ae.Code, errors.ERunIDAmbiguous)
	}

	// Error message should contain both candidates
	if !strings.Contains(ae.Msg, "20260110-a3f2") || !strings.Contains(ae.Msg, "20260110-a3ff") {
		t.Errorf("error message missing candidates: %s", ae.Msg)
	}
}

func TestHandleResolveError_NotFound(t *testing.T) {
	opts := ShowOpts{RunID: "nonexistent", JSON: false}
	var stdout, stderr bytes.Buffer

	// Use real ids.ErrNotFound type
	notFoundErr := &ids.ErrNotFound{Input: "nonexistent"}
	err := handleResolveError(notFoundErr, opts, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error")
	}

	ae, ok := errors.AsAgencyError(err)
	if !ok {
		t.Fatal("expected AgencyError")
	}

	if ae.Code != errors.ERunNotFound {
		t.Errorf("Code = %v, want %v", ae.Code, errors.ERunNotFound)
	}
}

func TestHandleResolveError_JSONMode(t *testing.T) {
	opts := ShowOpts{RunID: "nonexistent", JSON: true}
	var stdout, stderr bytes.Buffer

	// Use real ids.ErrNotFound type
	notFoundErr := &ids.ErrNotFound{Input: "nonexistent"}
	_ = handleResolveError(notFoundErr, opts, &stdout, &stderr)

	// In JSON mode, should output JSON envelope to stdout
	output := stdout.String()
	if output == "" {
		t.Error("expected JSON output to stdout")
	}

	var env render.ShowJSONEnvelope
	if err := json.Unmarshal([]byte(output), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if env.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q", env.SchemaVersion, "1.0")
	}
	if env.Data != nil {
		t.Errorf("Data = %v, want nil", env.Data)
	}
}

// Helper functions

func createValidMetaForShow(t *testing.T, dataDir, repoID, runID, worktreePath string, createdAt time.Time) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create logs directory
	logsDir := filepath.Join(runDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	meta := store.RunMeta{
		SchemaVersion:   "1.0",
		RunID:           runID,
		RepoID:          repoID,
		Title:           "Test Run " + runID,
		Runner:          "claude",
		RunnerCmd:       "claude",
		ParentBranch:    "main",
		Branch:          "agency/test-" + runID,
		WorktreePath:    worktreePath,
		CreatedAt:       createdAt.UTC().Format(time.RFC3339),
		TmuxSessionName: "agency_" + runID,
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "meta.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func createCorruptMetaForShow(t *testing.T, dataDir, repoID, runID string) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create logs directory
	logsDir := filepath.Join(runDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(runDir, "meta.json"), []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}
}
