package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIntegrationWorktreeMeta(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	meta := NewIntegrationWorktreeMeta(
		"20260131120000-a1b2",
		"my-feature",
		"abc123",
		"agency/my-feature-a1b2",
		"main",
		"/path/to/tree",
		now,
	)

	assert.Equal(t, "1.0", meta.SchemaVersion)
	assert.Equal(t, "20260131120000-a1b2", meta.WorktreeID)
	assert.Equal(t, "my-feature", meta.Name)
	assert.Equal(t, "main", meta.BaseBranch)
	assert.Equal(t, WorktreeStatePresent, meta.State)
	assert.Equal(t, "2026-01-31T12:00:00Z", meta.CreatedAt)
}

func TestEnsureIntegrationWorktreeDir(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "abc123"
	wtID := "20260131120000-a1b2"

	dir, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)
	assert.DirExists(t, dir)

	_, err = st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.Error(t, err, "expected error on duplicate")
}

func TestRemoveIntegrationWorktreeDir_RejectsPathTraversalOutsideDataDir(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	outsideRoot := t.TempDir()
	st := NewStore(fs.NewRealFS(), dataDir, time.Now)

	reposDir := filepath.Join(dataDir, "repos")
	repoID, err := filepath.Rel(reposDir, outsideRoot)
	require.NoError(t, err)

	const wtID = "wt-escape"
	targetDir := filepath.Join(outsideRoot, "integration_worktrees", wtID)
	require.NoError(t, os.MkdirAll(targetDir, 0o700))
	marker := filepath.Join(targetDir, "keep.txt")
	require.NoError(t, os.WriteFile(marker, []byte("do-not-delete"), 0o600))

	err = st.RemoveIntegrationWorktreeDir(repoID, wtID)
	require.Error(t, err)
	var guardErr *fs.ErrNotUnderPrefix
	require.ErrorAs(t, err, &guardErr)

	_, statErr := os.Stat(marker)
	require.NoError(t, statErr, "guard failure should not delete paths outside data dir")
}

func TestWriteAndReadIntegrationWorktreeMeta(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "abc123"
	wtID := "20260131120000-a1b2"

	// Create the directory first
	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)

	// Write meta
	meta := NewIntegrationWorktreeMeta(
		wtID,
		"my-feature",
		repoID,
		"agency/my-feature-a1b2",
		"main",
		"/path/to/tree",
		time.Now(),
	)

	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, wtID, meta))

	// Read back
	read, err := st.ReadIntegrationWorktreeMeta(repoID, wtID)
	require.NoError(t, err)

	assert.Equal(t, meta.WorktreeID, read.WorktreeID)
	assert.Equal(t, meta.Name, read.Name)
	assert.Equal(t, meta.BaseBranch, read.BaseBranch)
}

func TestReadIntegrationWorktreeMeta_NotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	_, err := st.ReadIntegrationWorktreeMeta("nonexistent", "notfound")
	require.Error(t, err, "expected error for nonexistent worktree")
}

func TestUpdateIntegrationWorktreeMeta(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "abc123"
	wtID := "20260131120000-a1b2"

	// Create directory and initial meta
	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)

	meta := NewIntegrationWorktreeMeta(
		wtID,
		"my-feature",
		repoID,
		"agency/my-feature-a1b2",
		"main",
		"/path/to/tree",
		time.Now(),
	)
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, wtID, meta))

	// Update to archived state
	err = st.UpdateIntegrationWorktreeMeta(repoID, wtID, func(m *IntegrationWorktreeMeta) {
		m.State = WorktreeStateArchived
	})
	require.NoError(t, err)

	// Verify update
	read, err := st.ReadIntegrationWorktreeMeta(repoID, wtID)
	require.NoError(t, err)
	assert.Equal(t, WorktreeStateArchived, read.State)
}

