package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

// testGitEnv holds a temporary git repository for testing
type testGitEnv struct {
	RepoPath string
	t        *testing.T
}

// setupGitRepo creates a temporary git repository for testing
func setupGitRepo(t *testing.T) *testGitEnv {
	t.Helper()
	testutil.HermeticGitEnv(t)

	repoDir := t.TempDir()
	cr := exec.NewRealRunner()
	ctx := context.Background()

	// Initialize git repo
	result, err := cr.Run(ctx, "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		require.FailNow(t, "git init failed", "err=%v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	// Create initial commit
	testFile := filepath.Join(repoDir, "README.md")
	require.NoError(t, os.WriteFile(testFile, []byte("# Test Repo\n"), 0o644), "failed to write test file")

	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		require.FailNow(t, "git add failed", "err=%v, exit %d", err, result.ExitCode)
	}

	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "Initial commit"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		require.FailNow(t, "git commit failed", "err=%v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	return &testGitEnv{
		RepoPath: repoDir,
		t:        t,
	}
}

func writeWorktreeGuardInvocation(t *testing.T, st *store.Store, repoID, worktreeID, invocationID string, status store.InvocationStatus, landingStatus store.LandingStatus) {
	t.Helper()

	_, err := st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err, "ensure invocation dir")

	meta := store.NewInvocationMeta(
		invocationID,
		"",
		worktreeID,
		filepath.Join(t.TempDir(), "sandbox", invocationID, "tree"),
		"agency/sandbox-"+invocationID,
		"basecommit",
		"claude-code",
		store.RunnerModeHeadless,
		time.Now(),
	)
	meta.Status = status
	meta.LandingStatus = landingStatus
	if status == store.InvocationStatusFinished || status == store.InvocationStatusFailed {
		meta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}

	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, meta), "write invocation meta")
}

func TestHandleWorktreeCreate_ValidationErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		req        WorktreeCreateRequest
		wantCode   string
		wantStatus int
	}{
		{
			name:       "missing repo_root",
			req:        WorktreeCreateRequest{Name: "my-feature"},
			wantCode:   "E_INVALID_REQUEST",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing name",
			req:        WorktreeCreateRequest{RepoRoot: "/tmp/repo"},
			wantCode:   "E_INVALID_REQUEST",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing base_branch",
			req:        WorktreeCreateRequest{RepoRoot: "/tmp/repo", Name: "my-feature"},
			wantCode:   "E_INVALID_REQUEST",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Create server with minimal setup
			tmpDir := t.TempDir()
			st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
			s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

			// Create request
			body, _ := json.Marshal(tc.req)
			req := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
			w := httptest.NewRecorder()

			// Handle request
			s.handleWorktreeCreate(w, req)

			// Parse response
			var resp WorktreeCreateResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCode, resp.ErrorCode)
			assert.False(t, resp.OK, "expected OK=false")
		})
	}
}

func TestHandleWorktreeCreate_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a real git repo for testing
	env := setupGitRepo(t)

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	// Create request
	req := WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "test-feature",
		BaseBranch: "main",
	}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// Handle request
	s.handleWorktreeCreate(w, httpReq)

	// Parse response
	var resp WorktreeCreateResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

	assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.True(t, resp.OK, "expected OK=true, got error: %s - %s", resp.ErrorCode, resp.Message)
	assert.NotEmpty(t, resp.WorktreeID, "expected worktree_id to be set")
	assert.NotEmpty(t, resp.TreePath, "expected tree_path to be set")
	assert.NotEmpty(t, resp.Branch, "expected branch to be set")

	// Verify worktree was created
	_, err := os.Stat(resp.TreePath)
	assert.False(t, os.IsNotExist(err), "tree_path does not exist: %s", resp.TreePath)

	// Verify INTEGRATION_MARKER exists
	markerPath := filepath.Join(resp.TreePath, ".agency", "INTEGRATION_MARKER")
	_, err = os.Stat(markerPath)
	assert.False(t, os.IsNotExist(err), "INTEGRATION_MARKER does not exist: %s", markerPath)
}

