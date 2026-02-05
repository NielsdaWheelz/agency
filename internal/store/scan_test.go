package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScanAllRuns_ValidAndCorruptMeta verifies scanning handles valid and corrupt meta.json.
func TestScanAllRuns_ValidAndCorruptMeta(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	// Create repo r1 with 2 runs: one valid, one corrupt
	createValidMeta(t, dataDir, "r1", "20260110-a3f2")
	createCorruptMeta(t, dataDir, "r1", "20260110-a3ff")

	// Create repo r2 with 1 valid run
	createValidMeta(t, dataDir, "r2", "20260110-b111")

	// Optionally create repo.json for r1 only
	createRepoJSON(t, dataDir, "r1", "github:owner/repo1", "git@github.com:owner/repo1.git")

	records, err := ScanAllRuns(dataDir)
	require.NoError(t, err)

	require.Len(t, records, 3)

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
		assert.Equal(t, exp.repoID, rec.RepoID, "records[%d].RepoID", i)
		assert.Equal(t, exp.runID, rec.RunID, "records[%d].RunID", i)
		assert.Equal(t, exp.broken, rec.Broken, "records[%d].Broken", i)
		if exp.broken {
			assert.Nil(t, rec.Meta, "records[%d].Meta should be nil for broken record", i)
		} else {
			assert.NotNil(t, rec.Meta, "records[%d].Meta should not be nil for valid record", i)
		}
		hasRepo := rec.Repo != nil
		assert.Equal(t, exp.hasRepo, hasRepo, "records[%d].Repo != nil", i)
	}
}

// TestScanAllRuns_MismatchedMetaIdentity verifies RunRecord identity comes from dir names.
func TestScanAllRuns_MismatchedMetaIdentity(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	// Create a meta.json with mismatched run_id/repo_id
	runDir := filepath.Join(dataDir, "repos", "dir-repo", "runs", "dir-run")
	require.NoError(t, os.MkdirAll(runDir, 0755))

	meta := RunMeta{
		SchemaVersion: "1.0",
		RunID:         "meta-run-id",  // Different from directory name
		RepoID:        "meta-repo-id", // Different from directory name
		Name:          "test",
		Runner:        "claude",
		CreatedAt:     "2026-01-10T12:00:00Z",
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), data, 0644))

	records, err := ScanAllRuns(dataDir)
	require.NoError(t, err)

	require.Len(t, records, 1)

	rec := records[0]
	// RunRecord identity should come from directory names, NOT meta.json
	assert.Equal(t, "dir-repo", rec.RepoID, "from directory")
	assert.Equal(t, "dir-run", rec.RunID, "from directory")

	// But the meta should preserve the original values for debugging
	assert.Equal(t, "meta-repo-id", rec.Meta.RepoID, "preserved from meta")
	assert.Equal(t, "meta-run-id", rec.Meta.RunID, "preserved from meta")
}

// TestScanAllRuns_EmptyDataDir verifies empty result for missing directories.
func TestScanAllRuns_EmptyDataDir(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	// Don't create any repos

	records, err := ScanAllRuns(dataDir)
	require.NoError(t, err)
	assert.Empty(t, records)
}

// TestScanAllRuns_MissingReposDir verifies empty result when repos dir doesn't exist.
func TestScanAllRuns_MissingReposDir(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	// dataDir exists but repos/ does not

	records, err := ScanAllRuns(dataDir)
	require.NoError(t, err)
	assert.Empty(t, records)
}

// TestScanRunsForRepo_ScopesCorrectly verifies scoped scanning.
func TestScanRunsForRepo_ScopesCorrectly(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	// Create runs in multiple repos
	createValidMeta(t, dataDir, "r1", "run1")
	createValidMeta(t, dataDir, "r1", "run2")
	createValidMeta(t, dataDir, "r2", "run3")
	createValidMeta(t, dataDir, "r3", "run4")

	// Scan only r1
	records, err := ScanRunsForRepo(dataDir, "r1")
	require.NoError(t, err)

	require.Len(t, records, 2)

	for _, rec := range records {
		assert.Equal(t, "r1", rec.RepoID)
	}
}

// TestScanRunsForRepo_MissingRepo verifies empty result for nonexistent repo.
func TestScanRunsForRepo_MissingRepo(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	createValidMeta(t, dataDir, "r1", "run1")

	records, err := ScanRunsForRepo(dataDir, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, records)
}

