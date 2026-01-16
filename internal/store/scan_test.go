package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestScanAllRuns_ValidAndCorruptMeta verifies scanning handles valid and corrupt meta.json.
func TestScanAllRuns_ValidAndCorruptMeta(t *testing.T) {
	dataDir := t.TempDir()

	// Create repo r1 with 2 runs: one valid, one corrupt
	createValidMeta(t, dataDir, "r1", "20260110-a3f2")
	createCorruptMeta(t, dataDir, "r1", "20260110-a3ff")

	// Create repo r2 with 1 valid run
	createValidMeta(t, dataDir, "r2", "20260110-b111")

	// Optionally create repo.json for r1 only
	createRepoJSON(t, dataDir, "r1", "github:owner/repo1", "git@github.com:owner/repo1.git")

	records, err := ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v, want nil", err)
	}

	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(records))
	}

	// Verify records are sorted by RepoID, then RunID
	// Expected order: r1/20260110-a3f2, r1/20260110-a3ff, r2/20260110-b111
	expectations := []struct {
		repoID  string
		runID   string
		broken  bool
		hasRepo bool
	}{
		{"r1", "20260110-a3f2", false, true},
		{"r1", "20260110-a3ff", true, true},
		{"r2", "20260110-b111", false, false},
	}

	for i, exp := range expectations {
		rec := records[i]
		if rec.RepoID != exp.repoID {
			t.Errorf("records[%d].RepoID = %q, want %q", i, rec.RepoID, exp.repoID)
		}
		if rec.RunID != exp.runID {
			t.Errorf("records[%d].RunID = %q, want %q", i, rec.RunID, exp.runID)
		}
		if rec.Broken != exp.broken {
			t.Errorf("records[%d].Broken = %v, want %v", i, rec.Broken, exp.broken)
		}
		if exp.broken && rec.Meta != nil {
			t.Errorf("records[%d].Meta should be nil for broken record", i)
		}
		if !exp.broken && rec.Meta == nil {
			t.Errorf("records[%d].Meta should not be nil for valid record", i)
		}
		hasRepo := rec.Repo != nil
		if hasRepo != exp.hasRepo {
			t.Errorf("records[%d].Repo != nil = %v, want %v", i, hasRepo, exp.hasRepo)
		}
	}
}

// TestScanAllRuns_MismatchedMetaIdentity verifies RunRecord identity comes from dir names.
func TestScanAllRuns_MismatchedMetaIdentity(t *testing.T) {
	dataDir := t.TempDir()

	// Create a meta.json with mismatched run_id/repo_id
	runDir := filepath.Join(dataDir, "repos", "dir-repo", "runs", "dir-run")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}

	meta := RunMeta{
		SchemaVersion: "1.0",
		RunID:         "meta-run-id",  // Different from directory name
		RepoID:        "meta-repo-id", // Different from directory name
		Title:         "Test",
		Runner:        "claude",
		CreatedAt:     "2026-01-10T12:00:00Z",
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "meta.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	records, err := ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}

	rec := records[0]
	// RunRecord identity should come from directory names, NOT meta.json
	if rec.RepoID != "dir-repo" {
		t.Errorf("RepoID = %q, want %q (from directory)", rec.RepoID, "dir-repo")
	}
	if rec.RunID != "dir-run" {
		t.Errorf("RunID = %q, want %q (from directory)", rec.RunID, "dir-run")
	}

	// But the meta should preserve the original values for debugging
	if rec.Meta.RepoID != "meta-repo-id" {
		t.Errorf("Meta.RepoID = %q, want %q (preserved from meta)", rec.Meta.RepoID, "meta-repo-id")
	}
	if rec.Meta.RunID != "meta-run-id" {
		t.Errorf("Meta.RunID = %q, want %q (preserved from meta)", rec.Meta.RunID, "meta-run-id")
	}
}

// TestScanAllRuns_EmptyDataDir verifies empty result for missing directories.
func TestScanAllRuns_EmptyDataDir(t *testing.T) {
	dataDir := t.TempDir()
	// Don't create any repos

	records, err := ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Errorf("len(records) = %d, want 0", len(records))
	}
}

