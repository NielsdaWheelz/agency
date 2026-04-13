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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		assert.Equal(t, exp, summaries[i].RunID, "summaries[%d].RunID", i)
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
		assert.Equal(t, exp, summaries[i].RunID, "summaries[%d].RunID", i)
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
		assert.Equal(t, exp, summaries[i].RunID, "summaries[%d].RunID", i)
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
		assert.Equal(t, exp, summaries[i].RunID, "summaries[%d].RunID", i)
	}
}

// ============================================================
// JSON output tests
// ============================================================

func TestWriteLSJSON_SchemaVersion(t *testing.T) {
	var buf bytes.Buffer
	summaries := []render.RunSummary{}

	require.NoError(t, render.WriteLSJSON(&buf, summaries))

	var env render.LSJSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))

	assert.Equal(t, "1.0", env.SchemaVersion)
	assert.Len(t, env.Data, 0)
}

func TestWriteLSJSON_AllFields(t *testing.T) {
	createdAt := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	lastPushAt := time.Date(2026, 1, 10, 14, 0, 0, 0, time.UTC)
	runner := "claude-code"
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
			Name:            "test run",
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
	require.NoError(t, render.WriteLSJSON(&buf, summaries))

	var env render.LSJSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))

	require.Len(t, env.Data, 1)

	s := env.Data[0]

	// Check all fields
	assert.Equal(t, "20260110-a3f2", s.RunID)
	assert.Equal(t, "abc123", s.RepoID)
	require.NotNil(t, s.RepoKey)
	assert.Equal(t, repoKey, *s.RepoKey)
	require.NotNil(t, s.OriginURL)
	assert.Equal(t, originURL, *s.OriginURL)
	assert.Equal(t, "test run", s.Name)
	require.NotNil(t, s.Runner)
	assert.Equal(t, "claude-code", *s.Runner)
	assert.True(t, s.TmuxActive, "TmuxActive")
	assert.True(t, s.WorktreePresent, "WorktreePresent")
	assert.False(t, s.Archived, "Archived")
	require.NotNil(t, s.PRNumber)
	assert.Equal(t, 123, *s.PRNumber)
	require.NotNil(t, s.PRURL)
	assert.Equal(t, prURL, *s.PRURL)
	assert.Equal(t, "ready for review", s.DerivedStatus)
	assert.False(t, s.Broken, "Broken")

	// Check timestamps are valid RFC3339 when parsed back from JSON
	// The JSON encoder uses RFC3339Nano format
	assert.NotNil(t, s.CreatedAt, "CreatedAt")
	assert.NotNil(t, s.LastPushAt, "LastPushAt")
}

func TestWriteLSJSON_BrokenRun(t *testing.T) {
	summaries := []render.RunSummary{
		{
			RunID:           "20260110-bad1",
			RepoID:          "abc123",
			RepoKey:         nil, // missing
			OriginURL:       nil, // missing
			Name:            "<broken>",
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
	require.NoError(t, render.WriteLSJSON(&buf, summaries))

	var env render.LSJSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))

	require.Len(t, env.Data, 1)

	s := env.Data[0]
	assert.True(t, s.Broken, "Broken")
	assert.Equal(t, "<broken>", s.Name)
	assert.Nil(t, s.Runner, "Runner")
	assert.Nil(t, s.CreatedAt, "CreatedAt")
}

func TestWriteLSJSON_NilSummaries(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, render.WriteLSJSON(&buf, nil))

	var env render.LSJSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))

	// Should output empty array, not null
	require.NotNil(t, env.Data, "Data should be empty slice, not nil")
}

// ============================================================
// Human output tests
// ============================================================

func TestWriteLSHuman_EmptyList_RepoScope(t *testing.T) {
	var buf bytes.Buffer
	rows := []render.RunSummaryHumanRow{}
	ctx := render.LSContext{
		Scope:            render.LSScopeRepo,
		IncludesArchived: false,
	}

	require.NoError(t, render.WriteLSHuman(&buf, rows, ctx))

	// Empty list in repo scope without --all should suggest using --all
	expected := "no active runs (use --all to include archived)\n"
	assert.Equal(t, expected, buf.String())
}

func TestWriteLSHuman_EmptyList_RepoScopeWithAll(t *testing.T) {
	var buf bytes.Buffer
	rows := []render.RunSummaryHumanRow{}
	ctx := render.LSContext{
		Scope:            render.LSScopeRepo,
		IncludesArchived: true,
	}

	require.NoError(t, render.WriteLSHuman(&buf, rows, ctx))

	// Empty list in repo scope with --all should just say no runs
	expected := "no runs found\n"
	assert.Equal(t, expected, buf.String())
}

func TestWriteLSHuman_EmptyList_AllReposScope(t *testing.T) {
	var buf bytes.Buffer
	rows := []render.RunSummaryHumanRow{}
	ctx := render.LSContext{
		Scope:            render.LSScopeAllRepos,
		IncludesArchived: false,
	}

	require.NoError(t, render.WriteLSHuman(&buf, rows, ctx))

	// Empty list in all-repos scope should just say no runs
	expected := "no runs found\n"
	assert.Equal(t, expected, buf.String())
}

func TestWriteLSHuman_WithRows(t *testing.T) {
	rows := []render.RunSummaryHumanRow{
		{
			RunID:   "20260110-a3f2",
			Name:    "test run",
			Status:  "active",
			Summary: "Implementing feature",
			PR:      "#123",
		},
	}
	ctx := render.LSContext{
		Scope:            render.LSScopeRepo,
		IncludesArchived: false,
	}

	var buf bytes.Buffer
	require.NoError(t, render.WriteLSHuman(&buf, rows, ctx))

	output := buf.String()

	// Check header exists
	assert.Contains(t, output, "RUN_ID", "missing RUN_ID header")
	assert.Contains(t, output, "NAME", "missing NAME header")
	assert.Contains(t, output, "SUMMARY", "missing SUMMARY header")

	// Check row data exists
	assert.Contains(t, output, "20260110-a3f2", "missing run_id in output")
	assert.Contains(t, output, "test run", "missing name in output")
	assert.Contains(t, output, "#123", "missing PR in output")
}

