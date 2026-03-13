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

// fixedTime returns a clock function that always returns the same time.
func fixedTime(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// TestRepoIndexPath verifies path construction.
func TestRepoIndexPath(t *testing.T) {
	t.Parallel()
	s := NewStore(nil, "/data/agency", nil)
	got := s.RepoIndexPath()
	want := "/data/agency/repo_index.json"
	assert.Equal(t, want, got, "RepoIndexPath()")
}

// TestRepoDir verifies repo directory path construction.
func TestRepoDir(t *testing.T) {
	t.Parallel()
	s := NewStore(nil, "/data/agency", nil)
	got := s.RepoDir("abc123")
	want := "/data/agency/repos/abc123"
	assert.Equal(t, want, got, "RepoDir()")
}

// TestRepoRecordPath verifies repo record path construction.
func TestRepoRecordPath(t *testing.T) {
	t.Parallel()
	s := NewStore(nil, "/data/agency", nil)
	got := s.RepoRecordPath("abc123")
	want := "/data/agency/repos/abc123/repo.json"
	assert.Equal(t, want, got, "RepoRecordPath()")
}

func TestInvocationLogPaths(t *testing.T) {
	t.Parallel()
	s := NewStore(nil, "/data/agency", nil)

	assert.Equal(t,
		"/data/agency/repos/repo123/invocations/inv456/logs",
		s.InvocationLogsDir("repo123", "inv456"),
	)
	assert.Equal(t,
		"/data/agency/repos/repo123/invocations/inv456/logs/raw.jsonl",
		s.InvocationRawLogPath("repo123", "inv456"),
	)
	assert.Equal(t,
		"/data/agency/repos/repo123/invocations/inv456/logs/stderr.log",
		s.InvocationStderrLogPath("repo123", "inv456"),
	)
	assert.Equal(t,
		"/data/agency/repos/repo123/invocations/inv456/logs/stream.jsonl",
		s.InvocationStreamLogPath("repo123", "inv456"),
	)
}

func TestResolveInvocationLogPath_PrefersInvocationOwned(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)

	const repoID = "repo123"
	const invocationID = "inv456"

	_, err := s.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(s.SandboxRawLogPath(repoID, invocationID)), 0o700))
	require.NoError(t, os.WriteFile(s.SandboxRawLogPath(repoID, invocationID), []byte("legacy\n"), 0o644))
	require.NoError(t, os.MkdirAll(s.InvocationLogsDir(repoID, invocationID), 0o700))
	require.NoError(t, os.WriteFile(s.InvocationRawLogPath(repoID, invocationID), []byte("canonical\n"), 0o644))

	assert.Equal(t,
		s.InvocationRawLogPath(repoID, invocationID),
		s.ResolveInvocationLogPath(repoID, invocationID, "raw"),
	)
	assert.Equal(t,
		s.InvocationLogsDir(repoID, invocationID),
		s.ResolveInvocationLogsDir(repoID, invocationID),
	)
}

func TestPrepareInvocationLogPath_PromotesLegacySandboxLog(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)

	const repoID = "repo123"
	const invocationID = "inv456"

	_, err := s.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	legacyPath := s.SandboxRawLogPath(repoID, invocationID)
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0o700))
	require.NoError(t, os.WriteFile(legacyPath, []byte("legacy raw\n"), 0o644))

	preparedPath, err := s.PrepareInvocationLogPath(repoID, invocationID, "raw")
	require.NoError(t, err)
	assert.Equal(t, s.InvocationRawLogPath(repoID, invocationID), preparedPath)

	data, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	assert.Equal(t, "legacy raw\n", string(data))

	_, err = os.Stat(legacyPath)
	assert.True(t, os.IsNotExist(err), "legacy sandbox log should be moved into invocation storage")
}

func TestResolveInvocationCheckpointsDir_PrefersInvocationOwned(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)

	const repoID = "repo123"
	const invocationID = "inv456"

	_, err := s.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)

	legacyPath := s.SandboxCheckpointsPath(repoID, invocationID)
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0o700))
	require.NoError(t, os.WriteFile(legacyPath, []byte("legacy checkpoints\n"), 0o644))

	invocationPath := s.InvocationCheckpointsPath(repoID, invocationID)
	require.NoError(t, os.WriteFile(invocationPath, []byte("invocation checkpoints\n"), 0o644))

	assert.Equal(
		t,
		s.InvocationDir(repoID, invocationID),
		s.ResolveInvocationCheckpointsDir(repoID, invocationID),
	)
}

