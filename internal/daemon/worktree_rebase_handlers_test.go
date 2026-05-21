package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestHandleWorktreeRebase_MissingRepoIDReturnsInvalidRequest(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	w := doWorktreeRequestWithBody(t, env, http.MethodPost, "/worktrees/wt-1/rebase", []byte(`{}`))
	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp WorktreeRebaseResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EInvalidRequest), resp.ErrorCode)
	assert.Equal(t, "repo_id query parameter is required", resp.Message)
}

func TestHandleWorktreeRebase_DirtyWorktreeRejected(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_ = setupWorktreeMutationReadyState(t, env)
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
		Stdout: " M README.md\n",
	}

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/wt-1/rebase?repo_id="+env.RepoID,
		[]byte(`{}`),
	)
	require.Equal(t, http.StatusConflict, w.Code)

	var resp WorktreeRebaseResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EDirtyWorktree), resp.ErrorCode)
}

func TestHandleWorktreeRebase_RebaseConflictAbortsAndReturnsTypedError(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_ = setupWorktreeMutationReadyState(t, env)
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	fakeRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
	fakeRunner.Responses["git rebase origin/main"] = testutil.FakeResponse{
		ExitCode: 1,
		Stderr:   "CONFLICT (content): Merge conflict in README.md",
	}
	fakeRunner.Responses["git rebase --abort"] = testutil.FakeResponse{ExitCode: 0}

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/wt-1/rebase?repo_id="+env.RepoID,
		[]byte(`{}`),
	)
	require.Equal(t, http.StatusConflict, w.Code)

	var resp WorktreeRebaseResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.ERebaseConflict), resp.ErrorCode)
	assert.Contains(t, resp.Hint, "rebase was rolled back")
	assert.Contains(t, fakeRunner.Calls, "git rebase --abort")
}

func TestHandleWorktreeRebase_FetchFailureReturnsTypedError(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_ = setupWorktreeMutationReadyState(t, env)
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	fakeRunner.Responses["git fetch origin"] = testutil.FakeResponse{
		ExitCode: 128,
		Stderr:   "fatal: could not fetch origin",
	}

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/wt-1/rebase?repo_id="+env.RepoID,
		[]byte(`{}`),
	)
	require.Equal(t, http.StatusBadGateway, w.Code)

	var resp WorktreeRebaseResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EGitFetchFailed), resp.ErrorCode)
	assert.Contains(t, resp.Message, "git fetch origin failed")
}

func TestHandleWorktreeRebase_Success(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	_ = setupWorktreeMutationReadyState(t, env)
	fakeRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	fakeRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
	fakeRunner.Responses["git rebase origin/main"] = testutil.FakeResponse{ExitCode: 0}

	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/wt-1/rebase?repo_id="+env.RepoID,
		[]byte(`{}`),
	)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp WorktreeRebaseResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.OK)
	assert.Equal(t, env.RepoID, resp.RepoID)
	assert.Equal(t, "wt-1", resp.IntegrationWorktreeID)
	assert.Equal(t, "agency/alpha", resp.Branch)
	assert.Equal(t, "main", resp.BaseBranch)
}

func TestHandleWorktreeRebase_TrailingObjectRejected(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	w := doWorktreeRequestWithBody(
		t,
		env,
		http.MethodPost,
		"/worktrees/wt-1/rebase?repo_id="+env.RepoID,
		[]byte(`{} {}`),
	)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp WorktreeRebaseResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EInvalidRequest), resp.ErrorCode)
	assert.Equal(t, "invalid request body: expected a single JSON object", resp.Message)
}

func doWorktreeRequestWithBody(t *testing.T, env *readTestEnv, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.apiHandler().ServeHTTP(w, req)
	return w
}

func setupWorktreeMutationReadyState(t *testing.T, env *readTestEnv) string {
	t.Helper()

	treePath := filepath.Join(t.TempDir(), "integration-tree")
	stateDir := filepath.Join(treePath, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o755))
	statusBytes, err := json.Marshal(runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		State:         runnerstatus.StateSucceeded,
		UpdatedAt:     "2026-02-05T12:00:00Z",
		Summary:       "ready for mutation",
		HowToTest:     "go test ./...",
		Questions:     []string{},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "runner_status.json"), statusBytes, 0o644))

	require.NoError(t, env.Store.UpdateIntegrationWorktreeMeta(env.RepoID, "wt-1", func(meta *store.IntegrationWorktreeMeta) {
		meta.TreePath = treePath
		meta.Branch = "agency/alpha"
		meta.BaseBranch = "main"
		meta.Name = "alpha"
	}))

	return treePath
}
