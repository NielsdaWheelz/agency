package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/render"
	"github.com/NielsdaWheelz/agency/internal/status"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// ============================================================
// Sorting tests
// ============================================================

func TestSortSummaries_ByCreatedAtDescending(t *testing.T) {
	t1 := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC) // newer
	t3 := time.Date(2026, 1, 10, 11, 0, 0, 0, time.UTC) // older

	summaries := []render.RunSummary{
		{RunID: "run1", CreatedAt: &t1},
		{RunID: "run2", CreatedAt: &t2},
		{RunID: "run3", CreatedAt: &t3},
	}

	sortSummaries(summaries)

	// Expected order: run2 (newest), run1, run3 (oldest)
	expected := []string{"run2", "run1", "run3"}
	for i, exp := range expected {
		if summaries[i].RunID != exp {
			t.Errorf("summaries[%d].RunID = %q, want %q", i, summaries[i].RunID, exp)
		}
	}
}

func TestSortSummaries_BrokenRunsLast(t *testing.T) {
	t1 := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC)

	summaries := []render.RunSummary{
		{RunID: "broken1", CreatedAt: nil, Broken: true},
		{RunID: "run1", CreatedAt: &t1},
		{RunID: "broken2", CreatedAt: nil, Broken: true},
		{RunID: "run2", CreatedAt: &t2},
	}

	sortSummaries(summaries)

	// Non-broken should come first (newer first), broken last (sorted by run_id)
	expected := []string{"run2", "run1", "broken1", "broken2"}
	for i, exp := range expected {
		if summaries[i].RunID != exp {
			t.Errorf("summaries[%d].RunID = %q, want %q", i, summaries[i].RunID, exp)
		}
	}
}

func TestSortSummaries_TieBreaker(t *testing.T) {
	t1 := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	summaries := []render.RunSummary{
		{RunID: "runC", CreatedAt: &t1},
		{RunID: "runA", CreatedAt: &t1},
		{RunID: "runB", CreatedAt: &t1},
	}

	sortSummaries(summaries)

	// Same timestamp: sort by run_id ascending
	expected := []string{"runA", "runB", "runC"}
	for i, exp := range expected {
		if summaries[i].RunID != exp {
			t.Errorf("summaries[%d].RunID = %q, want %q", i, summaries[i].RunID, exp)
		}
	}
}

func TestSortSummaries_AllBroken(t *testing.T) {
	summaries := []render.RunSummary{
		{RunID: "broken-z", CreatedAt: nil, Broken: true},
		{RunID: "broken-a", CreatedAt: nil, Broken: true},
		{RunID: "broken-m", CreatedAt: nil, Broken: true},
	}

	sortSummaries(summaries)

	// All broken: sort by run_id ascending
	expected := []string{"broken-a", "broken-m", "broken-z"}
	for i, exp := range expected {
		if summaries[i].RunID != exp {
			t.Errorf("summaries[%d].RunID = %q, want %q", i, summaries[i].RunID, exp)
		}
	}
}

// ============================================================
// JSON output tests
// ============================================================

func TestWriteLSJSON_SchemaVersion(t *testing.T) {
	var buf bytes.Buffer
	summaries := []render.RunSummary{}

	if err := render.WriteLSJSON(&buf, summaries); err != nil {
		t.Fatalf("WriteLSJSON() error = %v", err)
	}

	var env render.LSJSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if env.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q", env.SchemaVersion, "1.0")
	}

	if len(env.Data) != 0 {
		t.Errorf("len(Data) = %d, want 0", len(env.Data))
	}
}