func TestHandleWorktreeCreate_Idempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a real git repo for testing
	env := setupGitRepo(t)

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	idempotencyKey := "test-idempotency-key"

	// First request
	req := WorktreeCreateRequest{
		RepoRoot:       env.RepoPath,
		Name:           "idempotent-feature",
		BaseBranch:     "main",
		IdempotencyKey: idempotencyKey,
	}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var resp1 WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp1)

	require.True(t, resp1.OK, "first request failed: %s - %s", resp1.ErrorCode, resp1.Message)

	// Second request with same idempotency key
	body, _ = json.Marshal(req)
	httpReq = httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var resp2 WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp2)

	require.True(t, resp2.OK, "second request failed: %s - %s", resp2.ErrorCode, resp2.Message)

	// Should return same worktree
	assert.Equal(t, resp1.WorktreeID, resp2.WorktreeID, "idempotent requests returned different worktree IDs")
}

func TestHandleWorktreeCreate_NameUniqueness(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a real git repo for testing
	env := setupGitRepo(t)

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	// First request - should succeed
	req := WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "unique-feature",
		BaseBranch: "main",
	}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var resp1 WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp1)
	require.True(t, resp1.OK, "first request failed: %s - %s", resp1.ErrorCode, resp1.Message)

	// Second request with same name (different key) - should fail
	req2 := WorktreeCreateRequest{
		RepoRoot:       env.RepoPath,
		Name:           "unique-feature",
		BaseBranch:     "main",
		IdempotencyKey: "different-key",
	}
	body, _ = json.Marshal(req2)
	httpReq = httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var resp2 WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp2)

	assert.False(t, resp2.OK, "expected second request to fail due to name collision")
	assert.Equal(t, string(errors.ENameExists), resp2.ErrorCode)
}

func TestHandleWorktreeRm_ValidationErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		repoID     string
		wantCode   string
		wantStatus int
	}{
		{
			name:       "missing repo_id",
			repoID:     "",
			wantCode:   "E_INVALID_REQUEST",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
			s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

			req := WorktreeRmRequest{Force: false}
			body, _ := json.Marshal(req)

			url := "/worktrees/test-id/rm"
			if tc.repoID != "" {
				url += "?repo_id=" + tc.repoID
			}

			httpReq := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			w := httptest.NewRecorder()

			s.handleWorktreeRm(w, httpReq, "test-id")

			var resp WorktreeRmResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCode, resp.ErrorCode)
		})
	}
}

func TestHandleWorktreeRm_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a real git repo for testing
	env := setupGitRepo(t)

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	// First create a worktree
	createReq := WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "to-be-removed",
		BaseBranch: "main",
	}
	body, _ := json.Marshal(createReq)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var createResp WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&createResp)
	require.True(t, createResp.OK, "failed to create worktree: %s - %s", createResp.ErrorCode, createResp.Message)

	// Verify worktree exists
	_, err := os.Stat(createResp.TreePath)
	require.False(t, os.IsNotExist(err), "worktree was not created: %s", createResp.TreePath)

	// Now remove it (force=true because integration marker file is untracked)
	rmReq := WorktreeRmRequest{Force: true}
	body, _ = json.Marshal(rmReq)
	url := "/worktrees/" + createResp.WorktreeID + "/rm?repo_id=" + createResp.RepoID
	httpReq = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeRm(w, httpReq, createResp.WorktreeID)

	var rmResp WorktreeRmResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rmResp), "failed to decode rm response")

	assert.True(t, rmResp.OK, "expected OK=true, got error: %s - %s", rmResp.ErrorCode, rmResp.Message)

	// Verify worktree tree was removed
	_, err = os.Stat(createResp.TreePath)
	assert.True(t, os.IsNotExist(err), "worktree tree still exists: %s", createResp.TreePath)
}

