package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		t.Fatalf("git init failed: %v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	// Create initial commit
	testFile := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git add failed: %v, exit %d", err, result.ExitCode)
	}

	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "Initial commit"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git commit failed: %v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	return &testGitEnv{
		RepoPath: repoDir,
		t:        t,
	}
}

func TestHandleWorktreeCreate_ValidationErrors(t *testing.T) {
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
			name:       "invalid name - too short",
			req:        WorktreeCreateRequest{RepoRoot: "/tmp/repo", Name: "a"},
			wantCode:   string(errors.EInvalidName),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid name - uppercase",
			req:        WorktreeCreateRequest{RepoRoot: "/tmp/repo", Name: "MyFeature"},
			wantCode:   string(errors.EInvalidName),
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if resp.ErrorCode != tc.wantCode {
				t.Errorf("error_code = %q, want %q", resp.ErrorCode, tc.wantCode)
			}
			if resp.OK {
				t.Error("expected OK=false")
			}
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
		RepoRoot:     env.RepoPath,
		Name:         "test-feature",
		ParentBranch: "main",
	}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// Handle request
	s.handleWorktreeCreate(w, httpReq)

	// Parse response
	var resp WorktreeCreateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if !resp.OK {
		t.Errorf("expected OK=true, got error: %s - %s", resp.ErrorCode, resp.Message)
	}
	if resp.WorktreeID == "" {
		t.Error("expected worktree_id to be set")
	}
	if resp.TreePath == "" {
		t.Error("expected tree_path to be set")
	}
	if resp.Branch == "" {
		t.Error("expected branch to be set")
	}

	// Verify worktree was created
	if _, err := os.Stat(resp.TreePath); os.IsNotExist(err) {
		t.Errorf("tree_path does not exist: %s", resp.TreePath)
	}

	// Verify INTEGRATION_MARKER exists
	markerPath := filepath.Join(resp.TreePath, ".agency", "INTEGRATION_MARKER")
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Errorf("INTEGRATION_MARKER does not exist: %s", markerPath)
	}
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
		ParentBranch:   "main",
		IdempotencyKey: idempotencyKey,
	}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var resp1 WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp1)

	if !resp1.OK {
		t.Fatalf("first request failed: %s - %s", resp1.ErrorCode, resp1.Message)
	}

	// Second request with same idempotency key
	body, _ = json.Marshal(req)
	httpReq = httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var resp2 WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp2)

	if !resp2.OK {
		t.Fatalf("second request failed: %s - %s", resp2.ErrorCode, resp2.Message)
	}

	// Should return same worktree
	if resp1.WorktreeID != resp2.WorktreeID {
		t.Errorf("idempotent requests returned different worktree IDs: %s vs %s", resp1.WorktreeID, resp2.WorktreeID)
	}
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
		RepoRoot:     env.RepoPath,
		Name:         "unique-feature",
		ParentBranch: "main",
	}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var resp1 WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp1)
	if !resp1.OK {
		t.Fatalf("first request failed: %s - %s", resp1.ErrorCode, resp1.Message)
	}

	// Second request with same name (different key) - should fail
	req2 := WorktreeCreateRequest{
		RepoRoot:       env.RepoPath,
		Name:           "unique-feature",
		ParentBranch:   "main",
		IdempotencyKey: "different-key",
	}
	body, _ = json.Marshal(req2)
	httpReq = httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var resp2 WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&resp2)

	if resp2.OK {
		t.Error("expected second request to fail due to name collision")
	}
	if resp2.ErrorCode != string(errors.EWorktreeNameExists) {
		t.Errorf("expected error code %s, got %s", errors.EWorktreeNameExists, resp2.ErrorCode)
	}
}

func TestHandleWorktreeRm_ValidationErrors(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) {
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
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if resp.ErrorCode != tc.wantCode {
				t.Errorf("error_code = %q, want %q", resp.ErrorCode, tc.wantCode)
			}
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
		RepoRoot:     env.RepoPath,
		Name:         "to-be-removed",
		ParentBranch: "main",
	}
	body, _ := json.Marshal(createReq)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var createResp WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&createResp)
	if !createResp.OK {
		t.Fatalf("failed to create worktree: %s - %s", createResp.ErrorCode, createResp.Message)
	}

	// Verify worktree exists
	if _, err := os.Stat(createResp.TreePath); os.IsNotExist(err) {
		t.Fatalf("worktree was not created: %s", createResp.TreePath)
	}

	// Now remove it (force=true because integration marker file is untracked)
	rmReq := WorktreeRmRequest{Force: true}
	body, _ = json.Marshal(rmReq)
	url := "/worktrees/" + createResp.WorktreeID + "/rm?repo_id=" + createResp.RepoID
	httpReq = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeRm(w, httpReq, createResp.WorktreeID)

	var rmResp WorktreeRmResponse
	if err := json.NewDecoder(w.Body).Decode(&rmResp); err != nil {
		t.Fatalf("failed to decode rm response: %v", err)
	}

	if !rmResp.OK {
		t.Errorf("expected OK=true, got error: %s - %s", rmResp.ErrorCode, rmResp.Message)
	}

	// Verify worktree tree was removed
	if _, err := os.Stat(createResp.TreePath); !os.IsNotExist(err) {
		t.Errorf("worktree tree still exists: %s", createResp.TreePath)
	}
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
		RepoRoot:     env.RepoPath,
		Name:         "idempotent-rm",
		ParentBranch: "main",
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
	if !rmResp1.OK {
		t.Fatalf("first rm failed: %s - %s", rmResp1.ErrorCode, rmResp1.Message)
	}

	// Remove it second time - should succeed (idempotent)
	rmReq2 := WorktreeRmRequest{Force: false} // Second can be non-force since tree is gone
	body, _ = json.Marshal(rmReq2)
	httpReq = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeRm(w, httpReq, createResp.WorktreeID)

	var rmResp2 WorktreeRmResponse
	_ = json.NewDecoder(w.Body).Decode(&rmResp2)

	if !rmResp2.OK {
		t.Errorf("second rm should succeed (idempotent), got error: %s - %s", rmResp2.ErrorCode, rmResp2.Message)
	}
}