func TestScanIntegrationWorktreesForRepo(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create repo directory structure
	repoID := "abc123"
	wtDir := filepath.Join(tmpDir, "repos", repoID, "integration_worktrees")
	require.NoError(t, os.MkdirAll(wtDir, 0o755))

	// Create two worktrees
	wt1Dir := filepath.Join(wtDir, "20260131100000-a1b2")
	wt2Dir := filepath.Join(wtDir, "20260131110000-c3d4")
	require.NoError(t, os.Mkdir(wt1Dir, 0o755))
	require.NoError(t, os.Mkdir(wt2Dir, 0o755))

	// Write meta for wt1
	meta1 := &IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    "20260131100000-a1b2",
		Name:          "feature-a",
		RepoID:        repoID,
		BaseBranch:    "main",
		State:         WorktreeStatePresent,
		CreatedAt:     "2026-01-31T10:00:00Z",
	}
	data1, _ := json.MarshalIndent(meta1, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wt1Dir, "meta.json"), data1, 0o644))

	// Write meta for wt2
	meta2 := &IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    "20260131110000-c3d4",
		Name:          "feature-b",
		RepoID:        repoID,
		BaseBranch:    "main",
		State:         WorktreeStateArchived,
		CreatedAt:     "2026-01-31T11:00:00Z",
	}
	data2, _ := json.MarshalIndent(meta2, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wt2Dir, "meta.json"), data2, 0o644))

	// Create a broken worktree (no meta.json)
	wt3Dir := filepath.Join(wtDir, "20260131120000-e5f6")
	require.NoError(t, os.Mkdir(wt3Dir, 0o755))

	// Scan
	records, err := ScanIntegrationWorktreesForRepo(tmpDir, repoID)
	require.NoError(t, err)

	assert.Len(t, records, 3)

	// Records should be sorted by created_at (broken last)
	assert.Equal(t, "feature-a", records[0].Name)
	assert.Equal(t, "feature-b", records[1].Name)
	assert.True(t, records[2].Broken, "third record should be broken")
}

// TestLoad_CorruptWorktreeMeta verifies that reading a corrupt meta.json
// returns E_STORE_CORRUPT, which is the store-level indicator for a broken worktree.
func TestLoad_CorruptWorktreeMeta(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "abc123"
	wtID := "20260131120000-a1b2"

	// Create the worktree directory
	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)

	// Write invalid JSON to meta.json
	metaPath := st.IntegrationWorktreeMetaPath(repoID, wtID)
	require.NoError(t, os.WriteFile(metaPath, []byte("{invalid json!!!}"), 0o644))

	// ReadIntegrationWorktreeMeta should return E_STORE_CORRUPT
	_, err = st.ReadIntegrationWorktreeMeta(repoID, wtID)
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err), "expected E_STORE_CORRUPT for corrupt meta.json")
}

func TestReadIntegrationWorktreeMeta_MissingSchemaVersionReturnsStoreCorrupt(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "abc123"
	wtID := "20260131120000-a1b2"

	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)

	metaPath := st.IntegrationWorktreeMetaPath(repoID, wtID)
	require.NoError(t, os.WriteFile(metaPath, []byte(`{"worktree_id":"20260131120000-a1b2"}`), 0o644))

	_, err = st.ReadIntegrationWorktreeMeta(repoID, wtID)
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

func TestReadIntegrationWorktreeMeta_UnsupportedSchemaVersionReturnsStoreCorrupt(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "abc123"
	wtID := "20260131120000-a1b2"

	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)

	metaPath := st.IntegrationWorktreeMetaPath(repoID, wtID)
	require.NoError(t, os.WriteFile(metaPath, []byte(`{"schema_version":"9.9","worktree_id":"20260131120000-a1b2"}`), 0o644))

	_, err = st.ReadIntegrationWorktreeMeta(repoID, wtID)
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

func TestNewIntegrationWorktreeMergeMeta(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 20, 19, 0, 0, 0, time.UTC)
	meta := NewIntegrationWorktreeMergeMeta(
		"repo-1",
		"wt-1",
		"20260420190000-a1b2",
		"req-123",
		"squash",
		true,
		"/tmp/agency.json",
		now,
	)

	assert.Equal(t, SchemaVersion, meta.SchemaVersion)
	assert.Equal(t, WorktreeMergeStatusRunning, meta.Status)
	assert.Equal(t, WorktreeMergeStagePreflight, meta.Stage)
	assert.Equal(t, "2026-04-20T19:00:00Z", meta.StartedAt)
	assert.Equal(t, "2026-04-20T19:00:00Z", meta.UpdatedAt)
	assert.Equal(t, "/tmp/agency.json", meta.AgencyConfigPath)
}