// TestScanAllRuns_MissingReposDir verifies empty result when repos dir doesn't exist.
func TestScanAllRuns_MissingReposDir(t *testing.T) {
	dataDir := t.TempDir()
	// dataDir exists but repos/ does not

	records, err := ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Errorf("records = %v, want empty slice", records)
	}
}

// TestScanRunsForRepo_ScopesCorrectly verifies scoped scanning.
func TestScanRunsForRepo_ScopesCorrectly(t *testing.T) {
	dataDir := t.TempDir()

	// Create runs in multiple repos
	createValidMeta(t, dataDir, "r1", "run1")
	createValidMeta(t, dataDir, "r1", "run2")
	createValidMeta(t, dataDir, "r2", "run3")
	createValidMeta(t, dataDir, "r3", "run4")

	// Scan only r1
	records, err := ScanRunsForRepo(dataDir, "r1")
	if err != nil {
		t.Fatalf("ScanRunsForRepo() error = %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}

	for _, rec := range records {
		if rec.RepoID != "r1" {
			t.Errorf("record has RepoID %q, want %q", rec.RepoID, "r1")
		}
	}
}

// TestScanRunsForRepo_MissingRepo verifies empty result for nonexistent repo.
func TestScanRunsForRepo_MissingRepo(t *testing.T) {
	dataDir := t.TempDir()
	createValidMeta(t, dataDir, "r1", "run1")

	records, err := ScanRunsForRepo(dataDir, "nonexistent")
	if err != nil {
		t.Fatalf("ScanRunsForRepo() error = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Errorf("records = %v, want empty slice", records)
	}
}

// TestScanAllRuns_DeterministicOrdering verifies stable sort order.
func TestScanAllRuns_DeterministicOrdering(t *testing.T) {
	dataDir := t.TempDir()

	// Create runs in non-sorted order (simulate filesystem order variance)
	createValidMeta(t, dataDir, "z-repo", "z-run")
	createValidMeta(t, dataDir, "a-repo", "b-run")
	createValidMeta(t, dataDir, "m-repo", "a-run")
	createValidMeta(t, dataDir, "a-repo", "a-run")
	createValidMeta(t, dataDir, "m-repo", "z-run")

	records, err := ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v", err)
	}

	// Expected order: a-repo/a-run, a-repo/b-run, m-repo/a-run, m-repo/z-run, z-repo/z-run
	expected := []struct{ repoID, runID string }{
		{"a-repo", "a-run"},
		{"a-repo", "b-run"},
		{"m-repo", "a-run"},
		{"m-repo", "z-run"},
		{"z-repo", "z-run"},
	}

	if len(records) != len(expected) {
		t.Fatalf("len(records) = %d, want %d", len(records), len(expected))
	}

	for i, exp := range expected {
		if records[i].RepoID != exp.repoID || records[i].RunID != exp.runID {
			t.Errorf("records[%d] = {%q, %q}, want {%q, %q}",
				i, records[i].RepoID, records[i].RunID, exp.repoID, exp.runID)
		}
	}
}

// TestScanRunsForRepo_DeterministicOrdering verifies stable sort order within repo.
func TestScanRunsForRepo_DeterministicOrdering(t *testing.T) {
	dataDir := t.TempDir()

	createValidMeta(t, dataDir, "r1", "z-run")
	createValidMeta(t, dataDir, "r1", "a-run")
	createValidMeta(t, dataDir, "r1", "m-run")

	records, err := ScanRunsForRepo(dataDir, "r1")
	if err != nil {
		t.Fatalf("ScanRunsForRepo() error = %v", err)
	}

	expected := []string{"a-run", "m-run", "z-run"}
	if len(records) != len(expected) {
		t.Fatalf("len(records) = %d, want %d", len(records), len(expected))
	}

	for i, exp := range expected {
		if records[i].RunID != exp {
			t.Errorf("records[%d].RunID = %q, want %q", i, records[i].RunID, exp)
		}
	}
}

// TestScanAllRuns_BrokenMetaTypes verifies different types of broken meta.
func TestScanAllRuns_BrokenMetaTypes(t *testing.T) {
	dataDir := t.TempDir()

	// Missing schema_version
	createMetaWithContent(t, dataDir, "r1", "missing-schema", `{
		"run_id": "missing-schema",
		"created_at": "2026-01-10T12:00:00Z"
	}`)

	// Missing created_at (empty string)
	createMetaWithContent(t, dataDir, "r1", "missing-created", `{
		"schema_version": "1.0",
		"run_id": "missing-created"
	}`)

	// Invalid JSON
	createMetaWithContent(t, dataDir, "r1", "invalid-json", `{not valid json}`)

	// Empty file
	createMetaWithContent(t, dataDir, "r1", "empty-file", ``)

	// Valid for comparison
	createValidMeta(t, dataDir, "r1", "valid-run")

	records, err := ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v", err)
	}

	if len(records) != 5 {
		t.Fatalf("len(records) = %d, want 5", len(records))
	}

	brokenCount := 0
	for _, rec := range records {
		if rec.Broken {
			brokenCount++
			if rec.Meta != nil {
				t.Errorf("broken record %q should have Meta=nil", rec.RunID)
			}
		}
	}

	if brokenCount != 4 {
		t.Errorf("brokenCount = %d, want 4", brokenCount)
	}
}

// TestScanAllRuns_RepoJoinBestEffort verifies repo.json join is best-effort.
func TestScanAllRuns_RepoJoinBestEffort(t *testing.T) {
	dataDir := t.TempDir()

	// Repo with valid repo.json
	createValidMeta(t, dataDir, "r1", "run1")
	createRepoJSON(t, dataDir, "r1", "github:owner/repo1", "git@github.com:owner/repo1.git")

	// Repo with corrupt repo.json
	createValidMeta(t, dataDir, "r2", "run2")
	createCorruptRepoJSON(t, dataDir, "r2")

	// Repo without repo.json
	createValidMeta(t, dataDir, "r3", "run3")

	records, err := ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(records))
	}

	// All runs should be non-broken (repo.json issues don't affect broken status)
	for _, rec := range records {
		if rec.Broken {
			t.Errorf("record %q should not be broken", rec.RunID)
		}
	}

	// Only r1 should have Repo populated
	for _, rec := range records {
		switch rec.RepoID {
		case "r1":
			if rec.Repo == nil {
				t.Error("r1 should have Repo populated")
			} else {
				if rec.Repo.RepoKey != "github:owner/repo1" {
					t.Errorf("r1 Repo.RepoKey = %q, want %q", rec.Repo.RepoKey, "github:owner/repo1")
				}
				if rec.Repo.OriginURL == nil || *rec.Repo.OriginURL != "git@github.com:owner/repo1.git" {
					t.Errorf("r1 Repo.OriginURL incorrect")
				}
			}
		case "r2", "r3":
			if rec.Repo != nil {
				t.Errorf("%s should have Repo=nil (missing or corrupt repo.json)", rec.RepoID)
			}
		}
	}
}