func TestHandleWorktreeRm_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a real git repo for testing
	env := setupGitRepo(t)

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	// First create a worktree
	createReq := WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "idempotent-rm",
		BaseBranch: "main",
	}
	body, _ := json.Marshal(createReq)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var createResp WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&createResp)

	// Remove it first time (force=true because integration marker file is untracked)
	rmReq := WorktreeRmRequest{Force: true}
	body, _ = json.Marshal(rmReq)
	url := "/worktrees/" + createResp.WorktreeID + "/rm?repo_id=" + createResp.RepoID
	httpReq = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeRm(w, httpReq, createResp.WorktreeID)

	var rmResp1 WorktreeRmResponse
	_ = json.NewDecoder(w.Body).Decode(&rmResp1)
	require.True(t, rmResp1.OK, "first rm failed: %s - %s", rmResp1.ErrorCode, rmResp1.Message)

	// Remove it second time - should succeed (idempotent)
	rmReq2 := WorktreeRmRequest{Force: false} // Second can be non-force since tree is gone
	body, _ = json.Marshal(rmReq2)
	httpReq = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeRm(w, httpReq, createResp.WorktreeID)

	var rmResp2 WorktreeRmResponse
	_ = json.NewDecoder(w.Body).Decode(&rmResp2)

	assert.True(t, rmResp2.OK, "second rm should succeed (idempotent), got error: %s - %s", rmResp2.ErrorCode, rmResp2.Message)
}

func TestWorktreeIdempotencyKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		repoID string
		key    string
		want   string
	}{
		{"abc123", "uuid-1", "abc123:worktree:uuid-1"},
		{"def456", "uuid-2", "def456:worktree:uuid-2"},
	}

	for _, tc := range tests {
		got := worktreeIdempotencyKey(tc.repoID, tc.key)
		assert.Equal(t, tc.want, got)
	}
}

func TestUnresolvedInvocationsForWorktree(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	writeWorktreeGuardInvocation(t, st, "repo-1", "wt-a", "inv-1", store.InvocationStatusRunning, "")
	writeWorktreeGuardInvocation(t, st, "repo-1", "wt-a", "inv-2", store.InvocationStatusFinished, store.LandingStatusPending)
	writeWorktreeGuardInvocation(t, st, "repo-1", "wt-a", "inv-3", store.InvocationStatusFailed, store.LandingStatusPending)
	writeWorktreeGuardInvocation(t, st, "repo-1", "wt-a", "inv-4", store.InvocationStatusFinished, store.LandingStatusLanded)
	writeWorktreeGuardInvocation(t, st, "repo-1", "wt-a", "inv-5", store.InvocationStatusFinished, store.LandingStatusDiscarded)
	writeWorktreeGuardInvocation(t, st, "repo-1", "wt-b", "inv-6", store.InvocationStatusRunning, "")

	unresolved, err := s.unresolvedInvocationsForWorktree("repo-1", "wt-a")
	require.NoError(t, err)
	require.Len(t, unresolved, 3)
	assert.Equal(t, "inv-3", unresolved[0].InvocationID)
	assert.Equal(t, "inv-2", unresolved[1].InvocationID)
	assert.Equal(t, "inv-1", unresolved[2].InvocationID)
}

func TestUnresolvedInvocationsForWorktree_UnknownLandingStatusIsCorrupt(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)
	writeWorktreeGuardInvocation(t, st, "repo-1", "wt-a", "inv-1", store.InvocationStatusFinished, store.LandingStatus("bogus"))

	_, err := s.unresolvedInvocationsForWorktree("repo-1", "wt-a")
	require.Error(t, err)
	assert.Equal(t, errors.EStoreCorrupt, errors.GetCode(err))
}

func TestHandleWorktrees_Routing(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{
			name:       "create with GET should fail",
			method:     http.MethodGet,
			path:       "/worktrees/create",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "rm with GET should fail",
			method:     http.MethodGet,
			path:       "/worktrees/test-id/rm",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "unknown action should 404",
			method:     http.MethodPost,
			path:       "/worktrees/test-id/unknown",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "pr sync with GET should fail",
			method:     http.MethodGet,
			path:       "/worktrees/test-id/pr/sync",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "unsupported nested route should 404",
			method:     http.MethodGet,
			path:       "/worktrees/test-id/merge",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "pr merge with GET routes to merge read handler",
			method:     http.MethodGet,
			path:       "/worktrees/test-id/pr/merge",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "rebase with GET should fail",
			method:     http.MethodGet,
			path:       "/worktrees/test-id/rebase",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "old update route should 404",
			method:     http.MethodPost,
			path:       "/worktrees/test-id/update",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "base path with GET should list worktrees (PR-12)",
			method:     http.MethodGet,
			path:       "/worktrees/",
			wantStatus: http.StatusOK,
		},
		{
			name:       "base path with POST should 405",
			method:     http.MethodPost,
			path:       "/worktrees/",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()

			s.handleWorktrees(w, req)

			assert.Equal(t, tc.wantStatus, w.Code, "body: %s", w.Body.String())
		})
	}
}

