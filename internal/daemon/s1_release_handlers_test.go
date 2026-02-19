package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func newS1TestServer(t *testing.T) *Server {
	t.Helper()
	tmpDir := t.TempDir()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, tmpDir, time.Now)
	return NewServer(st, exec.NewRealRunner(), fsys, tmpDir)
}

func TestS1ReleaseEndpoints_RequireRepoID(t *testing.T) {
	t.Parallel()

	srv := newS1TestServer(t)

	endpoints := []string{
		"/spec/v2.1/s1/release/readiness",
		"/spec/v2.1/s1/release/closure-report",
		"/spec/v2.1/s1/release/freeze-readiness",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			w := httptest.NewRecorder()

			srv.handleS1Release(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)

			var resp APIResponse
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.False(t, resp.OK)
			assert.Equal(t, "E_INVALID_REQUEST", resp.ErrorCode)
		})
	}
}

func TestS1ReleaseEndpoints_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	srv := newS1TestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/spec/v2.1/s1/release/readiness", nil)
	w := httptest.NewRecorder()

	srv.handleS1Release(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestS1ReleaseEndpoints_NotFound(t *testing.T) {
	t.Parallel()

	srv := newS1TestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/spec/v2.1/s1/release/nonexistent", nil)
	w := httptest.NewRecorder()

	srv.handleS1Release(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestS1ReleaseEndpoints_RepoNotFound(t *testing.T) {
	t.Parallel()

	srv := newS1TestServer(t)

	endpoints := []string{
		"/spec/v2.1/s1/release/readiness?repo_id=nonexistent",
		"/spec/v2.1/s1/release/closure-report?repo_id=nonexistent",
		"/spec/v2.1/s1/release/freeze-readiness?repo_id=nonexistent",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			w := httptest.NewRecorder()

			srv.handleS1Release(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)

			var resp APIResponse
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.False(t, resp.OK)
			assert.Contains(t, resp.ErrorCode, "E_REPO_NOT_FOUND")
		})
	}
}

func TestS1FreezeReadinessEndpoint_BlockedContract(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, tmpDir, time.Now)
	srv := NewServer(st, exec.NewRealRunner(), fsys, tmpDir)

	repoID := "test-repo-freeze"
	repoRoot := filepath.Join(tmpDir, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, "docs", "v2.1", "s1"), 0o755))

	specContent := `# S1 Spec

## 9. Unresolved Questions + Temporary Defaults (must be empty before freeze)

| question | temporary default behavior | owner | due |
|---|---|---|---|
| Should we block on failures? | Yes | @owner | 2026-03-01 |
`
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "docs", "v2.1", "s1", "s1_spec.md"), []byte(specContent), 0o644))

	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			repoID: {RepoID: repoID, Paths: []string{repoRoot}, LastSeenAt: "2026-02-05T12:00:00Z"},
		},
	}))
	require.NoError(t, st.SaveRepoRecord(store.RepoRecord{
		SchemaVersion: "1.0",
		RepoID:        repoID,
		RepoKey:       "test-key",
		PreferredRoot: repoRoot,
	}))

	req := httptest.NewRequest(http.MethodGet, "/spec/v2.1/s1/release/freeze-readiness?repo_id="+repoID, nil)
	w := httptest.NewRecorder()

	srv.handleS1Release(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)

	var resp APIResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.False(t, resp.OK)
	assert.Equal(t, "E_GATE_BLOCKED", resp.ErrorCode)
}