// TestScanAllRuns_DeterministicOrdering verifies stable sort order.
func TestScanAllRuns_DeterministicOrdering(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	// Create runs in non-sorted order (simulate filesystem order variance)
	createValidMeta(t, dataDir, "z-repo", "z-run")
	createValidMeta(t, dataDir, "a-repo", "b-run")
	createValidMeta(t, dataDir, "m-repo", "a-run")
	createValidMeta(t, dataDir, "a-repo", "a-run")
	createValidMeta(t, dataDir, "m-repo", "z-run")

	records, err := ScanAllRuns(dataDir)
	require.NoError(t, err)

	// Expected order: a-repo/a-run, a-repo/b-run, m-repo/a-run, m-repo/z-run, z-repo/z-run
	expected := []struct{ repoID, runID string }{
		{"a-repo", "a-run"},
		{"a-repo", "b-run"},
		{"m-repo", "a-run"},
		{"m-repo", "z-run"},
		{"z-repo", "z-run"},
	}

	require.Len(t, records, len(expected))

	for i, exp := range expected {
		assert.Equal(t, exp.repoID, records[i].RepoID, "records[%d].RepoID", i)
		assert.Equal(t, exp.runID, records[i].RunID, "records[%d].RunID", i)
	}
}

// TestScanRunsForRepo_DeterministicOrdering verifies stable sort order within repo.
func TestScanRunsForRepo_DeterministicOrdering(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	createValidMeta(t, dataDir, "r1", "z-run")
	createValidMeta(t, dataDir, "r1", "a-run")
	createValidMeta(t, dataDir, "r1", "m-run")

	records, err := ScanRunsForRepo(dataDir, "r1")
	require.NoError(t, err)

	expected := []string{"a-run", "m-run", "z-run"}
	require.Len(t, records, len(expected))

	for i, exp := range expected {
		assert.Equal(t, exp, records[i].RunID, "records[%d].RunID", i)
	}
}

// TestScanAllRuns_BrokenMetaTypes verifies different types of broken meta.
func TestScanAllRuns_BrokenMetaTypes(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err)

	require.Len(t, records, 5)

	brokenCount := 0
	for _, rec := range records {
		if rec.Broken {
			brokenCount++
			assert.Nil(t, rec.Meta, "broken record %q should have Meta=nil", rec.RunID)
		}
	}

	assert.Equal(t, 4, brokenCount)
}

// TestScanAllRuns_RepoJoinBestEffort verifies repo.json join is best-effort.
func TestScanAllRuns_RepoJoinBestEffort(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err)

	require.Len(t, records, 3)

	// All runs should be non-broken (repo.json issues don't affect broken status)
	for _, rec := range records {
		assert.False(t, rec.Broken, "record %q should not be broken", rec.RunID)
	}

	// Only r1 should have Repo populated
	for _, rec := range records {
		switch rec.RepoID {
		case "r1":
			require.NotNil(t, rec.Repo, "r1 should have Repo populated")
			assert.Equal(t, "github:owner/repo1", rec.Repo.RepoKey)
			require.NotNil(t, rec.Repo.OriginURL)
			assert.Equal(t, "git@github.com:owner/repo1.git", *rec.Repo.OriginURL)
		case "r2", "r3":
			assert.Nil(t, rec.Repo, "%s should have Repo=nil (missing or corrupt repo.json)", rec.RepoID)
		}
	}
}

// TestScanAllRuns_RepoJoinCaching verifies repo.json is read once per repo.
func TestScanAllRuns_RepoJoinCaching(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	// Create multiple runs in same repo
	createValidMeta(t, dataDir, "r1", "run1")
	createValidMeta(t, dataDir, "r1", "run2")
	createValidMeta(t, dataDir, "r1", "run3")
	createRepoJSON(t, dataDir, "r1", "github:owner/repo1", "")

	records, err := ScanAllRuns(dataDir)
	require.NoError(t, err)

	require.Len(t, records, 3)

	// All records should point to the same RepoInfo (cached)
	firstRepo := records[0].Repo
	require.NotNil(t, firstRepo, "first record should have Repo")

	for i, rec := range records[1:] {
		assert.Same(t, firstRepo, rec.Repo, "records[%d].Repo should be same pointer (cached)", i+1)
	}
}

// Helper functions

func createValidMeta(t *testing.T, dataDir, repoID, runID string) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(runDir, 0755))

	meta := RunMeta{
		SchemaVersion: "1.0",
		RunID:         runID,
		RepoID:        repoID,
		Name:          "test-run",
		Runner:        "claude",
		ParentBranch:  "main",
		Branch:        "agency/test-" + runID,
		WorktreePath:  "/path/to/worktree/" + runID,
		CreatedAt:     "2026-01-10T12:00:00Z",
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), data, 0644))
}

func createCorruptMeta(t *testing.T, dataDir, repoID, runID string) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(runDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), []byte("{invalid json"), 0644))
}

func createMetaWithContent(t *testing.T, dataDir, repoID, runID, content string) {
	t.Helper()
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(runDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), []byte(content), 0644))
}

func createRepoJSON(t *testing.T, dataDir, repoID, repoKey, originURL string) {
	t.Helper()
	repoDir := filepath.Join(dataDir, "repos", repoID)
	require.NoError(t, os.MkdirAll(repoDir, 0755))

	rec := RepoRecord{
		SchemaVersion: "1.0",
		RepoKey:       repoKey,
		RepoID:        repoID,
		OriginURL:     originURL,
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "repo.json"), data, 0644))
}