func TestWriteLSJSON_AllFields(t *testing.T) {
	createdAt := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	lastPushAt := time.Date(2026, 1, 10, 14, 0, 0, 0, time.UTC)
	runner := "claude"
	repoKey := "github:owner/repo"
	originURL := "git@github.com:owner/repo.git"
	prNumber := 123
	prURL := "https://github.com/owner/repo/pull/123"

	summaries := []render.RunSummary{
		{
			RunID:           "20260110-a3f2",
			RepoID:          "abc123",
			RepoKey:         &repoKey,
			OriginURL:       &originURL,
			Title:           "test run",
			Runner:          &runner,
			CreatedAt:       &createdAt,
			LastPushAt:      &lastPushAt,
			TmuxActive:      true,
			WorktreePresent: true,
			Archived:        false,
			PRNumber:        &prNumber,
			PRURL:           &prURL,
			DerivedStatus:   "ready for review",
			Broken:          false,
		},
	}

	var buf bytes.Buffer
	if err := render.WriteLSJSON(&buf, summaries); err != nil {
		t.Fatalf("WriteLSJSON() error = %v", err)
	}

	var env render.LSJSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if len(env.Data) != 1 {
		t.Fatalf("len(Data) = %d, want 1", len(env.Data))
	}

	s := env.Data[0]

	// Check all fields
	if s.RunID != "20260110-a3f2" {
		t.Errorf("RunID = %q, want %q", s.RunID, "20260110-a3f2")
	}
	if s.RepoID != "abc123" {
		t.Errorf("RepoID = %q, want %q", s.RepoID, "abc123")
	}
	if s.RepoKey == nil || *s.RepoKey != repoKey {
		t.Errorf("RepoKey = %v, want %q", s.RepoKey, repoKey)
	}
	if s.OriginURL == nil || *s.OriginURL != originURL {
		t.Errorf("OriginURL = %v, want %q", s.OriginURL, originURL)
	}
	if s.Title != "test run" {
		t.Errorf("Title = %q, want %q", s.Title, "test run")
	}
	if s.Runner == nil || *s.Runner != "claude" {
		t.Errorf("Runner = %v, want %q", s.Runner, "claude")
	}
	if !s.TmuxActive {
		t.Error("TmuxActive = false, want true")
	}
	if !s.WorktreePresent {
		t.Error("WorktreePresent = false, want true")
	}
	if s.Archived {
		t.Error("Archived = true, want false")
	}
	if s.PRNumber == nil || *s.PRNumber != 123 {
		t.Errorf("PRNumber = %v, want 123", s.PRNumber)
	}
	if s.PRURL == nil || *s.PRURL != prURL {
		t.Errorf("PRURL = %v, want %q", s.PRURL, prURL)
	}
	if s.DerivedStatus != "ready for review" {
		t.Errorf("DerivedStatus = %q, want %q", s.DerivedStatus, "ready for review")
	}
	if s.Broken {
		t.Error("Broken = true, want false")
	}

	// Check timestamps are valid RFC3339 when parsed back from JSON
	// The JSON encoder uses RFC3339Nano format
	if s.CreatedAt == nil {
		t.Error("CreatedAt is nil")
	}
	if s.LastPushAt == nil {
		t.Error("LastPushAt is nil")
	}
}

func TestWriteLSJSON_BrokenRun(t *testing.T) {
	summaries := []render.RunSummary{
		{
			RunID:           "20260110-bad1",
			RepoID:          "abc123",
			RepoKey:         nil, // missing
			OriginURL:       nil, // missing
			Title:           "<broken>",
			Runner:          nil, // null for broken
			CreatedAt:       nil, // null for broken
			LastPushAt:      nil,
			TmuxActive:      false,
			WorktreePresent: false,
			Archived:        true,
			PRNumber:        nil,
			PRURL:           nil,
			DerivedStatus:   status.StatusBroken,
			Broken:          true,
		},
	}

	var buf bytes.Buffer
	if err := render.WriteLSJSON(&buf, summaries); err != nil {
		t.Fatalf("WriteLSJSON() error = %v", err)
	}

	var env render.LSJSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if len(env.Data) != 1 {
		t.Fatalf("len(Data) = %d, want 1", len(env.Data))
	}

	s := env.Data[0]
	if !s.Broken {
		t.Error("Broken = false, want true")
	}
	if s.Title != "<broken>" {
		t.Errorf("Title = %q, want %q", s.Title, "<broken>")
	}
	if s.Runner != nil {
		t.Errorf("Runner = %v, want nil", s.Runner)
	}
	if s.CreatedAt != nil {
		t.Errorf("CreatedAt = %v, want nil", s.CreatedAt)
	}
}

func TestWriteLSJSON_NilSummaries(t *testing.T) {
	var buf bytes.Buffer
	if err := render.WriteLSJSON(&buf, nil); err != nil {
		t.Fatalf("WriteLSJSON() error = %v", err)
	}

	var env render.LSJSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Should output empty array, not null
	if env.Data == nil {
		t.Error("Data is nil, want empty slice")
	}
}

// ============================================================
// Human output tests
// ============================================================