// TestScanAllRuns_RepoJoinCaching verifies repo.json is read once per repo.
func TestScanAllRuns_RepoJoinCaching(t *testing.T) {
	dataDir := t.TempDir()

	// Create multiple runs in same repo
	createValidMeta(t, dataDir, "r1", "run1")
	createValidMeta(t, dataDir, "r1", "run2")
	createValidMeta(t, dataDir, "r1", "run3")
	createRepoJSON(t, dataDir, "r1", "github:owner/repo1", "")

	records, err := ScanAllRuns(dataDir)
	if err != nil {
		t.Fatalf("ScanAllRuns() error = %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(records))
	}

	// All records should point to the same RepoInfo (cached)
	firstRepo := records[0].Repo
	if firstRepo == nil {
		t.Fatal("first record should have Repo")
	}

	for i, rec := range records[1:] {
		if rec.Repo != firstRepo {
			t.Errorf("records[%d].Repo should be same pointer (cached)", i+1)
		}
	}
}

// Helper functions

func createValidMeta(t *testing.T, dataDir, repoID, runID string) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}

	meta := RunMeta{
		SchemaVersion: "1.0",
		RunID:         runID,
		RepoID:        repoID,
		Title:         "Test Run",
		Runner:        "claude",
		ParentBranch:  "main",
		Branch:        "agency/test-" + runID,
		WorktreePath:  "/path/to/worktree/" + runID,
		CreatedAt:     "2026-01-10T12:00:00Z",
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "meta.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func createCorruptMeta(t *testing.T, dataDir, repoID, runID string) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "meta.json"), []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}
}

