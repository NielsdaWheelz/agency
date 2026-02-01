package daemon

import (
	"bytes"
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

func TestControlPlaneStart_ValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		req      ControlPlaneStartRequest
		wantCode string
	}{
		{
			name: "missing client_request_id",
			req: ControlPlaneStartRequest{
				RepoRoot:    "/tmp/repo",
				WorktreeRef: "wt-1",
				Runner:      "claude",
				Prompt:      "test",
			},
			wantCode: "E_INVALID_REQUEST",
		},
		{
			name: "missing repo_root",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				WorktreeRef:     "wt-1",
				Runner:          "claude",
				Prompt:          "test",
			},
			wantCode: "E_INVALID_REQUEST",
		},
		{
			name: "missing worktree_ref",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				Runner:          "claude",
				Prompt:          "test",
			},
			wantCode: "E_INVALID_REQUEST",
		},
		{
			name: "missing runner",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Prompt:          "test",
			},
			wantCode: "E_INVALID_REQUEST",
		},
		{
			name: "missing prompt",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          "claude",
			},
			wantCode: string(errors.EPromptRequired),
		},
		{
			name: "prompt too large",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          "claude",
				Prompt:          strings.Repeat("x", MaxPromptSize+1),
			},
			wantCode: string(errors.EPromptTooLarge),
		},
		{
			name: "unrecognized runner",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          "unknown",
				Prompt:          "test",
			},
			wantCode: string(errors.ERunnerNotFound),
		},
		{
			name: "reserved claude arg --output-format",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          "claude",
				Prompt:          "test",
				RunnerArgs:      []string{"--output-format"},
			},
			wantCode: string(errors.ERunnerArgConflict),
		},
		{
			name: "reserved claude arg -p",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          "claude",
				Prompt:          "test",
				RunnerArgs:      []string{"-p"},
			},
			wantCode: string(errors.ERunnerArgConflict),
		},
		{
			name: "reserved codex arg exec",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          "codex",
				Prompt:          "test",
				RunnerArgs:      []string{"exec"},
			},
			wantCode: string(errors.ERunnerArgConflict),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
			s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

			body, _ := json.Marshal(tc.req)
			req := httptest.NewRequest(http.MethodPost, "/invocations/start_headless", bytes.NewReader(body))
			w := httptest.NewRecorder()

			s.handleControlPlaneStartHeadless(w, req)

			var resp ControlPlaneStartResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if resp.OK {
				t.Error("expected OK=false")
			}
			if resp.ErrorCode != tc.wantCode {
				t.Errorf("error_code = %q, want %q", resp.ErrorCode, tc.wantCode)
			}
		})
	}
}

func TestControlPlaneStart_UnsafeRepoRoot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Resolve tmpDir through EvalSymlinks, matching what DaemonStart does
	// in production. On macOS, /var is a symlink to /private/var.
	tmpDir := t.TempDir()
	tmpDir, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	// Create a path that looks like it's inside an agency-managed worktree.
	fakeWorktreePath := filepath.Join(tmpDir, "repos", "some-repo", "integration_worktrees", "wt-1", "tree")
	if err := os.MkdirAll(fakeWorktreePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	req := ControlPlaneStartRequest{
		ClientRequestID: "test-uuid",
		RepoRoot:        fakeWorktreePath,
		WorktreeRef:     "wt-1",
		Runner:          "claude",
		Prompt:          "test",
	}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/invocations/start_headless", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleControlPlaneStartHeadless(w, httpReq)

	var resp ControlPlaneStartResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.OK {
		t.Error("expected OK=false for unsafe repo root")
	}
	if resp.ErrorCode != string(errors.EUnsafeRepoRoot) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, errors.EUnsafeRepoRoot)
	}
}

func TestControlPlaneStart_WorktreeNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testutil.HermeticGitEnv(t)

	env := setupGitRepo(t)
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	req := ControlPlaneStartRequest{
		ClientRequestID: "test-uuid",
		RepoRoot:        env.RepoPath,
		WorktreeRef:     "nonexistent-worktree",
		Runner:          "claude",
		Prompt:          "test",
	}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/invocations/start_headless", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleControlPlaneStartHeadless(w, httpReq)

	var resp ControlPlaneStartResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.OK {
		t.Error("expected OK=false for nonexistent worktree")
	}
	// Could be E_WORKTREE_NOT_FOUND or E_INTERNAL depending on whether repo is registered.
	if resp.ErrorCode == "" {
		t.Error("expected an error code")
	}
}

func TestControlPlaneStart_RunnerNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testutil.HermeticGitEnv(t)

	env := setupGitRepo(t)
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write config pointing to nonexistent runner binary.
	// Must include "defaults" for LoadUserConfig validation to pass.
	cfg := `{"version":1,"defaults":{"runner":"claude","editor":"code"},"runners":{"claude":"/nonexistent/path/to/runner"}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	// First create a worktree so the control plane can find it.
	createReq := WorktreeCreateRequest{
		RepoRoot:     env.RepoPath,
		Name:         "runner-test",
		ParentBranch: "main",
	}
	createBody, _ := json.Marshal(createReq)
	createHTTPReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(createBody))
	createW := httptest.NewRecorder()
	s.handleWorktreeCreate(createW, createHTTPReq)

	var createResp WorktreeCreateResponse
	_ = json.NewDecoder(createW.Body).Decode(&createResp)
	if !createResp.OK {
		t.Fatalf("create worktree failed: %s - %s", createResp.ErrorCode, createResp.Message)
	}

	// Now try to start an invocation — should fail because runner binary doesn't exist.
	req := ControlPlaneStartRequest{
		ClientRequestID: "test-uuid",
		RepoRoot:        env.RepoPath,
		WorktreeRef:     "runner-test",
		Runner:          "claude",
		Prompt:          "test",
	}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/invocations/start_headless", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleControlPlaneStartHeadless(w, httpReq)

	var resp ControlPlaneStartResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.OK {
		t.Error("expected OK=false for nonexistent runner")
	}
}
