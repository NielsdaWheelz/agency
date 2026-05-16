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
				Runner:      "claude-code",
				Prompt:      "test",
			},
			wantCode: "E_INVALID_REQUEST",
		},
		{
			name: "missing repo_root",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				WorktreeRef:     "wt-1",
				Runner:          "claude-code",
				Prompt:          "test",
			},
			wantCode: "E_INVALID_REQUEST",
		},
		{
			name: "missing worktree_ref",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				Runner:          "claude-code",
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
				Runner:          "claude-code",
			},
			wantCode: string(errors.EPromptRequired),
		},
		{
			name: "prompt too large",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          "claude-code",
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
				Runner:          "claude-code",
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
				Runner:          "claude-code",
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
		{
			name: "reserved codex approval arg",
			req: ControlPlaneStartRequest{
				ClientRequestID: "test-uuid",
				RepoRoot:        "/tmp/repo",
				WorktreeRef:     "wt-1",
				Runner:          "codex",
				Prompt:          "test",
				RunnerArgs:      []string{"--ask-for-approval", "never"},
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

func TestControlPlaneStartFingerprintIgnoresProfileEnvValues(t *testing.T) {
	t.Parallel()

	requestEnv := map[string]string{"REQUEST_TOKEN": "old"}
	req := ControlPlaneStartRequest{
		RepoRoot:         "/repo",
		WorktreeRef:      "wt-1",
		Runner:           "claude-code",
		Prompt:           "same prompt",
		ExecutionProfile: "personal",
		Env:              envForLaunch(map[string]string{"PROFILE_TOKEN": "old"}, requestEnv),
	}
	first := controlPlaneStartFingerprint("/repo", "wt-1", "/checkout", store.RunnerModeHeadless, req, requestEnv)

	req.Env = envForLaunch(map[string]string{"PROFILE_TOKEN": "new"}, requestEnv)
	second := controlPlaneStartFingerprint("/repo", "wt-1", "/checkout", store.RunnerModeHeadless, req, requestEnv)
	assert.Equal(t, first, second)

	changedRequestEnv := map[string]string{"REQUEST_TOKEN": "new"}
	req.Env = envForLaunch(map[string]string{"PROFILE_TOKEN": "new"}, changedRequestEnv)
	third := controlPlaneStartFingerprint("/repo", "wt-1", "/checkout", store.RunnerModeHeadless, req, changedRequestEnv)
	assert.Equal(t, first, third)

	changedRequestEnv["REQUEST_TOKEN_2"] = "new"
	req.Env = envForLaunch(map[string]string{"PROFILE_TOKEN": "new"}, changedRequestEnv)
	fourth := controlPlaneStartFingerprint("/repo", "wt-1", "/checkout", store.RunnerModeHeadless, req, changedRequestEnv)
	assert.NotEqual(t, first, fourth)
}

func TestFindInvocationByClientRequestIDIgnoresTaskInvocations(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	meta := store.NewInvocationMeta("inv-task", "", "wt-1", "/sandbox", "/checkout", "work", "agency/sandbox-inv-task", "base", "codex", store.RunnerModeHeadless, time.Now())
	meta.TaskID = "task-1"
	meta.ClientRequestID = "shared-request"
	meta.RequestFingerprint = "task-fingerprint"
	_, err := st.EnsureInvocationDir("repo-1", "inv-task")
	require.NoError(t, err)
	require.NoError(t, st.WriteInvocationMeta("repo-1", "inv-task", meta))

	record, exists, conflict, err := s.findInvocationByClientRequestID("repo-1", "shared-request", "agent-fingerprint")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.False(t, conflict)
	assert.Nil(t, record)
}

func TestControlPlaneStart_DurableIdempotencyDoesNotReplayIncompleteOrFailedInvocation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testutil.HermeticGitEnv(t)
	env := setupGitRepo(t)
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	writeTestUserConfig(t, configDir)
	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	createBody, err := json.Marshal(WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "direct-idempotency",
		BaseBranch: "main",
	})
	require.NoError(t, err)
	createReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(createBody))
	createW := httptest.NewRecorder()
	s.newHTTPHandler().ServeHTTP(createW, createReq)

	var createResp WorktreeCreateResponse
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &createResp))
	require.True(t, createResp.OK, "create worktree failed: %s %s", createResp.ErrorCode, createResp.Message)

	repoRoot := env.RepoPath
	if resolved, err := filepath.EvalSymlinks(repoRoot); err == nil {
		repoRoot = resolved
	}
	tests := []struct {
		name        string
		status      store.InvocationStatus
		failure     string
		wantMessage string
	}{
		{
			name:        "starting",
			status:      store.InvocationStatusStarting,
			wantMessage: "has not reached running state",
		},
		{
			name:        "failed",
			status:      store.InvocationStatusFailed,
			failure:     "runner failed before claim",
			wantMessage: "runner failed before claim",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := ControlPlaneStartRequest{
				ClientRequestID:  "direct-" + tc.name,
				RepoRoot:         env.RepoPath,
				WorktreeRef:      "direct-idempotency",
				Runner:           "claude-code",
				Prompt:           "test",
				ExecutionProfile: createResp.ExecutionProfile,
			}
			fingerprint := controlPlaneStartFingerprint(repoRoot, createResp.WorktreeID, createResp.CheckoutRoot, store.RunnerModeHeadless, request, nil)
			invocationID := "inv-direct-" + tc.name
			_, err := st.EnsureInvocationDir(createResp.RepoID, invocationID)
			require.NoError(t, err)
			meta := store.NewInvocationMeta(
				invocationID,
				"",
				createResp.WorktreeID,
				filepath.Join(createResp.CheckoutRoot, "sandboxes", invocationID),
				createResp.CheckoutRoot,
				createResp.ExecutionProfile,
				"agency/sandbox-"+invocationID,
				"abc123",
				"claude-code",
				store.RunnerModeHeadless,
				time.Now(),
			)
			meta.Status = tc.status
			meta.ClientRequestID = request.ClientRequestID
			meta.RequestFingerprint = fingerprint
			meta.FailureReason = tc.failure
			require.NoError(t, st.WriteInvocationMeta(createResp.RepoID, invocationID, meta))

			body, err := json.Marshal(request)
			require.NoError(t, err)
			httpReq := httptest.NewRequest(http.MethodPost, "/invocations/start_headless", bytes.NewReader(body))
			w := httptest.NewRecorder()
			s.newHTTPHandler().ServeHTTP(w, httpReq)

			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			var resp ControlPlaneStartResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.False(t, resp.OK)
			assert.Equal(t, string(errors.EInvocationStartFailed), resp.ErrorCode)
			assert.Contains(t, resp.Message, tc.wantMessage)
		})
	}

	request := ControlPlaneStartRequest{
		ClientRequestID:  "direct-finished",
		RepoRoot:         env.RepoPath,
		WorktreeRef:      "direct-idempotency",
		Runner:           "claude-code",
		Prompt:           "test",
		ExecutionProfile: createResp.ExecutionProfile,
	}
	fingerprint := controlPlaneStartFingerprint(repoRoot, createResp.WorktreeID, createResp.CheckoutRoot, store.RunnerModeHeadless, request, nil)
	_, err = st.EnsureInvocationDir(createResp.RepoID, "inv-direct-finished")
	require.NoError(t, err)
	meta := store.NewInvocationMeta(
		"inv-direct-finished",
		"",
		createResp.WorktreeID,
		filepath.Join(createResp.CheckoutRoot, "sandboxes", "inv-direct-finished"),
		createResp.CheckoutRoot,
		createResp.ExecutionProfile,
		"agency/sandbox-inv-direct-finished",
		"abc123",
		"claude-code",
		store.RunnerModeHeadless,
		time.Now(),
	)
	meta.Status = store.InvocationStatusFinished
	meta.ClientRequestID = request.ClientRequestID
	meta.RequestFingerprint = fingerprint
	meta.ClaimedAt = time.Now().UTC().Format(time.RFC3339)
	require.NoError(t, st.WriteInvocationMeta(createResp.RepoID, "inv-direct-finished", meta))
	require.NoError(t, st.UpdateIntegrationWorktreeMeta(createResp.RepoID, createResp.WorktreeID, func(meta *store.IntegrationWorktreeMeta) {
		meta.State = store.WorktreeStateArchived
	}))

	body, err := json.Marshal(request)
	require.NoError(t, err)
	httpReq := httptest.NewRequest(http.MethodPost, "/invocations/start_headless", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.newHTTPHandler().ServeHTTP(w, httpReq)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp ControlPlaneStartResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.OK)
	assert.True(t, resp.AlreadyRunning)
	assert.Equal(t, "inv-direct-finished", resp.InvocationID)
}