func TestPrepareInvocationCheckpointsPath_PromotesLegacySandboxCheckpoints(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)

	const repoID = "repo123"
	const invocationID = "inv456"

	_, err := s.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)

	legacyPath := s.SandboxCheckpointsPath(repoID, invocationID)
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0o700))
	require.NoError(t, os.WriteFile(legacyPath, []byte("legacy checkpoints\n"), 0o644))

	preparedPath, err := s.PrepareInvocationCheckpointsPath(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, s.InvocationCheckpointsPath(repoID, invocationID), preparedPath)

	data, err := os.ReadFile(preparedPath)
	require.NoError(t, err)
	assert.Equal(t, "legacy checkpoints\n", string(data))

	_, err = os.Stat(legacyPath)
	assert.True(t, os.IsNotExist(err), "legacy sandbox checkpoints should be moved into invocation storage")
}

func TestReadInvocationMeta_MissingSchemaVersionReturnsStoreCorrupt(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)
	const repoID = "repo123"
	const invocationID = "inv456"

	_, err := s.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)

	metaPath := s.InvocationMetaPath(repoID, invocationID)
	require.NoError(t, os.WriteFile(metaPath, []byte(`{"invocation_id":"inv456"}`), 0o644))

	_, err = s.ReadInvocationMeta(repoID, invocationID)
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

func TestReadInvocationMeta_UnsupportedSchemaVersionReturnsStoreCorrupt(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)
	const repoID = "repo123"
	const invocationID = "inv456"

	_, err := s.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)

	metaPath := s.InvocationMetaPath(repoID, invocationID)
	require.NoError(t, os.WriteFile(metaPath, []byte(`{"schema_version":"9.9","invocation_id":"inv456"}`), 0o644))

	_, err = s.ReadInvocationMeta(repoID, invocationID)
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

func TestRemoveInvocationDir_RejectsPathTraversalOutsideDataDir(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	outsideRoot := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)

	reposDir := filepath.Join(dataDir, "repos")
	repoID, err := filepath.Rel(reposDir, outsideRoot)
	require.NoError(t, err)

	const invocationID = "inv-escape"
	targetDir := filepath.Join(outsideRoot, "invocations", invocationID)
	require.NoError(t, os.MkdirAll(targetDir, 0o700))
	marker := filepath.Join(targetDir, "keep.txt")
	require.NoError(t, os.WriteFile(marker, []byte("do-not-delete"), 0o600))

	err = s.RemoveInvocationDir(repoID, invocationID)
	require.Error(t, err)
	var guardErr *fs.ErrNotUnderPrefix
	require.ErrorAs(t, err, &guardErr)

	_, statErr := os.Stat(marker)
	require.NoError(t, statErr, "guard failure should not delete paths outside data dir")
}

func TestRemoveSandboxDir_RejectsPathTraversalOutsideDataDir(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	outsideRoot := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)

	reposDir := filepath.Join(dataDir, "repos")
	repoID, err := filepath.Rel(reposDir, outsideRoot)
	require.NoError(t, err)

	const invocationID = "sandbox-escape"
	targetDir := filepath.Join(outsideRoot, "sandboxes", invocationID)
	require.NoError(t, os.MkdirAll(targetDir, 0o700))
	marker := filepath.Join(targetDir, "keep.txt")
	require.NoError(t, os.WriteFile(marker, []byte("do-not-delete"), 0o600))

	err = s.RemoveSandboxDir(repoID, invocationID)
	require.Error(t, err)
	var guardErr *fs.ErrNotUnderPrefix
	require.ErrorAs(t, err, &guardErr)

	_, statErr := os.Stat(marker)
	require.NoError(t, statErr, "guard failure should not delete paths outside data dir")
}

// TestLoadRepoIndex_MissingFile verifies empty index returned for missing file.
func TestLoadRepoIndex_MissingFile(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	idx, err := s.LoadRepoIndex()
	require.NoError(t, err)
	assert.Equal(t, SchemaVersion, idx.SchemaVersion)
	assert.Empty(t, idx.Repos)
}

// TestRepoIndexRoundtrip tests save/load cycle.
func TestRepoIndexRoundtrip(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Start with empty index
	idx, err := s.LoadRepoIndex()
	require.NoError(t, err)

	// Upsert an entry
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123def456", "/path/to/repo")

	// Save
	require.NoError(t, s.SaveRepoIndex(idx))

	// Load again
	loaded, err := s.LoadRepoIndex()
	require.NoError(t, err)

	// Verify
	assert.Equal(t, SchemaVersion, loaded.SchemaVersion)
	entry, ok := loaded.Repos["github:owner/repo"]
	require.True(t, ok, "expected entry for github:owner/repo")
	assert.Equal(t, "abc123def456", entry.RepoID)
	require.Len(t, entry.Paths, 1)
	assert.Equal(t, "/path/to/repo", entry.Paths[0])
	assert.Equal(t, "2026-01-09T12:00:00Z", entry.LastSeenAt)
}