func createCorruptRepoJSON(t *testing.T, dataDir, repoID string) {
	t.Helper()
	repoDir := filepath.Join(dataDir, "repos", repoID)
	require.NoError(t, os.MkdirAll(repoDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "repo.json"), []byte("{corrupt}"), 0644))
}

// Tests for LoadRepoIndexForScan

func TestLoadRepoIndexForScan_MissingFile(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	idx, err := LoadRepoIndexForScan(dataDir)
	require.NoError(t, err)
	assert.Nil(t, idx, "want nil for missing file")
}

func TestLoadRepoIndexForScan_InvalidJSON(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	path := filepath.Join(dataDir, "repo_index.json")
	require.NoError(t, os.WriteFile(path, []byte("{invalid}"), 0644))

	idx, err := LoadRepoIndexForScan(dataDir)
	require.Error(t, err, "want error for invalid JSON")
	assert.Nil(t, idx, "idx should be nil on error")
}

func TestLoadRepoIndexForScan_StandardFormat(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	idx, err := LoadRepoIndexForScan(dataDir)
	require.NoError(t, err)
	require.NotNil(t, idx)

	entry, ok := idx.Repos["github:owner/repo"]
	require.True(t, ok, "missing entry for github:owner/repo")
	assert.Equal(t, "abc123", entry.RepoID)
	assert.Len(t, entry.Paths, 2)
}

func TestLoadRepoIndexForScan_LegacyFormat(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	idx, err := LoadRepoIndexForScan(dataDir)
	require.NoError(t, err)
	require.NotNil(t, idx)

	entry, ok := idx.Repos["github:owner/repo"]
	require.True(t, ok, "missing entry for github:owner/repo (from legacy format)")
	assert.Equal(t, "abc123", entry.RepoID)
}

// Tests for PickRepoRoot

func TestPickRepoRoot_CwdWins(t *testing.T) {
	t.Parallel()
	cwdPath := t.TempDir() // exists

	idx := &RepoIndex{
		Repos: map[string]RepoIndexEntry{
			"github:owner/repo": {
				Paths: []string{"/some/other/path"},
			},
		},
	}

	result := PickRepoRoot("github:owner/repo", &cwdPath, idx)
	require.NotNil(t, result, "want cwdPath")
	assert.Equal(t, cwdPath, *result, "cwdPath wins")
}

func TestPickRepoRoot_MostRecentPathWins(t *testing.T) {
	t.Parallel()
	existingPath := t.TempDir()

	idx := &RepoIndex{
		Repos: map[string]RepoIndexEntry{
			"github:owner/repo": {
				Paths: []string{existingPath, "/nonexistent/path"},
			},
		},
	}

	result := PickRepoRoot("github:owner/repo", nil, idx)
	require.NotNil(t, result)
	assert.Equal(t, existingPath, *result)
}

func TestPickRepoRoot_FirstExistingPath(t *testing.T) {
	t.Parallel()
	existingPath := t.TempDir()

	idx := &RepoIndex{
		Repos: map[string]RepoIndexEntry{
			"github:owner/repo": {
				Paths: []string{"/nonexistent1", "/nonexistent2", existingPath},
			},
		},
	}

	result := PickRepoRoot("github:owner/repo", nil, idx)
	require.NotNil(t, result)
	assert.Equal(t, existingPath, *result, "first existing")
}

func TestPickRepoRoot_NilWhenNoneExist(t *testing.T) {
	t.Parallel()
	idx := &RepoIndex{
		Repos: map[string]RepoIndexEntry{
			"github:owner/repo": {
				Paths: []string{"/nonexistent1", "/nonexistent2"},
			},
		},
	}

	result := PickRepoRoot("github:owner/repo", nil, idx)
	assert.Nil(t, result)
}

func TestPickRepoRoot_NilWhenMissingEntry(t *testing.T) {
	t.Parallel()
	idx := &RepoIndex{
		Repos: map[string]RepoIndexEntry{
			"github:other/repo": {
				Paths: []string{"/some/path"},
			},
		},
	}

	result := PickRepoRoot("github:owner/repo", nil, idx)
	assert.Nil(t, result, "missing entry")
}

func TestPickRepoRoot_NilWhenIndexNil(t *testing.T) {
	t.Parallel()
	result := PickRepoRoot("github:owner/repo", nil, nil)
	assert.Nil(t, result, "nil index")
}

func TestPickRepoRoot_CwdNonexistentFallsThrough(t *testing.T) {
	t.Parallel()
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
	require.NotNil(t, result)
	assert.Equal(t, existingPath, *result, "cwd nonexistent, fallback to index")
}

func TestPickRepoRoot_FileNotDirectory(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0644))

	// cwdRepoRoot is a file, not a directory
	result := PickRepoRoot("github:owner/repo", &filePath, nil)
	assert.Nil(t, result, "file not directory")
}