func createMetaWithContent(t *testing.T, dataDir, repoID, runID, content string) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "meta.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func createRepoJSON(t *testing.T, dataDir, repoID, repoKey, originURL string) {
	t.Helper()
	repoDir := filepath.Join(dataDir, "repos", repoID)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	rec := RepoRecord{
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

func createCorruptRepoJSON(t *testing.T, dataDir, repoID string) {
	t.Helper()
	repoDir := filepath.Join(dataDir, "repos", repoID)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "repo.json"), []byte("{corrupt}"), 0644); err != nil {
		t.Fatal(err)
	}
}

// Tests for LoadRepoIndexForScan

func TestLoadRepoIndexForScan_MissingFile(t *testing.T) {
	dataDir := t.TempDir()

	idx, err := LoadRepoIndexForScan(dataDir)
	if err != nil {
		t.Fatalf("LoadRepoIndexForScan() error = %v, want nil", err)
	}
	if idx != nil {
		t.Errorf("idx = %v, want nil for missing file", idx)
	}
}

func TestLoadRepoIndexForScan_InvalidJSON(t *testing.T) {
	dataDir := t.TempDir()

	path := filepath.Join(dataDir, "repo_index.json")
	if err := os.WriteFile(path, []byte("{invalid}"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := LoadRepoIndexForScan(dataDir)
	if err == nil {
		t.Fatal("LoadRepoIndexForScan() error = nil, want error for invalid JSON")
	}
	if idx != nil {
		t.Errorf("idx should be nil on error")
	}
}

func TestLoadRepoIndexForScan_StandardFormat(t *testing.T) {
	dataDir := t.TempDir()

	content := `{
		"schema_version": "1.0",
		"repos": {
			"github:owner/repo": {
				"repo_id": "abc123",
				"paths": ["/path/one", "/path/two"],
				"last_seen_at": "2026-01-10T12:00:00Z"
			}
		}
	}`
	path := filepath.Join(dataDir, "repo_index.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := LoadRepoIndexForScan(dataDir)
	if err != nil {
		t.Fatalf("LoadRepoIndexForScan() error = %v", err)
	}
	if idx == nil {
		t.Fatal("idx is nil, want non-nil")
	}

	entry, ok := idx.Repos["github:owner/repo"]
	if !ok {
		t.Fatal("missing entry for github:owner/repo")
	}
	if entry.RepoID != "abc123" {
		t.Errorf("entry.RepoID = %q, want %q", entry.RepoID, "abc123")
	}
	if len(entry.Paths) != 2 {
		t.Errorf("len(entry.Paths) = %d, want 2", len(entry.Paths))
	}
}

func TestLoadRepoIndexForScan_LegacyFormat(t *testing.T) {
	dataDir := t.TempDir()

	// Legacy format with "entries" key instead of "repos"
	content := `{
		"entries": {
			"github:owner/repo": {
				"repo_id": "abc123",
				"paths": ["/path/one"],
				"last_seen_at": "2026-01-10T12:00:00Z"
			}
		}
	}`
	path := filepath.Join(dataDir, "repo_index.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := LoadRepoIndexForScan(dataDir)
	if err != nil {
		t.Fatalf("LoadRepoIndexForScan() error = %v", err)
	}
	if idx == nil {
		t.Fatal("idx is nil, want non-nil")
	}

	entry, ok := idx.Repos["github:owner/repo"]
	if !ok {
		t.Fatal("missing entry for github:owner/repo (from legacy format)")
	}
	if entry.RepoID != "abc123" {
		t.Errorf("entry.RepoID = %q, want %q", entry.RepoID, "abc123")
	}
}

// Tests for PickRepoRoot

func TestPickRepoRoot_CwdWins(t *testing.T) {
	cwdPath := t.TempDir() // exists

	idx := &RepoIndex{
		Repos: map[string]RepoIndexEntry{
			"github:owner/repo": {
				Paths: []string{"/some/other/path"},
			},
		},
	}

	result := PickRepoRoot("github:owner/repo", &cwdPath, idx)
	if result == nil {
		t.Fatal("result is nil, want cwdPath")
	}
	if *result != cwdPath {
		t.Errorf("result = %q, want %q (cwdPath wins)", *result, cwdPath)
	}
}

func TestPickRepoRoot_MostRecentPathWins(t *testing.T) {
	existingPath := t.TempDir()

	idx := &RepoIndex{
		Repos: map[string]RepoIndexEntry{
			"github:owner/repo": {
				Paths: []string{existingPath, "/nonexistent/path"},
			},
		},
	}

	result := PickRepoRoot("github:owner/repo", nil, idx)
	if result == nil {
		t.Fatal("result is nil, want existingPath")
	}
	if *result != existingPath {
		t.Errorf("result = %q, want %q", *result, existingPath)
	}
}

func TestPickRepoRoot_FirstExistingPath(t *testing.T) {
	existingPath := t.TempDir()

	idx := &RepoIndex{
		Repos: map[string]RepoIndexEntry{
			"github:owner/repo": {
				Paths: []string{"/nonexistent1", "/nonexistent2", existingPath},
			},
		},
	}

	result := PickRepoRoot("github:owner/repo", nil, idx)
	if result == nil {
		t.Fatal("result is nil, want existingPath")
	}
	if *result != existingPath {
		t.Errorf("result = %q, want %q (first existing)", *result, existingPath)
	}
}

func TestPickRepoRoot_NilWhenNoneExist(t *testing.T) {
	idx := &RepoIndex{
		Repos: map[string]RepoIndexEntry{
			"github:owner/repo": {
				Paths: []string{"/nonexistent1", "/nonexistent2"},
			},
		},
	}

	result := PickRepoRoot("github:owner/repo", nil, idx)
	if result != nil {
		t.Errorf("result = %q, want nil", *result)
	}
}

func TestPickRepoRoot_NilWhenMissingEntry(t *testing.T) {
	idx := &RepoIndex{
		Repos: map[string]RepoIndexEntry{
			"github:other/repo": {
				Paths: []string{"/some/path"},
			},
		},
	}

	result := PickRepoRoot("github:owner/repo", nil, idx)
	if result != nil {
		t.Errorf("result = %q, want nil (missing entry)", *result)
	}
}

func TestPickRepoRoot_NilWhenIndexNil(t *testing.T) {
	result := PickRepoRoot("github:owner/repo", nil, nil)
	if result != nil {
		t.Errorf("result = %q, want nil (nil index)", *result)
	}
}

func TestPickRepoRoot_CwdNonexistentFallsThrough(t *testing.T) {
	existingPath := t.TempDir()
	nonexistent := "/this/does/not/exist"

	idx := &RepoIndex{
		Repos: map[string]RepoIndexEntry{
			"github:owner/repo": {
				Paths: []string{existingPath},
			},
		},
	}

	result := PickRepoRoot("github:owner/repo", &nonexistent, idx)
	if result == nil {
		t.Fatal("result is nil, want existingPath")
	}
	if *result != existingPath {
		t.Errorf("result = %q, want %q (cwd nonexistent, fallback to index)", *result, existingPath)
	}
}

func TestPickRepoRoot_FileNotDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// cwdRepoRoot is a file, not a directory
	result := PickRepoRoot("github:owner/repo", &filePath, nil)
	if result != nil {
		t.Errorf("result = %q, want nil (file not directory)", *result)
	}
}