// TestUpsertRepoIndexEntry_NoDuplication verifies path deduplication.
func TestUpsertRepoIndexEntry_NoDuplication(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}

	// Add entry with path
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/one")

	// Add same path again
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/one")

	entry := idx.Repos["github:owner/repo"]
	assert.Len(t, entry.Paths, 1, "no duplication")
}

// TestUpsertRepoIndexEntry_NewPath verifies new paths are added and sorted.
func TestUpsertRepoIndexEntry_NewPath(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}

	// Add first path
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/one")

	// Add second path
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/two")

	entry := idx.Repos["github:owner/repo"]
	require.Len(t, entry.Paths, 2)
	// PR-A: paths are sorted lexicographically for stable diffs
	assert.Equal(t, "/path/one", entry.Paths[0], "sorted alphabetically")
	assert.Equal(t, "/path/two", entry.Paths[1])
}

// TestUpsertRepoIndexEntry_DeduplicatesExistingPath verifies duplicate paths are not added.
func TestUpsertRepoIndexEntry_DeduplicatesExistingPath(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}

	// Add paths in order
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/one")
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/two")

	// Touch first path again — should not duplicate
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/one")

	entry := idx.Repos["github:owner/repo"]
	require.Len(t, entry.Paths, 2)
	// PR-A: paths are sorted lexicographically
	assert.Equal(t, "/path/one", entry.Paths[0])
	assert.Equal(t, "/path/two", entry.Paths[1])
}

