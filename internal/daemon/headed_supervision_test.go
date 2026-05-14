package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func TestResolveHeadedSupervisionRepoRootPrefersRepoRecordPreferredRoot(t *testing.T) {
	t.Parallel()

	srv, st := newHeadedSupervisionRootTestServer(t)
	repoID := "repo-headed-root"
	preferredRoot := t.TempDir()
	lastSeenRoot := t.TempDir()
	indexRoot := t.TempDir()

	require.NoError(t, st.SaveRepoRecord(store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          "path:" + repoID,
		RepoID:           repoID,
		RepoRootLastSeen: lastSeenRoot,
		PreferredRoot:    preferredRoot,
		AgencyJSONPath:   filepath.Join(preferredRoot, "agency.json"),
		CreatedAt:        "2026-02-05T12:00:00Z",
		UpdatedAt:        "2026-02-05T12:00:00Z",
	}))
	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			"path:" + repoID: {
				RepoID:     repoID,
				Paths:      []string{indexRoot},
				LastSeenAt: "2026-02-05T12:00:00Z",
			},
		},
	}))

	got, err := srv.resolveHeadedSupervisionRepoRoot(repoID)
	require.NoError(t, err)
	assert.Equal(t, canonicalHeadedRootTestPath(t, preferredRoot), got)
}

func TestResolveHeadedSupervisionRepoRootFallsBackToRepoRecordLastSeen(t *testing.T) {
	t.Parallel()

	srv, st := newHeadedSupervisionRootTestServer(t)
	repoID := "repo-headed-last-seen"
	lastSeenRoot := t.TempDir()
	indexRoot := t.TempDir()

	require.NoError(t, st.SaveRepoRecord(store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          "path:" + repoID,
		RepoID:           repoID,
		RepoRootLastSeen: lastSeenRoot,
		AgencyJSONPath:   filepath.Join(lastSeenRoot, "agency.json"),
		CreatedAt:        "2026-02-05T12:00:00Z",
		UpdatedAt:        "2026-02-05T12:00:00Z",
	}))
	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			"path:" + repoID: {
				RepoID:     repoID,
				Paths:      []string{indexRoot},
				LastSeenAt: "2026-02-05T12:00:00Z",
			},
		},
	}))

	got, err := srv.resolveHeadedSupervisionRepoRoot(repoID)
	require.NoError(t, err)
	assert.Equal(t, canonicalHeadedRootTestPath(t, lastSeenRoot), got)
}

func newHeadedSupervisionRootTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	return NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir), st
}

func canonicalHeadedRootTestPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return resolved
}