// Integration test to verify the full flow
func TestWorktreeCreateAndRemove_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	_ = ctx // Will be used when we add more integration tests

	// Create a real git repo for testing
	env := setupGitRepo(t)

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	// Create worktree
	createReq := WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "integration-test",
		BaseBranch: "main",
	}
	body, _ := json.Marshal(createReq)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var createResp WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&createResp)

	require.True(t, createResp.OK, "create failed: %s - %s", createResp.ErrorCode, createResp.Message)

	// Verify files exist
	treePath := createResp.TreePath
	markerPath := filepath.Join(treePath, ".agency", "INTEGRATION_MARKER")
	metaPath := st.IntegrationWorktreeMetaPath(createResp.RepoID, createResp.WorktreeID)

	_, err := os.Stat(treePath)
	assert.False(t, os.IsNotExist(err), "tree path does not exist: %s", treePath)
	_, err = os.Stat(markerPath)
	assert.False(t, os.IsNotExist(err), "INTEGRATION_MARKER does not exist: %s", markerPath)
	_, err = os.Stat(metaPath)
	assert.False(t, os.IsNotExist(err), "meta.json does not exist: %s", metaPath)

	// Read and verify meta.json
	meta, err := st.ReadIntegrationWorktreeMeta(createResp.RepoID, createResp.WorktreeID)
	require.NoError(t, err, "failed to read meta.json")
	assert.Equal(t, "integration-test", meta.Name)
	assert.Equal(t, store.WorktreeStatePresent, meta.State)

	// Verify branch was created
	branches, _ := exec.NewRealRunner().Run(ctx, "git", []string{"-C", env.RepoPath, "branch", "-a"}, exec.RunOpts{})
	assert.Contains(t, branches.Stdout, createResp.Branch, "branch %q not found in git branches", createResp.Branch)

	// Remove worktree (force=true because integration marker file is untracked)
	rmReq := WorktreeRmRequest{Force: true}
	body, _ = json.Marshal(rmReq)
	url := "/worktrees/" + createResp.WorktreeID + "/rm?repo_id=" + createResp.RepoID
	httpReq = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeRm(w, httpReq, createResp.WorktreeID)

	var rmResp WorktreeRmResponse
	_ = json.NewDecoder(w.Body).Decode(&rmResp)

	require.True(t, rmResp.OK, "rm failed: %s - %s", rmResp.ErrorCode, rmResp.Message)

	// Verify tree was removed
	_, err = os.Stat(treePath)
	assert.True(t, os.IsNotExist(err), "tree path still exists: %s", treePath)

	// Verify meta.json shows archived state
	meta, err = st.ReadIntegrationWorktreeMeta(createResp.RepoID, createResp.WorktreeID)
	require.NoError(t, err, "failed to read meta.json after rm")
	assert.Equal(t, store.WorktreeStateArchived, meta.State, "meta.State after rm")
}

// TestHandleWorktreeRm_NotAnIntegrationWorktree verifies that removing a worktree
// whose tree lacks the .agency/INTEGRATION_MARKER returns E_NOT_AN_INTEGRATION_WORKTREE.
func TestHandleWorktreeRm_NotAnIntegrationWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a real git repo
	env := setupGitRepo(t)

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	// Create a worktree through the handler
	createReq := WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "no-marker-test",
		BaseBranch: "main",
	}
	body, _ := json.Marshal(createReq)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var createResp WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&createResp)
	require.True(t, createResp.OK, "create failed: %s - %s", createResp.ErrorCode, createResp.Message)

	// Remove the INTEGRATION_MARKER to simulate a non-integration worktree
	markerPath := filepath.Join(createResp.TreePath, ".agency", "INTEGRATION_MARKER")
	require.NoError(t, os.Remove(markerPath), "failed to remove INTEGRATION_MARKER")

	// Now attempt to rm - should fail with E_NOT_AN_INTEGRATION_WORKTREE
	rmReq := WorktreeRmRequest{Force: false}
	body, _ = json.Marshal(rmReq)
	url := "/worktrees/" + createResp.WorktreeID + "/rm?repo_id=" + createResp.RepoID
	httpReq = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeRm(w, httpReq, createResp.WorktreeID)

	var rmResp WorktreeRmResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rmResp), "failed to decode rm response")

	assert.False(t, rmResp.OK, "expected rm to fail")
	assert.Equal(t, string(errors.ENotAnIntegrationWorktree), rmResp.ErrorCode,
		"expected E_NOT_AN_INTEGRATION_WORKTREE")
}