func TestControlPlaneStart_RunnerTargetSetPassesValidation(t *testing.T) {
	t.Parallel()

	runners := []string{
		"claude-code",
		"codex",
		"amp",
		"opencode",
		"cursor",
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
		Runner:      "claude-code",
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
		Runner:      "claude-code",
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
				Runner:          "claude-code",
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
	require.NoError(t, os.MkdirAll(filepath.Join(fakeWorktreePath, ".agency"), 0o755), "mkdir marker dir")
	require.NoError(t, os.WriteFile(filepath.Join(fakeWorktreePath, ".agency", "INTEGRATION_MARKER"), []byte("true\n"), 0o644), "write marker")

	req := ControlPlaneStartRequest{
		ClientRequestID: "test-uuid",
		RepoRoot:        fakeWorktreePath,
		WorktreeRef:     "wt-1",
		Runner:          "claude-code",
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
		Runner:          "claude-code",
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
	cfg := `{"version":4,"defaults":{"runner":"claude-code","editor":"code","execution_profile":"personal"},"runners":{"claude-code":"/nonexistent/path/to/runner"},"execution_profiles":{"personal":{"env":{}}}}`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(cfg), 0o644), "write config")

	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	// First create a worktree so the control plane can find it.
	createReq := WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "runner-test",
		BaseBranch: "main",
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
		Runner:          "claude-code",
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

func TestControlPlaneStart_RespectsRepoLock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testutil.HermeticGitEnv(t)

	env := setupGitRepo(t)
	tmpDir := t.TempDir()
	writeTestUserConfig(t, tmpDir)
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	createReq := WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "lock-test",
		BaseBranch: "main",
	}
	createBody, _ := json.Marshal(createReq)
	createHTTPReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(createBody))
	createW := httptest.NewRecorder()
	s.handleWorktreeCreate(createW, createHTTPReq)

	var createResp WorktreeCreateResponse
	require.NoError(t, json.NewDecoder(createW.Body).Decode(&createResp), "failed to decode worktree create response")
	require.True(t, createResp.OK, "create worktree failed: %s - %s", createResp.ErrorCode, createResp.Message)

	unlock, err := s.repoLock.Lock(createResp.RepoID, "test-lock-holder")
	require.NoError(t, err)
	t.Cleanup(func() { _ = unlock() })

	req := ControlPlaneStartRequest{
		ClientRequestID: "test-uuid-lock",
		RepoRoot:        env.RepoPath,
		WorktreeRef:     "lock-test",
		Runner:          "claude-code",
		Prompt:          "test",
	}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/invocations/start_headless", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleControlPlaneStartHeadless(w, httpReq)

	var resp ControlPlaneStartResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.ERepoLocked), resp.ErrorCode)
}