func TestWriteReadAndUpdateIntegrationWorktreeMerge(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "repo-1"
	worktreeID := "wt-1"
	_, err := st.EnsureIntegrationWorktreeDir(repoID, worktreeID)
	require.NoError(t, err)

	meta := NewIntegrationWorktreeMergeMeta(
		repoID,
		worktreeID,
		"20260420190000-a1b2",
		"req-123",
		"squash",
		true,
		"",
		time.Date(2026, 4, 20, 19, 0, 0, 0, time.UTC),
	)
	meta.Branch = "agency/example-a1b2"
	meta.PRNumber = 42
	meta.PRURL = "https://example.test/pr/42"
	meta.VerifyLogPath = "/tmp/verify.log"

	require.NoError(t, st.WriteIntegrationWorktreeMerge(repoID, worktreeID, meta))

	read, err := st.ReadIntegrationWorktreeMerge(repoID, worktreeID)
	require.NoError(t, err)
	require.NotNil(t, read)
	assert.Equal(t, meta.AttemptID, read.AttemptID)
	assert.Equal(t, meta.PRNumber, read.PRNumber)
	assert.Equal(t, meta.VerifyLogPath, read.VerifyLogPath)

	err = st.UpdateIntegrationWorktreeMerge(repoID, worktreeID, func(m *IntegrationWorktreeMergeMeta) {
		m.Status = WorktreeMergeStatusSucceeded
		m.Stage = WorktreeMergeStageCompleted
		m.FinishedAt = "2026-04-20T19:01:00Z"
	})
	require.NoError(t, err)

	read, err = st.ReadIntegrationWorktreeMerge(repoID, worktreeID)
	require.NoError(t, err)
	require.NotNil(t, read)
	assert.Equal(t, WorktreeMergeStatusSucceeded, read.Status)
	assert.Equal(t, WorktreeMergeStageCompleted, read.Stage)
	assert.Equal(t, "2026-04-20T19:01:00Z", read.FinishedAt)
}

func TestReadIntegrationWorktreeMerge_NotFoundReturnsNil(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	meta, err := st.ReadIntegrationWorktreeMerge("repo-1", "wt-1")
	require.NoError(t, err)
	assert.Nil(t, meta)
}

func TestReadIntegrationWorktreeMerge_CorruptReturnsStoreCorrupt(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "repo-1"
	worktreeID := "wt-1"
	_, err := st.EnsureIntegrationWorktreeDir(repoID, worktreeID)
	require.NoError(t, err)

	mergePath := st.IntegrationWorktreeMergePath(repoID, worktreeID)
	require.NoError(t, os.WriteFile(mergePath, []byte("{invalid"), 0o644))

	_, err = st.ReadIntegrationWorktreeMerge(repoID, worktreeID)
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

// TestLoad_WorktreeNotFound verifies that reading a non-existent meta.json
// returns E_WORKTREE_NOT_FOUND.
func TestLoad_WorktreeNotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	_, err := st.ReadIntegrationWorktreeMeta("nonexistent-repo", "nonexistent-wt")
	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeNotFound, errors.GetCode(err), "expected E_WORKTREE_NOT_FOUND for missing meta.json")
}

// TestEnsureIntegrationWorktreeDir_DirExists verifies that creating a worktree directory
// that already exists returns E_WORKTREE_DIR_EXISTS.
func TestEnsureIntegrationWorktreeDir_DirExists(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "abc123"
	wtID := "20260131120000-a1b2"

	// First call should succeed
	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)

	// Second call should fail with E_WORKTREE_DIR_EXISTS
	_, err = st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeDirExists, errors.GetCode(err), "expected E_WORKTREE_DIR_EXISTS on duplicate")
}

// TestScanIntegrationWorktrees_CorruptMetaMarkedBroken verifies that the scan
// marks worktrees with corrupt meta.json as Broken=true.
func TestScanIntegrationWorktrees_CorruptMetaMarkedBroken(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	repoID := "abc123"
	wtDir := filepath.Join(tmpDir, "repos", repoID, "integration_worktrees")
	require.NoError(t, os.MkdirAll(wtDir, 0o755))

	// Create a worktree with corrupt meta.json
	corruptDir := filepath.Join(wtDir, "20260131100000-bad1")
	require.NoError(t, os.Mkdir(corruptDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(corruptDir, "meta.json"), []byte("not json"), 0o644))

	// Create a worktree with valid meta.json
	goodDir := filepath.Join(wtDir, "20260131110000-good")
	require.NoError(t, os.Mkdir(goodDir, 0o755))
	goodMeta := &IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    "20260131110000-good",
		Name:          "good-feature",
		RepoID:        repoID,
		BaseBranch:    "main",
		State:         WorktreeStatePresent,
		CreatedAt:     "2026-01-31T11:00:00Z",
	}
	data, _ := json.MarshalIndent(goodMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(goodDir, "meta.json"), data, 0o644))

	records, err := ScanIntegrationWorktreesForRepo(tmpDir, repoID)
	require.NoError(t, err)
	require.Len(t, records, 2)

	// Good record first (non-broken sorted before broken)
	assert.Equal(t, "good-feature", records[0].Name)
	assert.False(t, records[0].Broken)

	// Corrupt record is broken
	assert.True(t, records[1].Broken, "corrupt meta.json should produce Broken=true")
	assert.Equal(t, "20260131100000-bad1", records[1].WorktreeID)
	assert.Nil(t, records[1].Meta, "broken record should have nil Meta")
}