func TestHandleWorktreeRm_BlocksOnUnresolvedInvocations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cases := []struct {
		name         string
		status       store.InvocationStatus
		landingState store.LandingStatus
	}{
		{name: "starting_empty", status: store.InvocationStatusStarting, landingState: ""},
		{name: "running_empty", status: store.InvocationStatusRunning, landingState: ""},
		{name: "finished_pending", status: store.InvocationStatusFinished, landingState: store.LandingStatusPending},
		{name: "failed_pending", status: store.InvocationStatusFailed, landingState: store.LandingStatusPending},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupGitRepo(t)
			tmpDir := t.TempDir()
			st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
			s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

			createReq := WorktreeCreateRequest{
				RepoRoot:   env.RepoPath,
				Name:       "unresolved-rm-test",
				BaseBranch: "main",
			}
			body, _ := json.Marshal(createReq)
			httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
			w := httptest.NewRecorder()
			s.handleWorktreeCreate(w, httpReq)

			var createResp WorktreeCreateResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&createResp))
			require.True(t, createResp.OK, "create failed: %s - %s", createResp.ErrorCode, createResp.Message)

			writeWorktreeGuardInvocation(t, st, createResp.RepoID, createResp.WorktreeID, "inv-"+tc.name, tc.status, tc.landingState)

			rmReq := WorktreeRmRequest{Force: false}
			body, _ = json.Marshal(rmReq)
			url := "/worktrees/" + createResp.WorktreeID + "/rm?repo_id=" + createResp.RepoID
			httpReq = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			w = httptest.NewRecorder()
			s.handleWorktreeRm(w, httpReq, createResp.WorktreeID)

			var rmResp WorktreeRmResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&rmResp), "failed to decode rm response")

			assert.False(t, rmResp.OK, "expected rm to fail")
			assert.Equal(t, string(errors.EWorktreeHasUnresolvedInvocations), rmResp.ErrorCode)
		})
	}
}

// TestHandleWorktreeRm_BrokenWorktree verifies that removing a worktree whose
// meta.json is corrupt returns E_WORKTREE_BROKEN.
func TestHandleWorktreeRm_BrokenWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a real git repo
	env := setupGitRepo(t)

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	// Create a worktree through the handler
	createReq := WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "broken-meta-test",
		BaseBranch: "main",
	}
	body, _ := json.Marshal(createReq)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var createResp WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&createResp)
	require.True(t, createResp.OK, "create failed: %s - %s", createResp.ErrorCode, createResp.Message)

	// Corrupt the meta.json to make the worktree "broken"
	metaPath := st.IntegrationWorktreeMetaPath(createResp.RepoID, createResp.WorktreeID)
	require.NoError(t, os.WriteFile(metaPath, []byte("not valid json!!!"), 0o644),
		"failed to corrupt meta.json")

	// Attempt to rm by exact worktree ID - should resolve but detect broken record
	rmReq := WorktreeRmRequest{Force: false}
	body, _ = json.Marshal(rmReq)
	url := "/worktrees/" + createResp.WorktreeID + "/rm?repo_id=" + createResp.RepoID
	httpReq = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeRm(w, httpReq, createResp.WorktreeID)

	var rmResp WorktreeRmResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rmResp), "failed to decode rm response")

	assert.False(t, rmResp.OK, "expected rm to fail")
	assert.Equal(t, string(errors.EWorktreeBroken), rmResp.ErrorCode,
		"expected E_WORKTREE_BROKEN for corrupt meta.json")
}
