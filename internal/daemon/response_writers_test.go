package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func TestWriteControlPlaneSuccess_IncludesNormalizedRepoAndWorktreeNames(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	s := NewServer(st, nil, fs.NewRealFS(), configDir)

	repoID := "repo-1"
	worktreeID := "wt-1"
	require.NoError(t, st.SaveRepoRecord(store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoID:           repoID,
		RepoKey:          "github:owner/agency",
		PreferredRoot:    "/tmp/agency",
		RepoRootLastSeen: "/tmp/agency",
		CreatedAt:        "2026-04-20T10:00:00Z",
		UpdatedAt:        "2026-04-20T10:00:00Z",
	}))
	_, err := st.EnsureIntegrationWorktreeDir(repoID, worktreeID)
	require.NoError(t, err)
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, worktreeID, &store.IntegrationWorktreeMeta{
		SchemaVersion:    store.SchemaVersion,
		WorktreeID:       worktreeID,
		Name:             "command-button",
		RepoID:           repoID,
		Branch:           "agency/command-button",
		BaseBranch:       "main",
		TreePath:         "/tmp/agency-worktree",
		CheckoutRoot:     "/tmp/checkouts/repo-1",
		ExecutionProfile: "work",
		CreatedAt:        "2026-04-20T10:00:00Z",
		State:            store.WorktreeStatePresent,
	}))

	w := httptest.NewRecorder()
	s.writeControlPlaneSuccess(w, "inv-1", &store.InvocationMeta{
		InvocationID:          "inv-1",
		IntegrationWorktreeID: worktreeID,
		SandboxPath:           "/tmp/sandbox/inv-1",
	}, repoID, "client-1", "request-1", false)

	require.Equal(t, http.StatusOK, w.Code)

	var resp ControlPlaneStartResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.OK)
	assert.Equal(t, repoID, resp.RepoID)
	assert.Equal(t, "agency", resp.RepoName)
	assert.Equal(t, worktreeID, resp.WorktreeID)
	assert.Equal(t, "command-button", resp.WorktreeName)
}

func TestWriteHeadedSuccess_IncludesNormalizedRepoAndWorktreeNames(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	s := NewServer(st, nil, fs.NewRealFS(), configDir)

	repoID := "repo-1"
	worktreeID := "wt-1"
	require.NoError(t, st.SaveRepoRecord(store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoID:           repoID,
		RepoKey:          "github:owner/agency",
		PreferredRoot:    "/tmp/agency",
		RepoRootLastSeen: "/tmp/agency",
		CreatedAt:        "2026-04-20T10:00:00Z",
		UpdatedAt:        "2026-04-20T10:00:00Z",
	}))
	_, err := st.EnsureIntegrationWorktreeDir(repoID, worktreeID)
	require.NoError(t, err)
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, worktreeID, &store.IntegrationWorktreeMeta{
		SchemaVersion:    store.SchemaVersion,
		WorktreeID:       worktreeID,
		Name:             "command-button",
		RepoID:           repoID,
		Branch:           "agency/command-button",
		BaseBranch:       "main",
		TreePath:         "/tmp/agency-worktree",
		CheckoutRoot:     "/tmp/checkouts/repo-1",
		ExecutionProfile: "work",
		CreatedAt:        "2026-04-20T10:00:00Z",
		State:            store.WorktreeStatePresent,
	}))

	w := httptest.NewRecorder()
	s.writeHeadedSuccess(w, "inv-1", &store.InvocationMeta{
		InvocationID:          "inv-1",
		IntegrationWorktreeID: worktreeID,
		SandboxPath:           "/tmp/sandbox/inv-1",
		TmuxSession:           "agency_inv-1",
	}, repoID, "client-1", "request-1", false)

	require.Equal(t, http.StatusOK, w.Code)

	var resp ControlPlaneStartHeadedResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.OK)
	assert.Equal(t, repoID, resp.RepoID)
	assert.Equal(t, "agency", resp.RepoName)
	assert.Equal(t, worktreeID, resp.WorktreeID)
	assert.Equal(t, "command-button", resp.WorktreeName)
}