func TestControlPlaneStartHeaded_RespectsRepoLock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testutil.HermeticGitEnv(t)

	env := setupGitRepo(t)
	tmpDir := t.TempDir()
	writeTestUserConfig(t, tmpDir)
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	createReq := WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "lock-headed-test",
		BaseBranch: "main",
	}
	createBody, _ := json.Marshal(createReq)
	createHTTPReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(createBody))
	createW := httptest.NewRecorder()
	s.handleWorktreeCreate(createW, createHTTPReq)

	var createResp WorktreeCreateResponse
	require.NoError(t, json.NewDecoder(createW.Body).Decode(&createResp), "failed to decode worktree create response")
	require.True(t, createResp.OK, "create worktree failed: %s - %s", createResp.ErrorCode, createResp.Message)

	unlock, err := s.repoLock.Lock(createResp.RepoID, "test-lock-holder")
	require.NoError(t, err)
	t.Cleanup(func() { _ = unlock() })

	req := ControlPlaneStartHeadedRequest{
		ClientRequestID: "test-uuid-lock-headed",
		RepoRoot:        env.RepoPath,
		WorktreeRef:     "lock-headed-test",
		Runner:          "claude-code",
	}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/invocations/start_headed", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleControlPlaneStartHeaded(w, httpReq)

	var resp ControlPlaneStartHeadedResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.ERepoLocked), resp.ErrorCode)
}
