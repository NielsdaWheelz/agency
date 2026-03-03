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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestControlPlaneStart_ValidationErrors(t *testing.T) {
	t.Parallel()
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
			s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

			body, _ := json.Marshal(tc.req)
			req := httptest.NewRequest(http.MethodPost, "/invocations/start_headless", bytes.NewReader(body))
			w := httptest.NewRecorder()

			s.handleControlPlaneStartHeadless(w, req)

			var resp ControlPlaneStartResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

			assert.False(t, resp.OK, "expected OK=false")
			assert.Equal(t, tc.wantCode, resp.ErrorCode)
		})
	}
}

func TestControlPlaneStart_RunnerTargetSetPassesValidation(t *testing.T) {
	t.Parallel()

	runners := []string{
		"claude",
		"claude-code",
		"codex",
		"amp",
		"opencode",
		"cursor",
		"cursor-cli",
		"droid",
	}

	for _, runner := range runners {
		runner := runner
		t.Run(runner, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
			s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

			reqPayload := ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          runner,
				Prompt:          "test",
			}
			body, _ := json.Marshal(reqPayload)
			req := httptest.NewRequest(http.MethodPost, "/invocations/start_headless", bytes.NewReader(body))
			w := httptest.NewRecorder()

			s.handleControlPlaneStartHeadless(w, req)

			var resp ControlPlaneStartResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")
			require.False(t, resp.OK, "request should fail later with non-repo path")
			assert.NotEqual(t, string(errors.ERunnerNotFound), resp.ErrorCode)
		})
	}
}

func TestControlPlaneStartHeadless_ErrorResponseIncludesRequestID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	reqPayload := ControlPlaneStartRequest{
		RepoRoot:    "/tmp/repo",
		WorktreeRef: "wt-1",
		Runner:      "claude",
		Prompt:      "test",
		// intentionally missing client_request_id
	}
	body, _ := json.Marshal(reqPayload)
	req := httptest.NewRequest(http.MethodPost, "/invocations/start_headless", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleControlPlaneStartHeadless(w, req)

	var payload map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&payload), "failed to decode response")
	requestID, ok := payload["request_id"].(string)
	require.True(t, ok, "request_id must be present")
	assert.NotEmpty(t, requestID)
	assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
}

func TestControlPlaneStartHeaded_ErrorResponseIncludesRequestID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	reqPayload := ControlPlaneStartHeadedRequest{
		RepoRoot:    "/tmp/repo",
		WorktreeRef: "wt-1",
		Runner:      "claude",
		// intentionally missing client_request_id
	}
	body, _ := json.Marshal(reqPayload)
	req := httptest.NewRequest(http.MethodPost, "/invocations/start_headed", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleControlPlaneStartHeaded(w, req)

	var payload map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&payload), "failed to decode response")
	requestID, ok := payload["request_id"].(string)
	require.True(t, ok, "request_id must be present")
	assert.NotEmpty(t, requestID)
	assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
}

func TestControlPlaneStartHeaded_RunnerValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      ControlPlaneStartHeadedRequest
		wantCode string
	}{
		{
			name: "unrecognized runner",
			req: ControlPlaneStartHeadedRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          "unknown",
			},
			wantCode: string(errors.ERunnerNotFound),
		},
		{
			name: "reserved claude arg --output-format",
			req: ControlPlaneStartHeadedRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          "claude",
				RunnerArgs:      []string{"--output-format"},
			},
			wantCode: string(errors.ERunnerArgConflict),
		},
		{
			name: "reserved cursor arg -p",
			req: ControlPlaneStartHeadedRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          "cursor",
				RunnerArgs:      []string{"-p"},
			},
			wantCode: string(errors.ERunnerArgConflict),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
			s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

			body, _ := json.Marshal(tc.req)
			req := httptest.NewRequest(http.MethodPost, "/invocations/start_headed", bytes.NewReader(body))
			w := httptest.NewRecorder()

			s.handleControlPlaneStartHeaded(w, req)

			var resp ControlPlaneStartHeadedResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")
			assert.False(t, resp.OK, "expected OK=false")
			assert.Equal(t, tc.wantCode, resp.ErrorCode)
		})
	}
}

func TestControlPlaneStart_UnsafeRepoRoot(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Resolve tmpDir through EvalSymlinks, matching what DaemonStart does
	// in production. On macOS, /var is a symlink to /private/var.
	tmpDir := t.TempDir()
	tmpDir, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err, "eval symlinks")
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	// Create a path that looks like it's inside an agency-managed worktree.
	fakeWorktreePath := filepath.Join(tmpDir, "repos", "some-repo", "integration_worktrees", "wt-1", "tree")
	require.NoError(t, os.MkdirAll(fakeWorktreePath, 0o755), "mkdir")

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
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

	assert.False(t, resp.OK, "expected OK=false for unsafe repo root")
	assert.Equal(t, string(errors.EUnsafeRepoRoot), resp.ErrorCode)
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
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

	assert.False(t, resp.OK, "expected OK=false for nonexistent worktree")
	// Could be E_WORKTREE_NOT_FOUND or E_INTERNAL depending on whether repo is registered.
	assert.NotEmpty(t, resp.ErrorCode, "expected an error code")
}

func TestControlPlaneStart_RunnerNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testutil.HermeticGitEnv(t)

	env := setupGitRepo(t)
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755), "mkdir")

	// Write config pointing to nonexistent runner binary.
	// Must include "defaults" for LoadUserConfig validation to pass.
	cfg := `{"version":1,"defaults":{"runner":"claude","editor":"code"},"runners":{"claude":"/nonexistent/path/to/runner"}}`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(cfg), 0o644), "write config")

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
	require.True(t, createResp.OK, "create worktree failed: %s - %s", createResp.ErrorCode, createResp.Message)

	// Now try to start an invocation -- should fail because runner binary doesn't exist.
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
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

	assert.False(t, resp.OK, "expected OK=false for nonexistent runner")
}

func TestMergeEnvDeterministic_OverrideWinsNoDuplicatesSorted(t *testing.T) {
	t.Parallel()

	base := []string{
		"Z=9",
		"A=old",
		"A=older",
		"PATH=/bin",
	}
	merged := mergeEnvDeterministic(
		base,
		map[string]string{
			"CI": "1",
			"A":  "default",
		},
		map[string]string{
			"A": "override",
			"B": "2",
		},
	)

	assert.Equal(t, []string{
		"A=override",
		"B=2",
		"CI=1",
		"PATH=/bin",
		"Z=9",
	}, merged)
}

func TestNonInteractiveRunnerEnv_ContainsRequiredDefaults(t *testing.T) {
	t.Parallel()

	env := nonInteractiveRunnerEnv()
	assert.Equal(t, "1", env["CI"])
	assert.Equal(t, "0", env["GIT_TERMINAL_PROMPT"])
	assert.Equal(t, "1", env["GH_PROMPT_DISABLED"])
}