func TestFormatHumanRow_TitleTruncation(t *testing.T) {
	longTitle := "this is a very long title that exceeds fifty characters limit"
	createdAt := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	runner := "claude-code"

	summary := render.RunSummary{
		RunID:         "run1",
		Name:          longTitle,
		Runner:        &runner,
		CreatedAt:     &createdAt,
		DerivedStatus: "active",
	}

	row := render.FormatHumanRow(summary)

	// Title should be truncated with ellipsis
	assert.LessOrEqual(t, len([]rune(row.Name)), render.NameMaxLen, "title length exceeds max")

	// Should end with ellipsis
	assert.True(t, bytes.HasSuffix([]byte(row.Name), []byte("…")), "truncated title should end with ellipsis: %q", row.Name)
}

func TestFormatHumanRow_BrokenRun(t *testing.T) {
	summary := render.RunSummary{
		RunID:         "broken1",
		Broken:        true,
		Name:          "<broken>",
		DerivedStatus: status.StatusBroken,
	}

	row := render.FormatHumanRow(summary)

	assert.Equal(t, render.NameBroken, row.Name)
	assert.Equal(t, "-", row.Summary)
}

func TestFormatHumanRow_UntitledRun(t *testing.T) {
	createdAt := time.Now()
	runner := "codex"

	summary := render.RunSummary{
		RunID:         "run1",
		Name:          "", // empty title
		Runner:        &runner,
		CreatedAt:     &createdAt,
		DerivedStatus: "idle",
	}

	row := render.FormatHumanRow(summary)

	assert.Equal(t, render.NameUntitled, row.Name)
}

func TestFormatHumanRow_ArchivedStatus(t *testing.T) {
	createdAt := time.Now()
	runner := "claude-code"

	summary := render.RunSummary{
		RunID:         "run1",
		Runner:        &runner,
		CreatedAt:     &createdAt,
		DerivedStatus: "idle",
		Archived:      true,
	}

	row := render.FormatHumanRow(summary)

	assert.Equal(t, "idle (archived)", row.Status)
}

// ============================================================
// Integration-ish test with fake data
// ============================================================

func TestLS_IntegrationWithFakeData(t *testing.T) {
	t.Parallel()

	// Create temp data directory
	dataDir := t.TempDir()

	// Create repos with runs
	createValidMetaForLS(t, dataDir, "r1", "20260110-a3f2", time.Date(2026, 1, 10, 14, 0, 0, 0, time.UTC))
	createValidMetaForLS(t, dataDir, "r2", "20260110-b111", time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))
	createCorruptMetaForLS(t, dataDir, "r2", "20260110-bad1")
	createRepoJSONForLS(t, dataDir, "r1", "github:owner/repo1", "git@github.com:owner/repo1.git")

	// Scan all runs
	records, err := store.ScanAllRuns(dataDir)
	require.NoError(t, err)
	require.Len(t, records, 3)

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
		assert.Equal(t, exp.runID, summaries[i].RunID, "summaries[%d].RunID", i)
		assert.Equal(t, exp.broken, summaries[i].Broken, "summaries[%d].Broken", i)
	}

	// Verify broken run has correct fields
	brokenIdx := 2
	assert.Equal(t, render.NameBroken, summaries[brokenIdx].Name, "broken run Title")
	assert.Equal(t, status.StatusBroken, summaries[brokenIdx].DerivedStatus, "broken run DerivedStatus")

	// Verify repo join
	require.NotNil(t, summaries[0].RepoKey, "r1 run RepoKey")
	assert.Equal(t, "github:owner/repo1", *summaries[0].RepoKey)
	// r2 runs should have nil repo_key (no repo.json)
	assert.Nil(t, summaries[1].RepoKey, "r2 run RepoKey")

	// Test JSON output
	var buf bytes.Buffer
	require.NoError(t, render.WriteLSJSON(&buf, summaries))

	var env render.LSJSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))

	assert.Equal(t, "1.0", env.SchemaVersion)
	assert.Len(t, env.Data, 3)
}

// Helper functions for tests

func createValidMetaForLS(t *testing.T, dataDir, repoID, runID string, createdAt time.Time) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(runDir, 0755))

	meta := store.RunMeta{
		SchemaVersion: "1.0",
		RunID:         runID,
		RepoID:        repoID,
		Name:          "Test Run " + runID,
		Runner:        "claude-code",
		RunnerCmd:     "claude",
		ParentBranch:  "main",
		Branch:        "agency/test-" + runID,
		WorktreePath:  filepath.Join(dataDir, "repos", repoID, "worktrees", runID),
		CreatedAt:     createdAt.UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), data, 0644))
}

func createCorruptMetaForLS(t *testing.T, dataDir, repoID, runID string) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(runDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), []byte("{invalid json"), 0644))
}

func createRepoJSONForLS(t *testing.T, dataDir, repoID, repoKey, originURL string) {
	t.Helper()
	repoDir := filepath.Join(dataDir, "repos", repoID)
	require.NoError(t, os.MkdirAll(repoDir, 0755))

	rec := store.RepoRecord{
		SchemaVersion: "1.0",
		RepoKey:       repoKey,
		RepoID:        repoID,
		OriginURL:     originURL,
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "repo.json"), data, 0644))
}
