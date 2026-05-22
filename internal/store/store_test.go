package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

func TestUpdateInvocationMetaConcurrentPreservesIndependentUpdates(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)
	const repoID = "repo123"
	const invocationID = "inv-concurrent"

	_, err := s.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	meta := NewInvocationMeta(invocationID, "", "wt-1", "/sandbox", "/checkouts/repo123", "work", "agency/sandbox-"+invocationID, "abc123", "claude-code", RunnerModeHeadless, time.Now())
	require.NoError(t, s.WriteInvocationMeta(repoID, invocationID, meta))

	const updates = 50
	start := make(chan struct{})
	errs := make(chan error, updates)
	var wg sync.WaitGroup
	for i := 0; i < updates; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- s.UpdateInvocationMeta(repoID, invocationID, func(m *InvocationMeta) {
				time.Sleep(time.Millisecond)
				m.RunnerArgs = append(m.RunnerArgs, fmt.Sprintf("arg-%02d", i))
			})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	updated, err := s.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	require.Len(t, updated.RunnerArgs, updates)
	seen := make(map[string]bool, updates)
	for _, arg := range updated.RunnerArgs {
		seen[arg] = true
	}
	for i := 0; i < updates; i++ {
		assert.True(t, seen[fmt.Sprintf("arg-%02d", i)])
	}
}

func TestUpdateInvocationMetaLockCanonicalizesSymlinkedDataDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	linkDir := filepath.Join(root, "link")
	require.NoError(t, os.MkdirAll(dataDir, 0o700))
	require.NoError(t, os.Symlink(dataDir, linkDir))

	const repoID = "repo123"
	const invocationID = "inv-symlink-lock"
	s1 := NewStore(fs.NewRealFS(), dataDir, nil)
	s2 := NewStore(fs.NewRealFS(), linkDir, nil)
	_, err := s1.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	meta := NewInvocationMeta(invocationID, "", "wt-1", "/sandbox", "/checkouts/repo123", "work", "agency/sandbox-"+invocationID, "abc123", "claude-code", RunnerModeHeadless, time.Now())
	require.NoError(t, s1.WriteInvocationMeta(repoID, invocationID, meta))

	const updates = 40
	start := make(chan struct{})
	errs := make(chan error, updates)
	var wg sync.WaitGroup
	for i := 0; i < updates; i++ {
		i := i
		st := s1
		if i%2 == 1 {
			st = s2
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- st.UpdateInvocationMeta(repoID, invocationID, func(m *InvocationMeta) {
				time.Sleep(time.Millisecond)
				m.RunnerArgs = append(m.RunnerArgs, fmt.Sprintf("arg-%02d", i))
			})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	updated, err := s1.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	require.Len(t, updated.RunnerArgs, updates)
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

func TestReadInvocationMetaRejectsStrictViolations(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)
	const repoID = "repo123"

	cases := []struct {
		name         string
		invocationID string
		mutate       func(*InvocationMeta)
	}{
		{
			name:         "wrong invocation id",
			invocationID: "inv-wrong",
			mutate: func(meta *InvocationMeta) {
				meta.InvocationID = "other"
			},
		},
		{
			name:         "relative sandbox path",
			invocationID: "inv-relative",
			mutate: func(meta *InvocationMeta) {
				meta.SandboxPath = "sandbox"
			},
		},
		{
			name:         "missing required field",
			invocationID: "inv-missing",
			mutate: func(meta *InvocationMeta) {
				meta.IntegrationWorktreeID = ""
			},
		},
		{
			name:         "invalid status",
			invocationID: "inv-status",
			mutate: func(meta *InvocationMeta) {
				meta.Status = "paused"
			},
		},
		{
			name:         "invalid mode",
			invocationID: "inv-mode",
			mutate: func(meta *InvocationMeta) {
				meta.Mode = "batch"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.EnsureInvocationDir(repoID, tc.invocationID)
			require.NoError(t, err)
			meta := NewInvocationMeta(tc.invocationID, "", "wt-1", "/sandbox", "/checkouts/repo123", "work", "agency/sandbox-"+tc.invocationID, "abc123", "claude-code", RunnerModeHeadless, time.Now())
			tc.mutate(meta)
			data, err := json.MarshalIndent(meta, "", "  ")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(s.InvocationMetaPath(repoID, tc.invocationID), data, 0o600))

			_, err = s.ReadInvocationMeta(repoID, tc.invocationID)
			require.Error(t, err)
			assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
		})
	}
}