func TestWorktreeIdempotencyKey(t *testing.T) {
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
		if got != tc.want {
			t.Errorf("worktreeIdempotencyKey(%q, %q) = %q, want %q", tc.repoID, tc.key, got, tc.want)
		}
	}
}

func TestGetActiveInvocationsForWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	// Add some supervised processes
	s.mu.Lock()
	s.processes["inv-1"] = &SupervisedProcess{
		InvocationID:          "inv-1",
		IntegrationWorktreeID: "wt-a",
		done:                  make(chan struct{}),
	}
	s.processes["inv-2"] = &SupervisedProcess{
		InvocationID:          "inv-2",
		IntegrationWorktreeID: "wt-a",
		done:                  make(chan struct{}),
	}
	s.processes["inv-3"] = &SupervisedProcess{
		InvocationID:          "inv-3",
		IntegrationWorktreeID: "wt-b",
		done:                  make(chan struct{}),
	}
	s.mu.Unlock()

	// Test getting active invocations for wt-a
	active := s.getActiveInvocationsForWorktree("wt-a")
	if len(active) != 2 {
		t.Errorf("expected 2 active invocations for wt-a, got %d", len(active))
	}

	// Test getting active invocations for wt-b
	active = s.getActiveInvocationsForWorktree("wt-b")
	if len(active) != 1 {
		t.Errorf("expected 1 active invocation for wt-b, got %d", len(active))
	}

	// Test getting active invocations for non-existent worktree
	active = s.getActiveInvocationsForWorktree("wt-nonexistent")
	if len(active) != 0 {
		t.Errorf("expected 0 active invocations for wt-nonexistent, got %d", len(active))
	}
}

func TestHandleWorktrees_Routing(t *testing.T) {
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
			name:       "base path without action should 404",
			method:     http.MethodGet,
			path:       "/worktrees/",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()

			s.handleWorktrees(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tc.wantStatus, w.Body.String())
			}
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
		RepoRoot:     env.RepoPath,
		Name:         "integration-test",
		ParentBranch: "main",
	}
	body, _ := json.Marshal(createReq)
	httpReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWorktreeCreate(w, httpReq)

	var createResp WorktreeCreateResponse
	_ = json.NewDecoder(w.Body).Decode(&createResp)

	if !createResp.OK {
		t.Fatalf("create failed: %s - %s", createResp.ErrorCode, createResp.Message)
	}

	// Verify files exist
	treePath := createResp.TreePath
	markerPath := filepath.Join(treePath, ".agency", "INTEGRATION_MARKER")
	metaPath := st.IntegrationWorktreeMetaPath(createResp.RepoID, createResp.WorktreeID)

	if _, err := os.Stat(treePath); os.IsNotExist(err) {
		t.Errorf("tree path does not exist: %s", treePath)
	}
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Errorf("INTEGRATION_MARKER does not exist: %s", markerPath)
	}
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Errorf("meta.json does not exist: %s", metaPath)
	}

	// Read and verify meta.json
	meta, err := st.ReadIntegrationWorktreeMeta(createResp.RepoID, createResp.WorktreeID)
	if err != nil {
		t.Fatalf("failed to read meta.json: %v", err)
	}
	if meta.Name != "integration-test" {
		t.Errorf("meta.Name = %q, want %q", meta.Name, "integration-test")
	}
	if meta.State != store.WorktreeStatePresent {
		t.Errorf("meta.State = %q, want %q", meta.State, store.WorktreeStatePresent)
	}

	// Verify branch was created
	branches, _ := exec.NewRealRunner().Run(ctx, "git", []string{"-C", env.RepoPath, "branch", "-a"}, exec.RunOpts{})
	if !strings.Contains(branches.Stdout, createResp.Branch) {
		t.Errorf("branch %q not found in git branches", createResp.Branch)
	}

	// Remove worktree (force=true because integration marker file is untracked)
	rmReq := WorktreeRmRequest{Force: true}
	body, _ = json.Marshal(rmReq)
	url := "/worktrees/" + createResp.WorktreeID + "/rm?repo_id=" + createResp.RepoID
	httpReq = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleWorktreeRm(w, httpReq, createResp.WorktreeID)

	var rmResp WorktreeRmResponse
	_ = json.NewDecoder(w.Body).Decode(&rmResp)

	if !rmResp.OK {
		t.Fatalf("rm failed: %s - %s", rmResp.ErrorCode, rmResp.Message)
	}

	// Verify tree was removed
	if _, err := os.Stat(treePath); !os.IsNotExist(err) {
		t.Errorf("tree path still exists: %s", treePath)
	}

	// Verify meta.json shows archived state
	meta, err = st.ReadIntegrationWorktreeMeta(createResp.RepoID, createResp.WorktreeID)
	if err != nil {
		t.Fatalf("failed to read meta.json after rm: %v", err)
	}
	if meta.State != store.WorktreeStateArchived {
		t.Errorf("meta.State = %q, want %q after rm", meta.State, store.WorktreeStateArchived)
	}
}