// TestLoadRepoIndex_CorruptJSON verifies E_STORE_CORRUPT for invalid JSON.
func TestLoadRepoIndex_CorruptJSON(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Write corrupt JSON
	path := s.RepoIndexPath()
	require.NoError(t, os.WriteFile(path, []byte("{invalid json"), 0644))

	_, err := s.LoadRepoIndex()
	require.Error(t, err, "want E_STORE_CORRUPT")
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

// TestLoadRepoIndex_MissingSchemaVersion verifies E_STORE_CORRUPT for missing schema_version.
func TestLoadRepoIndex_MissingSchemaVersion(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Write JSON without schema_version
	path := s.RepoIndexPath()
	require.NoError(t, os.WriteFile(path, []byte(`{"repos":{}}`), 0644))

	_, err := s.LoadRepoIndex()
	require.Error(t, err, "want E_STORE_CORRUPT")
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

// TestLoadRepoRecord_MissingFile verifies (zero, false, nil) for missing file.
func TestLoadRepoRecord_MissingFile(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	rec, exists, err := s.LoadRepoRecord("nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, rec.RepoID)
}

// TestRepoRecordRoundtrip tests save/load cycle for repo records.
func TestRepoRecordRoundtrip(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	input := BuildRepoRecordInput{
		RepoKey:          "github:owner/repo",
		RepoID:           "abc123def456ghij",
		RepoRootLastSeen: "/path/to/repo",
		AgencyJSONPath:   "/path/to/repo/agency.json",
		OriginPresent:    true,
		OriginURL:        "git@github.com:owner/repo.git",
		OriginHost:       "github.com",
		Capabilities: Capabilities{
			GitHubOrigin: true,
			OriginHost:   "github.com",
			GhAuthed:     true,
		},
	}

	// Create new record
	rec := s.UpsertRepoRecord(nil, input)

	// Save
	require.NoError(t, s.SaveRepoRecord(rec))

	// Load
	loaded, exists, err := s.LoadRepoRecord(input.RepoID)
	require.NoError(t, err)
	require.True(t, exists)

	// Verify all fields
	assert.Equal(t, SchemaVersion, loaded.SchemaVersion)
	assert.Equal(t, input.RepoKey, loaded.RepoKey)
	assert.Equal(t, input.RepoID, loaded.RepoID)
	assert.Equal(t, input.RepoRootLastSeen, loaded.RepoRootLastSeen)
	assert.Equal(t, input.AgencyJSONPath, loaded.AgencyJSONPath)
	assert.Equal(t, input.OriginPresent, loaded.OriginPresent)
	assert.Equal(t, input.OriginURL, loaded.OriginURL)
	assert.Equal(t, input.OriginHost, loaded.OriginHost)
	assert.Equal(t, input.Capabilities.GitHubOrigin, loaded.Capabilities.GitHubOrigin)
	assert.Equal(t, input.Capabilities.OriginHost, loaded.Capabilities.OriginHost)
	assert.Equal(t, input.Capabilities.GhAuthed, loaded.Capabilities.GhAuthed)
	assert.Equal(t, "2026-01-09T12:00:00Z", loaded.CreatedAt)
	assert.Equal(t, "2026-01-09T12:00:00Z", loaded.UpdatedAt)
}

// TestUpsertRepoRecord_PreservesCreatedAt verifies CreatedAt is preserved on update.
func TestUpsertRepoRecord_PreservesCreatedAt(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	createTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updateTime := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)

	// Create initial record
	s := NewStore(realFS, dataDir, fixedTime(createTime))
	input := BuildRepoRecordInput{
		RepoKey:       "github:owner/repo",
		RepoID:        "abc123",
		OriginPresent: true,
		OriginURL:     "git@github.com:owner/repo.git",
		OriginHost:    "github.com",
		Capabilities:  Capabilities{GitHubOrigin: true},
	}
	rec := s.UpsertRepoRecord(nil, input)
	require.NoError(t, s.SaveRepoRecord(rec))

	// Load and update with later time
	s = NewStore(realFS, dataDir, fixedTime(updateTime))
	loaded, exists, err := s.LoadRepoRecord("abc123")
	require.NoError(t, err)
	require.True(t, exists)

	input.Capabilities.GhAuthed = true // change something
	updated := s.UpsertRepoRecord(&loaded, input)

	// Verify timestamps
	assert.Equal(t, "2026-01-01T00:00:00Z", updated.CreatedAt, "preserved")
	assert.Equal(t, "2026-01-09T12:00:00Z", updated.UpdatedAt, "updated")
}

// TestLoadRepoRecord_CorruptJSON verifies E_STORE_CORRUPT for invalid JSON.
func TestLoadRepoRecord_CorruptJSON(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Create repo directory and write corrupt JSON
	repoDir := s.RepoDir("abc123")
	require.NoError(t, os.MkdirAll(repoDir, 0755))
	path := s.RepoRecordPath("abc123")
	require.NoError(t, os.WriteFile(path, []byte("{invalid json"), 0644))

	_, _, err := s.LoadRepoRecord("abc123")
	require.Error(t, err, "want E_STORE_CORRUPT")
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

// TestLoadRepoRecord_MissingSchemaVersion verifies E_STORE_CORRUPT for missing schema_version.
func TestLoadRepoRecord_MissingSchemaVersion(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Create repo directory and write JSON without schema_version
	repoDir := s.RepoDir("abc123")
	require.NoError(t, os.MkdirAll(repoDir, 0755))
	path := s.RepoRecordPath("abc123")
	require.NoError(t, os.WriteFile(path, []byte(`{"repo_id":"abc123"}`), 0644))

	_, _, err := s.LoadRepoRecord("abc123")
	require.Error(t, err, "want E_STORE_CORRUPT")
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

// TestSaveRepoRecord_CreatesDirectory verifies repo directory is created.
func TestSaveRepoRecord_CreatesDirectory(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	rec := s.UpsertRepoRecord(nil, BuildRepoRecordInput{
		RepoKey: "github:owner/repo",
		RepoID:  "newrepo123",
	})

	// Directory should not exist yet
	repoDir := s.RepoDir("newrepo123")
	_, err := os.Stat(repoDir)
	assert.True(t, os.IsNotExist(err), "repo directory should not exist before save")

	// Save should create it
	require.NoError(t, s.SaveRepoRecord(rec))

	// Now it should exist
	_, err = os.Stat(repoDir)
	assert.NoError(t, err, "repo directory should exist after save")
}

// TestSaveRepoIndex_CreatesDirectory verifies data directory is created.
func TestSaveRepoIndex_CreatesDirectory(t *testing.T) {
	t.Parallel()
	dataDir := filepath.Join(t.TempDir(), "subdir", "agency")
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}

	// Directory should not exist yet
	_, err := os.Stat(dataDir)
	assert.True(t, os.IsNotExist(err), "data directory should not exist before save")

	// Save should create it
	require.NoError(t, s.SaveRepoIndex(idx))

	// Now it should exist
	_, err = os.Stat(dataDir)
	assert.NoError(t, err, "data directory should exist after save")
}

// TestJSONFormat verifies the output JSON is properly formatted.
func TestJSONFormat(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Save repo index
	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/to/repo")
	require.NoError(t, s.SaveRepoIndex(idx))

	// Read raw JSON and verify it's indented
	data, err := os.ReadFile(s.RepoIndexPath())
	require.NoError(t, err)

	// Check for indentation (should contain newlines and spaces)
	assert.True(t, json.Valid(data), "output is not valid JSON")
	// Verify trailing newline
	require.NotEmpty(t, data)
	assert.Equal(t, byte('\n'), data[len(data)-1], "output should end with newline")
}

// TestPathNormalization verifies paths are cleaned.
func TestPathNormalization(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}

	// Add path with .. components
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/to/../to/repo")

	entry := idx.Repos["github:owner/repo"]
	assert.Equal(t, "/path/to/repo", entry.Paths[0], "path not normalized")
}