func TestScanInvocationsForRepoSortsNewestFirstAndMarksInvalidMetaBroken(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)
	const repoID = "repo123"

	for _, entry := range []struct {
		id        string
		startedAt time.Time
		mutate    func(*InvocationMeta)
	}{
		{
			id:        "inv-old",
			startedAt: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			id:        "inv-new",
			startedAt: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC),
		},
		{
			id:        "inv-invalid-meta",
			startedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			mutate: func(meta *InvocationMeta) {
				meta.Status = "paused"
			},
		},
	} {
		_, err := s.EnsureInvocationDir(repoID, entry.id)
		require.NoError(t, err)
		meta := NewInvocationMeta(entry.id, "", "wt-1", "/sandbox", "/checkouts/repo123", "work", "agency/sandbox-"+entry.id, "abc123", "claude-code", RunnerModeHeadless, entry.startedAt)
		if entry.mutate != nil {
			entry.mutate(meta)
		}
		require.NoError(t, s.WriteInvocationMeta(repoID, entry.id, meta))
	}

	records, err := s.ScanInvocationsForRepo(repoID)
	require.NoError(t, err)
	require.Len(t, records, 3)
	assert.Equal(t, "inv-new", records[0].InvocationID)
	assert.Equal(t, "inv-old", records[1].InvocationID)
	assert.Equal(t, "inv-invalid-meta", records[2].InvocationID)
	assert.True(t, records[2].Broken)
	assert.Nil(t, records[2].Meta)
}

func TestReadAndScanInvocationMetaRejectUnknownFields(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)
	const repoID = "repo123"
	const invocationID = "inv-unknown"

	_, err := s.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(s.InvocationMetaPath(repoID, invocationID), []byte(`{
		"schema_version":"2.0",
		"invocation_id":"inv-unknown",
		"integration_worktree_id":"wt-1",
		"sandbox_path":"/sandbox",
		"checkout_root":"/checkouts/repo123",
		"execution_profile":"work",
		"sandbox_branch":"agency/sandbox-inv-unknown",
		"base_commit":"abc123",
		"runner":"claude-code",
		"mode":"headless",
		"started_at":"2026-01-01T00:00:00Z",
		"status":"starting",
		"checkpoint_include_untracked":true,
		"unexpected":true
	}`), 0o644))

	_, err = s.ReadInvocationMeta(repoID, invocationID)
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))

	records, err := s.ScanInvocationsForRepo(repoID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.True(t, records[0].Broken)
	assert.Nil(t, records[0].Meta)
}