func TestWriteLSHuman_EmptyList(t *testing.T) {
	var buf bytes.Buffer
	rows := []render.RunSummaryHumanRow{}

	if err := render.WriteLSHuman(&buf, rows); err != nil {
		t.Fatalf("WriteLSHuman() error = %v", err)
	}

	// Empty list should produce no output
	if buf.Len() != 0 {
		t.Errorf("output = %q, want empty", buf.String())
	}
}

func TestWriteLSHuman_WithRows(t *testing.T) {
	rows := []render.RunSummaryHumanRow{
		{
			RunID:     "20260110-a3f2",
			Title:     "test run",
			Runner:    "claude",
			CreatedAt: "2 hours ago",
			Status:    "active",
			PR:        "#123",
		},
	}

	var buf bytes.Buffer
	if err := render.WriteLSHuman(&buf, rows); err != nil {
		t.Fatalf("WriteLSHuman() error = %v", err)
	}

	output := buf.String()

	// Check header exists
	if !bytes.Contains(buf.Bytes(), []byte("RUN_ID")) {
		t.Error("missing RUN_ID header")
	}
	if !bytes.Contains(buf.Bytes(), []byte("TITLE")) {
		t.Error("missing TITLE header")
	}
	if !bytes.Contains(buf.Bytes(), []byte("RUNNER")) {
		t.Error("missing RUNNER header")
	}

	// Check row data exists
	if !bytes.Contains(buf.Bytes(), []byte("20260110-a3f2")) {
		t.Errorf("missing run_id in output: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("test run")) {
		t.Errorf("missing title in output: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("#123")) {
		t.Errorf("missing PR in output: %s", output)
	}
}

func TestFormatHumanRow_TitleTruncation(t *testing.T) {
	longTitle := "this is a very long title that exceeds fifty characters limit"
	createdAt := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	runner := "claude"

	summary := render.RunSummary{
		RunID:         "run1",
		Title:         longTitle,
		Runner:        &runner,
		CreatedAt:     &createdAt,
		DerivedStatus: "active",
	}

	now := time.Date(2026, 1, 10, 14, 0, 0, 0, time.UTC)
	row := render.FormatHumanRow(summary, now)

	// Title should be truncated with ellipsis
	if len([]rune(row.Title)) > render.TitleMaxLen {
		t.Errorf("title length = %d, want <= %d", len([]rune(row.Title)), render.TitleMaxLen)
	}

	// Should end with ellipsis
	if !bytes.HasSuffix([]byte(row.Title), []byte("…")) {
		t.Errorf("truncated title should end with ellipsis: %q", row.Title)
	}
}

func TestFormatHumanRow_BrokenRun(t *testing.T) {
	summary := render.RunSummary{
		RunID:         "broken1",
		Broken:        true,
		Title:         "<broken>",
		DerivedStatus: status.StatusBroken,
	}

	now := time.Now()
	row := render.FormatHumanRow(summary, now)

	if row.Title != render.TitleBroken {
		t.Errorf("Title = %q, want %q", row.Title, render.TitleBroken)
	}
	if row.Runner != "" {
		t.Errorf("Runner = %q, want empty", row.Runner)
	}
	if row.CreatedAt != "" {
		t.Errorf("CreatedAt = %q, want empty", row.CreatedAt)
	}
}

func TestFormatHumanRow_UntitledRun(t *testing.T) {
	createdAt := time.Now()
	runner := "codex"

	summary := render.RunSummary{
		RunID:         "run1",
		Title:         "", // empty title
		Runner:        &runner,
		CreatedAt:     &createdAt,
		DerivedStatus: "idle",
	}

	row := render.FormatHumanRow(summary, time.Now())

	if row.Title != render.TitleUntitled {
		t.Errorf("Title = %q, want %q", row.Title, render.TitleUntitled)
	}
}

func TestFormatHumanRow_ArchivedStatus(t *testing.T) {
	createdAt := time.Now()
	runner := "claude"

	summary := render.RunSummary{
		RunID:         "run1",
		Runner:        &runner,
		CreatedAt:     &createdAt,
		DerivedStatus: "idle",
		Archived:      true,
	}

	row := render.FormatHumanRow(summary, time.Now())

	if row.Status != "idle (archived)" {
		t.Errorf("Status = %q, want %q", row.Status, "idle (archived)")
	}
}

// ============================================================
// Integration-ish test with fake data
// ============================================================

func TestLS_IntegrationWithFakeData(t *testing.T) {
	// Create temp data directory
	dataDir := t.TempDir()

	// Set AGENCY_DATA_DIR to our temp dir
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	// Create repos with runs
	createValidMetaForLS(t, dataDir, "r1", "20260110-a3f2", time.Date(2026, 1, 10, 14, 0, 0, 0, time.UTC))
	createValidMetaForLS(t, dataDir, "r2", "20260110-b111", time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))
	createCorruptMetaForLS(t, dataDir, "r2", "20260110-bad1")
	createRepoJSONForLS(t, dataDir, "r1", "github:owner/repo1", "git@github.com:owner/repo1.git")

	// Scan all runs
	records, err := store.ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(records))
	}

	// Convert to summaries (without tmux - use empty session map)
	tmuxSessions := make(map[string]bool)
	summaries := make([]render.RunSummary, len(records))
	for i, rec := range records {
		summaries[i] = recordToSummary(rec, tmuxSessions, nil)
	}

	// Sort
	sortSummaries(summaries)

	// Verify order: newest first, broken last
	// Expected: r1/20260110-a3f2 (2026-01-10T14:00), r2/20260110-b111 (2026-01-10T12:00), r2/20260110-bad1 (broken)
	expectedOrder := []struct {
		runID  string
		broken bool
	}{
		{"20260110-a3f2", false},
		{"20260110-b111", false},
		{"20260110-bad1", true},
	}

	for i, exp := range expectedOrder {
		if summaries[i].RunID != exp.runID {
			t.Errorf("summaries[%d].RunID = %q, want %q", i, summaries[i].RunID, exp.runID)
		}
		if summaries[i].Broken != exp.broken {
			t.Errorf("summaries[%d].Broken = %v, want %v", i, summaries[i].Broken, exp.broken)
		}
	}

	// Verify broken run has correct fields
	brokenIdx := 2
	if summaries[brokenIdx].Title != render.TitleBroken {
		t.Errorf("broken run Title = %q, want %q", summaries[brokenIdx].Title, render.TitleBroken)
	}
	if summaries[brokenIdx].DerivedStatus != status.StatusBroken {
		t.Errorf("broken run DerivedStatus = %q, want %q", summaries[brokenIdx].DerivedStatus, status.StatusBroken)
	}

	// Verify repo join
	if summaries[0].RepoKey == nil || *summaries[0].RepoKey != "github:owner/repo1" {
		t.Errorf("r1 run RepoKey = %v, want %q", summaries[0].RepoKey, "github:owner/repo1")
	}
	// r2 runs should have nil repo_key (no repo.json)
	if summaries[1].RepoKey != nil {
		t.Errorf("r2 run RepoKey = %v, want nil", summaries[1].RepoKey)
	}

	// Test JSON output
	var buf bytes.Buffer
	if err := render.WriteLSJSON(&buf, summaries); err != nil {
		t.Fatalf("WriteLSJSON() error = %v", err)
	}

	var env render.LSJSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if env.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q", env.SchemaVersion, "1.0")
	}
	if len(env.Data) != 3 {
		t.Errorf("len(Data) = %d, want 3", len(env.Data))
	}
}

// Helper functions for tests

func createValidMetaForLS(t *testing.T, dataDir, repoID, runID string, createdAt time.Time) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}

	meta := store.RunMeta{
		SchemaVersion: "1.0",
		RunID:         runID,
		RepoID:        repoID,
		Title:         "Test Run " + runID,
		Runner:        "claude",
		RunnerCmd:     "claude",
		ParentBranch:  "main",
		Branch:        "agency/test-" + runID,
		WorktreePath:  filepath.Join(dataDir, "repos", repoID, "worktrees", runID),
		CreatedAt:     createdAt.UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "meta.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func createCorruptMetaForLS(t *testing.T, dataDir, repoID, runID string) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "meta.json"), []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}
}

func createRepoJSONForLS(t *testing.T, dataDir, repoID, repoKey, originURL string) {
	t.Helper()
	repoDir := filepath.Join(dataDir, "repos", repoID)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	rec := store.RepoRecord{
		SchemaVersion: "1.0",
		RepoKey:       repoKey,
		RepoID:        repoID,
		OriginURL:     originURL,
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "repo.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