func TestReadAndScanInvocationMetaRequireCheckpointIncludeUntracked(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)
	const repoID = "repo123"
	const invocationID = "inv-no-checkpoint-flag"

	_, err := s.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(s.InvocationMetaPath(repoID, invocationID), []byte(`{
		"schema_version":"2.0",
		"invocation_id":"inv-no-checkpoint-flag",
		"integration_worktree_id":"wt-1",
		"sandbox_path":"/sandbox",
		"checkout_root":"/checkouts/repo123",
		"execution_profile":"work",
		"sandbox_branch":"agency/sandbox-inv-no-checkpoint-flag",
		"base_commit":"abc123",
		"runner":"claude-code",
		"mode":"headless",
		"started_at":"2026-01-01T00:00:00Z",
		"status":"starting"
	}`), 0o644))

	_, err = s.ReadInvocationMeta(repoID, invocationID)
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))

	records, err := s.ScanInvocationsForRepo(repoID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.True(t, records[0].Broken)
	assert.Nil(t, records[0].Meta)
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

	require.FileExists(t, marker, "guard failure should not delete paths outside data dir")
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
	// Paths are sorted lexicographically for stable diffs.
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
	// Paths are sorted lexicographically for stable diffs.
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

func TestLoadRepoIndexRejectsUnknownFieldsAndInvalidPaths(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)

	cases := []struct {
		name string
		body string
	}{
		{
			name: "unknown top-level field",
			body: `{"schema_version":"2.0","repos":{},"unexpected":true}`,
		},
		{
			name: "missing repos",
			body: `{"schema_version":"2.0"}`,
		},
		{
			name: "relative path",
			body: `{"schema_version":"2.0","repos":{"github:owner/repo":{"repo_id":"abc123","paths":["relative"],"last_seen_at":"2026-01-01T00:00:00Z"}}}`,
		},
		{
			name: "missing entry field",
			body: `{"schema_version":"2.0","repos":{"github:owner/repo":{"repo_id":"abc123","paths":["/repo"]}}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(s.RepoIndexPath(), []byte(tc.body), 0o644))
			_, err := s.LoadRepoIndex()
			require.Error(t, err)
			assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
		})
	}
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
		RepoKey:          "github:owner/repo",
		RepoID:           "abc123",
		RepoRootLastSeen: "/path/to/repo",
		PreferredRoot:    "/path/to/repo",
		AgencyJSONPath:   "/path/to/repo/agency.json",
		OriginPresent:    true,
		OriginURL:        "git@github.com:owner/repo.git",
		OriginHost:       "github.com",
		Capabilities:     Capabilities{GitHubOrigin: true},
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

func TestLoadRepoRecordRejectsUnknownFieldsWrongIDAndRelativePaths(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	s := NewStore(fs.NewRealFS(), dataDir, nil)
	repoDir := s.RepoDir("abc123")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))

	cases := []struct {
		name string
		body string
	}{
		{
			name: "unknown field",
			body: `{"schema_version":"2.0","repo_key":"github:owner/repo","repo_id":"abc123","repo_root_last_seen":"/repo","preferred_root":"/repo","agency_json_path":"/repo/agency.json","origin_present":false,"origin_url":"","origin_host":"","capabilities":{"github_origin":false,"origin_host":"","gh_authed":false},"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","unexpected":true}`,
		},
		{
			name: "wrong repo id",
			body: `{"schema_version":"2.0","repo_key":"github:owner/repo","repo_id":"other","repo_root_last_seen":"/repo","preferred_root":"/repo","agency_json_path":"/repo/agency.json","origin_present":false,"origin_url":"","origin_host":"","capabilities":{"github_origin":false,"origin_host":"","gh_authed":false},"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`,
		},
		{
			name: "relative path",
			body: `{"schema_version":"2.0","repo_key":"github:owner/repo","repo_id":"abc123","repo_root_last_seen":"repo","preferred_root":"/repo","agency_json_path":"/repo/agency.json","origin_present":false,"origin_url":"","origin_host":"","capabilities":{"github_origin":false,"origin_host":"","gh_authed":false},"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(s.RepoRecordPath("abc123"), []byte(tc.body), 0o644))
			_, _, err := s.LoadRepoRecord("abc123")
			require.Error(t, err)
			assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
		})
	}
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
	assert.NoDirExists(t, repoDir, "repo directory should not exist before save")

	// Save should create it
	require.NoError(t, s.SaveRepoRecord(rec))

	// Now it should exist
	assert.DirExists(t, repoDir, "repo directory should exist after save")
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
	assert.NoDirExists(t, dataDir, "data directory should not exist before save")

	// Save should create it
	require.NoError(t, s.SaveRepoIndex(idx))

	// Now it should exist
	assert.DirExists(t, dataDir, "data directory should exist after save")
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
