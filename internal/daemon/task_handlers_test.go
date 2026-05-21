package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestTaskStartValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		req      TaskStartRequest
		wantCode string
	}{
		{
			name: "missing client request id",
			req: TaskStartRequest{
				RepoRoot:   "/tmp/repo",
				Name:       "feature",
				BaseBranch: "main",
				Runner:     "claude-code",
				Prompt:     "do it",
			},
			wantCode: string(errors.EInvalidRequest),
		},
		{
			name: "headless prompt required",
			req: TaskStartRequest{
				ClientRequestID: "req-1",
				RepoRoot:        "/tmp/repo",
				Name:            "feature",
				BaseBranch:      "main",
				Runner:          "claude-code",
			},
			wantCode: string(errors.EPromptRequired),
		},
		{
			name: "headed prompt rejected",
			req: TaskStartRequest{
				ClientRequestID: "req-1",
				RepoRoot:        "/tmp/repo",
				Name:            "feature",
				BaseBranch:      "main",
				Mode:            "headed",
				Runner:          "claude-code",
				Prompt:          "do it",
			},
			wantCode: string(errors.EUsage),
		},
		{
			name: "invalid mode",
			req: TaskStartRequest{
				ClientRequestID: "req-1",
				RepoRoot:        "/tmp/repo",
				Name:            "feature",
				BaseBranch:      "main",
				Mode:            "bogus",
				Runner:          "claude-code",
				Prompt:          "do it",
			},
			wantCode: string(errors.EInvalidArgument),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := store.NewStore(fs.NewRealFS(), t.TempDir(), time.Now)
			srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), t.TempDir())
			body, err := json.Marshal(tc.req)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/tasks/start", bytes.NewReader(body))
			w := httptest.NewRecorder()

			srv.newHTTPHandler().ServeHTTP(w, req)

			assert.NotEqual(t, http.StatusOK, w.Code)
			var payload TaskStartResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
			assert.False(t, payload.OK)
			assert.Equal(t, tc.wantCode, payload.ErrorCode)
			assert.NotEmpty(t, payload.RequestID)
		})
	}
}

func TestTaskStartInvalidBodyReturnsInvalidRequest(t *testing.T) {
	t.Parallel()

	st := store.NewStore(fs.NewRealFS(), t.TempDir(), time.Now)
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/tasks/start", strings.NewReader(`{"client_request_id":"req-1","unknown":true}`))
	w := httptest.NewRecorder()

	srv.newHTTPHandler().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var payload TaskStartResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.False(t, payload.OK)
	assert.Equal(t, string(errors.EInvalidRequest), payload.ErrorCode)
}

func TestTaskMutationRequestShapeErrorsReturnInvalidRequest(t *testing.T) {
	t.Parallel()

	st := store.NewStore(fs.NewRealFS(), t.TempDir(), time.Now)
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), t.TempDir())
	tests := []struct {
		name        string
		method      string
		path        string
		body        string
		wantMessage string
	}{
		{name: "archive missing repo", method: http.MethodPost, path: "/tasks/task-1/archive"},
		{name: "retry missing repo", method: http.MethodPost, path: "/tasks/task-1/retry", body: `{"client_request_id":"retry-1","prompt":"again"}`},
		{
			name:        "retry invalid body",
			method:      http.MethodPost,
			path:        "/tasks/task-1/retry?repo_id=repo-1",
			body:        `{"client_request_id":"retry-1","unknown":true}`,
			wantMessage: `invalid request body: unknown field "unknown"`,
		},
		{name: "retry missing client request id", method: http.MethodPost, path: "/tasks/task-1/retry?repo_id=repo-1", body: `{"prompt":"again"}`},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			srv.newHTTPHandler().ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code)
			var payload TaskStartResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
			assert.False(t, payload.OK)
			assert.Equal(t, string(errors.EInvalidRequest), payload.ErrorCode)
			if tt.wantMessage != "" {
				assert.Equal(t, tt.wantMessage, payload.Message)
			}
		})
	}
}

func TestStartRequestsRejectNullEnv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		dst  any
	}{
		{name: "control plane env null", body: `{"env":null}`, dst: &ControlPlaneStartRequest{}},
		{name: "control plane env value null", body: `{"env":{"TOKEN":null}}`, dst: &ControlPlaneStartRequest{}},
		{name: "task start env null", body: `{"env":null}`, dst: &TaskStartRequest{}},
		{name: "task retry env value null", body: `{"env":{"TOKEN":null}}`, dst: &TaskRetryRequest{}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := json.Unmarshal([]byte(tt.body), tt.dst)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "env")
		})
	}
}

func TestStartRequestsRejectInvalidEnvKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		dst  any
	}{
		{name: "control plane env empty key", body: `{"env":{"":"value"}}`, dst: &ControlPlaneStartRequest{}},
		{name: "control plane env equals key", body: `{"env":{"BAD=KEY":"value"}}`, dst: &ControlPlaneStartRequest{}},
		{name: "task start env empty key", body: `{"env":{"":"value"}}`, dst: &TaskStartRequest{}},
		{name: "task retry env equals key", body: `{"env":{"BAD=KEY":"value"}}`, dst: &TaskRetryRequest{}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := json.Unmarshal([]byte(tt.body), tt.dst)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "env keys must be non-empty and must not contain '='")
		})
	}
}

func TestTaskRetryIdempotentDuplicate(t *testing.T) {
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	writeTestUserConfig(t, configDir)
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	repoRoot := t.TempDir()
	writeTestAgencyConfig(t, repoRoot)
	require.NoError(t, seedTaskRetryRecord(st, "repo-1", repoRoot, "task-1", "inv-1", "retry-1", "same prompt"))

	body := []byte(`{"client_request_id":"retry-1","prompt":"same prompt"}`)
	req := httptest.NewRequest(http.MethodPost, "/tasks/task-1/retry?repo_id=repo-1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.newHTTPHandler().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var payload TaskStartResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.True(t, payload.OK)
	assert.True(t, payload.Duplicate)
	assert.Equal(t, "task-1", payload.TaskID)
	assert.Equal(t, "inv-1", payload.InvocationID)
}

func TestTaskRetryIdempotentConflict(t *testing.T) {
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	writeTestUserConfig(t, configDir)
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	repoRoot := t.TempDir()
	writeTestAgencyConfig(t, repoRoot)
	require.NoError(t, seedTaskRetryRecord(st, "repo-1", repoRoot, "task-1", "inv-1", "retry-1", "original prompt"))

	body := []byte(`{"client_request_id":"retry-1","prompt":"different prompt"}`)
	req := httptest.NewRequest(http.MethodPost, "/tasks/task-1/retry?repo_id=repo-1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.newHTTPHandler().ServeHTTP(w, req)

	require.Equal(t, http.StatusConflict, w.Code)
	var payload TaskStartResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.False(t, payload.OK)
	assert.Equal(t, string(errors.ETaskFingerprintConflict), payload.ErrorCode)
}

func TestTaskStartIdempotentEmptyStartingReservationFailsClosed(t *testing.T) {
	dataDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), filepath.Join(dataDir, "config"))

	repoRoot := t.TempDir()
	checkoutRoot := t.TempDir()
	req := TaskStartRequest{
		ClientRequestID:  "start-1",
		RepoRoot:         repoRoot,
		Name:             "feature",
		BaseBranch:       "main",
		Mode:             string(store.RunnerModeHeadless),
		Runner:           "claude-code",
		Prompt:           "start prompt",
		ExecutionProfile: "personal",
		CheckoutRoot:     checkoutRoot,
	}
	fingerprint := taskStartFingerprint(repoRoot, checkoutRoot, req, nil)
	require.NoError(t, seedStartingTaskReservation(st, "repo-1", repoRoot, checkoutRoot, "task-1", req.ClientRequestID, fingerprint))

	w := httptest.NewRecorder()
	handled := srv.writeTaskStartIdempotencyResult(w, "request-1", req.ClientRequestID, "repo-1", fingerprint, true)

	require.True(t, handled)
	require.Equal(t, http.StatusConflict, w.Code)
	var payload TaskStartResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.False(t, payload.OK)
	assert.Equal(t, string(errors.ETaskCreateFailed), payload.ErrorCode)
	assert.Equal(t, store.TaskStateFailed, payload.State)

	meta, err := st.ReadTaskMeta("repo-1", "task-1")
	require.NoError(t, err)
	assert.Equal(t, store.TaskStateFailed, meta.State)
	assert.Equal(t, "task_start_incomplete", meta.FailedPhase)
	assert.Empty(t, meta.PrimaryInvocationID)
}

func TestTaskStartIdempotentStartingReservationRepairsClaimedInvocation(t *testing.T) {
	dataDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), filepath.Join(dataDir, "config"))

	repoRoot := t.TempDir()
	checkoutRoot := t.TempDir()
	req := TaskStartRequest{
		ClientRequestID:  "start-1",
		RepoRoot:         repoRoot,
		Name:             "feature",
		BaseBranch:       "main",
		Mode:             string(store.RunnerModeHeadless),
		Runner:           "claude-code",
		Prompt:           "start prompt",
		ExecutionProfile: "personal",
		CheckoutRoot:     checkoutRoot,
	}
	fingerprint := taskStartFingerprint(repoRoot, checkoutRoot, req, nil)
	require.NoError(t, seedStartingTaskReservation(st, "repo-1", repoRoot, checkoutRoot, "task-1", req.ClientRequestID, fingerprint))
	require.NoError(t, st.UpdateTaskMeta("repo-1", "task-1", func(meta *store.TaskMeta) {
		meta.WorktreeID = "wt-1"
	}))
	require.NoError(t, seedClaimedTaskInvocation(st, "repo-1", "task-1", "wt-1", "inv-1", req.ClientRequestID, fingerprint, checkoutRoot))

	beforeLockW := httptest.NewRecorder()
	handledBeforeLock := srv.writeTaskStartIdempotencyResult(beforeLockW, "request-before-lock", req.ClientRequestID, "repo-1", fingerprint, false)
	assert.False(t, handledBeforeLock)
	metaBeforeLock, err := st.ReadTaskMeta("repo-1", "task-1")
	require.NoError(t, err)
	assert.Equal(t, store.TaskStateStarting, metaBeforeLock.State)
	assert.Empty(t, metaBeforeLock.PrimaryInvocationID)

	w := httptest.NewRecorder()
	handled := srv.writeTaskStartIdempotencyResult(w, "request-1", req.ClientRequestID, "repo-1", fingerprint, true)

	require.True(t, handled)
	require.Equal(t, http.StatusOK, w.Code)
	var payload TaskStartResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.True(t, payload.OK)
	assert.True(t, payload.Duplicate)
	assert.Equal(t, "inv-1", payload.InvocationID)
	assert.Equal(t, store.TaskStateRunning, payload.State)

	meta, err := st.ReadTaskMeta("repo-1", "task-1")
	require.NoError(t, err)
	assert.Equal(t, store.TaskStateRunning, meta.State)
	assert.Equal(t, "inv-1", meta.PrimaryInvocationID)
}

func TestTaskRetryEmptyStartingReservationFailsClosed(t *testing.T) {
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	writeTestUserConfig(t, configDir)
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	repoRoot := t.TempDir()
	writeTestAgencyConfig(t, repoRoot)
	require.NoError(t, seedStartingTaskRetryReservation(st, "repo-1", repoRoot, "task-1", "retry-1", "same prompt"))

	body := []byte(`{"client_request_id":"retry-1","prompt":"same prompt"}`)
	req := httptest.NewRequest(http.MethodPost, "/tasks/task-1/retry?repo_id=repo-1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.newHTTPHandler().ServeHTTP(w, req)

	require.Equal(t, http.StatusConflict, w.Code)
	var payload TaskStartResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.False(t, payload.OK)
	assert.Equal(t, string(errors.ETaskCreateFailed), payload.ErrorCode)

	meta, err := st.ReadTaskMeta("repo-1", "task-1")
	require.NoError(t, err)
	record := meta.RetryRequests["retry-1"]
	assert.Equal(t, store.TaskRetryStateFailed, record.State)
	assert.Equal(t, string(errors.ETaskCreateFailed), record.ErrorCode)
	assert.Empty(t, record.InvocationID)
}

func TestTaskRetryStartingReservationRepairsClaimedInvocation(t *testing.T) {
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	writeTestUserConfig(t, configDir)
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	repoRoot := t.TempDir()
	writeTestAgencyConfig(t, repoRoot)
	require.NoError(t, seedStartingTaskRetryReservation(st, "repo-1", repoRoot, "task-1", "retry-1", "same prompt"))
	meta, err := st.ReadTaskMeta("repo-1", "task-1")
	require.NoError(t, err)
	retryRecord := meta.RetryRequests["retry-1"]
	require.NoError(t, seedClaimedTaskInvocation(st, "repo-1", "task-1", "wt-1", "inv-retry", "retry-1", retryRecord.RequestFingerprint, meta.CheckoutRoot))

	beforeLockW := httptest.NewRecorder()
	handledBeforeLock := srv.writeTaskRetryIdempotencyResult(beforeLockW, "request-before-lock", meta, "retry-1", retryRecord.RequestFingerprint, false)
	assert.False(t, handledBeforeLock)
	metaBeforeLock, err := st.ReadTaskMeta("repo-1", "task-1")
	require.NoError(t, err)
	assert.Equal(t, store.TaskStateRunning, metaBeforeLock.State)
	assert.Empty(t, metaBeforeLock.PrimaryInvocationID)
	assert.Equal(t, store.TaskRetryStateStarting, metaBeforeLock.RetryRequests["retry-1"].State)

	body := []byte(`{"client_request_id":"retry-1","prompt":"same prompt"}`)
	req := httptest.NewRequest(http.MethodPost, "/tasks/task-1/retry?repo_id=repo-1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.newHTTPHandler().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var payload TaskStartResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.True(t, payload.OK)
	assert.True(t, payload.Duplicate)
	assert.Equal(t, "inv-retry", payload.InvocationID)

	meta, err = st.ReadTaskMeta("repo-1", "task-1")
	require.NoError(t, err)
	assert.Equal(t, "inv-retry", meta.PrimaryInvocationID)
	assert.Equal(t, store.TaskRetryStateRunning, meta.RetryRequests["retry-1"].State)
	assert.Equal(t, "inv-retry", meta.RetryRequests["retry-1"].InvocationID)
}

func TestTaskStartRepairDoesNotDuplicatePrewrittenRunningEvent(t *testing.T) {
	dataDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), filepath.Join(dataDir, "config"))

	repoRoot := t.TempDir()
	checkoutRoot := t.TempDir()
	req := TaskStartRequest{
		ClientRequestID:  "start-1",
		RepoRoot:         repoRoot,
		Name:             "feature",
		BaseBranch:       "main",
		Mode:             string(store.RunnerModeHeadless),
		Runner:           "claude-code",
		Prompt:           "start prompt",
		ExecutionProfile: "personal",
		CheckoutRoot:     checkoutRoot,
	}
	fingerprint := taskStartFingerprint(repoRoot, checkoutRoot, req, nil)
	require.NoError(t, seedStartingTaskReservation(st, "repo-1", repoRoot, checkoutRoot, "task-1", req.ClientRequestID, fingerprint))
	require.NoError(t, st.UpdateTaskMeta("repo-1", "task-1", func(meta *store.TaskMeta) {
		meta.WorktreeID = "wt-1"
	}))
	require.NoError(t, seedClaimedTaskInvocation(st, "repo-1", "task-1", "wt-1", "inv-1", req.ClientRequestID, fingerprint, checkoutRoot))
	require.NoError(t, srv.appendTaskEventOnceByInvocationID("repo-1", "task-1", "agency.task_running", "inv-1", map[string]any{
		"invocation_id": "inv-1",
		"worktree_id":   "wt-1",
	}))

	repaired, err := srv.repairTaskStartFromClaimedInvocation("repo-1", &store.TaskMeta{TaskID: "task-1"}, req.ClientRequestID, fingerprint)

	require.NoError(t, err)
	require.NotNil(t, repaired)
	assert.Equal(t, store.TaskStateRunning, repaired.State)
	assert.Equal(t, "inv-1", repaired.PrimaryInvocationID)
	assert.Equal(t, 1, countTaskEventsByInvocationID(t, st.TaskEventsPath("repo-1", "task-1"), "agency.task_running", "inv-1"))
}

func TestTaskRetryRepairDoesNotDuplicatePrewrittenRetriedEvent(t *testing.T) {
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	writeTestUserConfig(t, configDir)
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	repoRoot := t.TempDir()
	writeTestAgencyConfig(t, repoRoot)
	require.NoError(t, seedStartingTaskRetryReservation(st, "repo-1", repoRoot, "task-1", "retry-1", "same prompt"))
	meta, err := st.ReadTaskMeta("repo-1", "task-1")
	require.NoError(t, err)
	retryRecord := meta.RetryRequests["retry-1"]
	require.NoError(t, seedClaimedTaskInvocation(st, "repo-1", "task-1", "wt-1", "inv-retry", "retry-1", retryRecord.RequestFingerprint, meta.CheckoutRoot))
	require.NoError(t, srv.appendTaskEventOnceByInvocationID("repo-1", "task-1", "agency.task_retried", "inv-retry", map[string]any{
		"invocation_id":     "inv-retry",
		"checkout_root":     meta.CheckoutRoot,
		"execution_profile": meta.ExecutionProfile,
	}))

	repaired, err := srv.repairTaskRetryFromClaimedInvocation("repo-1", meta, "retry-1", retryRecord.RequestFingerprint)

	require.NoError(t, err)
	require.NotNil(t, repaired)
	assert.Equal(t, "inv-retry", repaired.PrimaryInvocationID)
	assert.Equal(t, store.TaskRetryStateRunning, repaired.RetryRequests["retry-1"].State)
	assert.Equal(t, 1, countTaskEventsByInvocationID(t, st.TaskEventsPath("repo-1", "task-1"), "agency.task_retried", "inv-retry"))
}

func TestTaskRetryEventAppendFailurePreservesExistingTask(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testutil.HermeticGitEnv(t)
	env := setupGitRepo(t)
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	writeTestUserConfig(t, configDir)
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)
	// The retry spawns a real runner; synchronously drain the server before
	// t.TempDir() cleanup so its supervision goroutines and runner child
	// process stop writing into dataDir before RemoveAll runs.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	createBody, err := json.Marshal(WorktreeCreateRequest{
		RepoRoot:   env.RepoPath,
		Name:       "retry-event-failure",
		BaseBranch: "main",
	})
	require.NoError(t, err)
	createReq := httptest.NewRequest(http.MethodPost, "/worktrees/create", bytes.NewReader(createBody))
	createW := httptest.NewRecorder()
	srv.newHTTPHandler().ServeHTTP(createW, createReq)

	var createResp WorktreeCreateResponse
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &createResp))
	require.True(t, createResp.OK, "create worktree failed: %s %s", createResp.ErrorCode, createResp.Message)

	taskID := "task-event-failure"
	_, err = st.EnsureTaskDir(createResp.RepoID, taskID)
	require.NoError(t, err)
	now := st.Now().UTC().Format(time.RFC3339)
	require.NoError(t, st.WriteTaskMeta(createResp.RepoID, taskID, &store.TaskMeta{
		SchemaVersion:       store.SchemaVersion,
		TaskID:              taskID,
		Name:                "feature",
		State:               store.TaskStateRunning,
		RepoID:              createResp.RepoID,
		RepoRoot:            env.RepoPath,
		BaseBranch:          "main",
		CheckoutRoot:        createResp.CheckoutRoot,
		ExecutionProfile:    createResp.ExecutionProfile,
		WorktreeID:          createResp.WorktreeID,
		WorktreeName:        "retry-event-failure",
		WorktreePath:        createResp.TreePath,
		Branch:              createResp.Branch,
		PrimaryInvocationID: "inv-original",
		Mode:                store.RunnerModeHeadless,
		Runner:              "claude-code",
		ClientRequestID:     "start-1",
		RequestFingerprint:  "start-fp",
		CreatedAt:           now,
		UpdatedAt:           now,
	}))
	require.NoError(t, os.MkdirAll(st.TaskEventsPath(createResp.RepoID, taskID), 0o700))

	body := []byte(`{"client_request_id":"retry-event-fail","mode":"headless","runner":"claude-code","prompt":"retry prompt","env":{"FAKE_RUNNER_MODE":"sleep"}}`)
	req := httptest.NewRequest(http.MethodPost, "/tasks/"+taskID+"/retry?repo_id="+createResp.RepoID, bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.newHTTPHandler().ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	var payload TaskStartResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.False(t, payload.OK)
	assert.Equal(t, string(errors.EPersistFailed), payload.ErrorCode)
	assert.Equal(t, store.TaskStateRunning, payload.State)
	assert.Equal(t, "inv-original", payload.InvocationID)

	meta, err := st.ReadTaskMeta(createResp.RepoID, taskID)
	require.NoError(t, err)
	assert.Equal(t, store.TaskStateRunning, meta.State)
	assert.Equal(t, "inv-original", meta.PrimaryInvocationID)
	retryRecord := meta.RetryRequests["retry-event-fail"]
	assert.Equal(t, store.TaskRetryStateFailed, retryRecord.State)
	assert.Equal(t, string(errors.EPersistFailed), retryRecord.ErrorCode)
	assert.Empty(t, retryRecord.InvocationID)

	records, err := store.ScanInvocationsForRepo(st.DataDir, createResp.RepoID)
	require.NoError(t, err)
	var retryInvocation *store.InvocationMeta
	for i := range records {
		if records[i].Meta != nil && records[i].Meta.ClientRequestID == "retry-event-fail" {
			retryInvocation = records[i].Meta
			break
		}
	}
	require.NotNil(t, retryInvocation)
	assert.Equal(t, store.InvocationStatusFailed, retryInvocation.Status)
	assert.Equal(t, "task_retry_event_failed", retryInvocation.FailureReason)
}

func countTaskEventsByInvocationID(t *testing.T, eventsPath, kind, invocationID string) int {
	t.Helper()

	data, err := os.ReadFile(eventsPath)
	require.NoError(t, err)
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event struct {
			Kind string         `json:"kind"`
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		if event.Kind == kind && event.Data["invocation_id"] == invocationID {
			count++
		}
	}
	return count
}

func TestAbortStartedTaskInvocation_HeadedKillFailureLeavesInvocationInspectable(t *testing.T) {
	dataDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), filepath.Join(dataDir, "config"))
	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.KillSessionErr = fmt.Errorf("tmux unavailable")
	fakeTmux.Sessions["session-1"] = testutil.FakeTmuxSession{Name: "session-1"}
	srv.TmuxClient = fakeTmux

	require.NoError(t, seedClaimedTaskInvocation(st, "repo-1", "task-1", "wt-1", "inv-headed", "start-1", "fingerprint-1", t.TempDir()))
	require.NoError(t, st.UpdateInvocationMeta("repo-1", "inv-headed", func(meta *store.InvocationMeta) {
		meta.Mode = store.RunnerModeHeaded
		meta.TmuxSession = "session-1"
		meta.PID = nil
		meta.PGID = nil
	}))
	meta, err := st.ReadInvocationMeta("repo-1", "inv-headed")
	require.NoError(t, err)

	srv.abortStartedTaskInvocation("repo-1", meta, "task_event_running_failed")

	meta, err = st.ReadInvocationMeta("repo-1", "inv-headed")
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusRunning, meta.Status)
	assert.True(t, meta.Flags.NeedsAttention)
	assert.Equal(t, "task_event_running_failed", meta.FailureReason)

	fakeTmux.Mu.Lock()
	_, sessionStillExists := fakeTmux.Sessions["session-1"]
	fakeTmux.Mu.Unlock()
	assert.True(t, sessionStillExists)
}

func TestFindClaimedTaskInvocationSkipsTaskAbortRunningInvocation(t *testing.T) {
	dataDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), filepath.Join(dataDir, "config"))

	require.NoError(t, seedClaimedTaskInvocation(st, "repo-1", "task-1", "wt-1", "inv-1", "retry-1", "fingerprint-1", t.TempDir()))
	require.NoError(t, st.UpdateInvocationMeta("repo-1", "inv-1", func(meta *store.InvocationMeta) {
		meta.FailureReason = "task_retry_event_failed"
	}))

	meta, ok, err := srv.findClaimedTaskInvocation("repo-1", "task-1", "retry-1", "fingerprint-1")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, meta)
}

func TestTaskRetryDoesNotFallbackToPersistedProfileBeforeConfigResolution(t *testing.T) {
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	writeTestUserConfig(t, configDir)
	st := store.NewStore(fs.NewRealFS(), dataDir, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	repoRoot := t.TempDir()
	writeTestAgencyConfigWithExecution(t, repoRoot, "missing", "repo-sibling")
	require.NoError(t, seedTaskForRetry(st, "repo-1", repoRoot, "task-1", "personal"))

	body := []byte(`{"client_request_id":"retry-1","prompt":"retry prompt"}`)
	req := httptest.NewRequest(http.MethodPost, "/tasks/task-1/retry?repo_id=repo-1", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.newHTTPHandler().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var payload TaskStartResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.False(t, payload.OK)
	assert.Equal(t, string(errors.EExecutionProfileNotFound), payload.ErrorCode)
}

func TestTaskRetryFingerprintUsesResolvedCheckoutRoot(t *testing.T) {
	meta := &store.TaskMeta{
		TaskID:       "task-1",
		WorktreeID:   "wt-1",
		CheckoutRoot: "/persisted-checkout",
	}
	req := TaskRetryRequest{
		ExecutionProfile: "personal",
		CheckoutRoot:     "/resolved-checkout-a",
		Prompt:           "retry prompt",
	}

	first := taskRetryFingerprint(meta, string(store.RunnerModeHeadless), "claude-code", req, req.Env)
	req.CheckoutRoot = "/resolved-checkout-b"
	second := taskRetryFingerprint(meta, string(store.RunnerModeHeadless), "claude-code", req, req.Env)

	assert.NotEqual(t, first, second)
}

func TestTaskStartFingerprintIgnoresProfileEnvValues(t *testing.T) {
	req := TaskStartRequest{
		RepoRoot:         "/repo",
		Name:             "feature",
		BaseBranch:       "main",
		Mode:             string(store.RunnerModeHeadless),
		Runner:           "claude-code",
		Prompt:           "start prompt",
		ExecutionProfile: "personal",
	}
	requestEnv := map[string]string{"REQUEST_TOKEN": "alpha"}
	req.Env = envForLaunch(map[string]string{"PROFILE_TOKEN": "old"}, requestEnv)
	first := taskStartFingerprint("/repo", "/checkout", req, requestEnv)

	req.Env = envForLaunch(map[string]string{"PROFILE_TOKEN": "new"}, requestEnv)
	second := taskStartFingerprint("/repo", "/checkout", req, requestEnv)
	assert.Equal(t, first, second)

	changedRequestEnv := map[string]string{"REQUEST_TOKEN": "beta"}
	req.Env = envForLaunch(map[string]string{"PROFILE_TOKEN": "new"}, changedRequestEnv)
	third := taskStartFingerprint("/repo", "/checkout", req, changedRequestEnv)
	assert.Equal(t, first, third)

	changedRequestEnv = map[string]string{"OTHER_REQUEST_TOKEN": "beta"}
	req.Env = envForLaunch(map[string]string{"PROFILE_TOKEN": "new"}, changedRequestEnv)
	fourth := taskStartFingerprint("/repo", "/checkout", req, changedRequestEnv)
	assert.NotEqual(t, first, fourth)
}

func TestTaskRetryFingerprintIgnoresProfileEnvValues(t *testing.T) {
	meta := &store.TaskMeta{
		TaskID:     "task-1",
		WorktreeID: "wt-1",
	}
	req := TaskRetryRequest{
		ExecutionProfile: "personal",
		CheckoutRoot:     "/checkout",
		Prompt:           "retry prompt",
	}
	requestEnv := map[string]string{"REQUEST_TOKEN": "alpha"}
	req.Env = envForLaunch(map[string]string{"PROFILE_TOKEN": "old"}, requestEnv)
	first := taskRetryFingerprint(meta, string(store.RunnerModeHeadless), "claude-code", req, requestEnv)

	req.Env = envForLaunch(map[string]string{"PROFILE_TOKEN": "new"}, requestEnv)
	second := taskRetryFingerprint(meta, string(store.RunnerModeHeadless), "claude-code", req, requestEnv)
	assert.Equal(t, first, second)

	changedRequestEnv := map[string]string{"REQUEST_TOKEN": "beta"}
	req.Env = envForLaunch(map[string]string{"PROFILE_TOKEN": "new"}, changedRequestEnv)
	third := taskRetryFingerprint(meta, string(store.RunnerModeHeadless), "claude-code", req, changedRequestEnv)
	assert.Equal(t, first, third)

	changedRequestEnv = map[string]string{"OTHER_REQUEST_TOKEN": "beta"}
	req.Env = envForLaunch(map[string]string{"PROFILE_TOKEN": "new"}, changedRequestEnv)
	fourth := taskRetryFingerprint(meta, string(store.RunnerModeHeadless), "claude-code", req, changedRequestEnv)
	assert.NotEqual(t, first, fourth)
}

func seedTaskForRetry(st *store.Store, repoID, repoRoot, taskID, executionProfile string) error {
	if _, err := st.EnsureTaskDir(repoID, taskID); err != nil {
		return err
	}
	checkoutRoot, err := config.ResolveCheckoutRoot(repoRoot, repoID, "repo-sibling")
	if err != nil {
		return err
	}
	now := st.Now().UTC().Format(time.RFC3339)
	meta := &store.TaskMeta{
		SchemaVersion:      store.SchemaVersion,
		TaskID:             taskID,
		Name:               "feature",
		State:              store.TaskStateStarting,
		RepoID:             repoID,
		RepoRoot:           repoRoot,
		BaseBranch:         "main",
		CheckoutRoot:       checkoutRoot,
		ExecutionProfile:   executionProfile,
		Mode:               store.RunnerModeHeadless,
		Runner:             "claude-code",
		ClientRequestID:    "start-1",
		RequestFingerprint: "start-fp",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	meta.State = store.TaskStateRunning
	meta.WorktreeID = "wt-1"
	return st.WriteTaskMeta(repoID, taskID, meta)
}

func seedStartingTaskReservation(st *store.Store, repoID, repoRoot, checkoutRoot, taskID, clientRequestID, fingerprint string) error {
	if _, err := st.EnsureTaskDir(repoID, taskID); err != nil {
		return err
	}
	now := st.Now().UTC().Format(time.RFC3339)
	meta := &store.TaskMeta{
		SchemaVersion:      store.SchemaVersion,
		TaskID:             taskID,
		Name:               "feature",
		State:              store.TaskStateStarting,
		RepoID:             repoID,
		RepoRoot:           repoRoot,
		BaseBranch:         "main",
		CheckoutRoot:       checkoutRoot,
		ExecutionProfile:   "personal",
		Mode:               store.RunnerModeHeadless,
		Runner:             "claude-code",
		ClientRequestID:    clientRequestID,
		RequestFingerprint: fingerprint,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	return st.WriteTaskMeta(repoID, taskID, meta)
}

func seedClaimedTaskInvocation(st *store.Store, repoID, taskID, worktreeID, invocationID, clientRequestID, fingerprint, checkoutRoot string) error {
	if _, err := st.EnsureInvocationDir(repoID, invocationID); err != nil {
		return err
	}
	sandboxPath := filepath.Join(checkoutRoot, "sandboxes", invocationID)
	meta := store.NewInvocationMeta(
		invocationID,
		"",
		worktreeID,
		sandboxPath,
		checkoutRoot,
		"personal",
		"agency/sandbox-"+invocationID,
		"abc123",
		"claude-code",
		store.RunnerModeHeadless,
		st.Now(),
	)
	meta.Status = store.InvocationStatusRunning
	meta.TaskID = taskID
	meta.ClientRequestID = clientRequestID
	meta.RequestFingerprint = fingerprint
	return st.WriteInvocationMeta(repoID, invocationID, meta)
}

func seedTaskRetryRecord(st *store.Store, repoID, repoRoot, taskID, invocationID, clientRequestID, prompt string) error {
	if _, err := st.EnsureTaskDir(repoID, taskID); err != nil {
		return err
	}
	checkoutRoot, err := config.ResolveCheckoutRoot(repoRoot, repoID, "repo-sibling")
	if err != nil {
		return err
	}
	now := st.Now().UTC().Format(time.RFC3339)
	meta := &store.TaskMeta{
		SchemaVersion:      store.SchemaVersion,
		TaskID:             taskID,
		Name:               "feature",
		State:              store.TaskStateStarting,
		RepoID:             repoID,
		RepoRoot:           repoRoot,
		BaseBranch:         "main",
		CheckoutRoot:       checkoutRoot,
		ExecutionProfile:   "personal",
		Mode:               store.RunnerModeHeadless,
		Runner:             "claude-code",
		ClientRequestID:    "start-1",
		RequestFingerprint: "start-fp",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	meta.State = store.TaskStateRunning
	meta.WorktreeID = "wt-1"
	meta.PrimaryInvocationID = invocationID
	req := TaskRetryRequest{
		Mode:             string(store.RunnerModeHeadless),
		Runner:           "claude-code",
		Prompt:           prompt,
		ClientRequestID:  clientRequestID,
		ExecutionProfile: "personal",
		CheckoutRoot:     meta.CheckoutRoot,
	}
	meta.RetryRequests = map[string]store.TaskRetryRecord{
		clientRequestID: {
			RequestFingerprint: taskRetryFingerprint(meta, req.Mode, req.Runner, req, req.Env),
			InvocationID:       invocationID,
			State:              store.TaskRetryStateRunning,
			CreatedAt:          meta.CreatedAt,
			UpdatedAt:          meta.UpdatedAt,
		},
	}
	if err := st.WriteTaskMeta(repoID, taskID, meta); err != nil {
		return err
	}
	if _, err := st.EnsureInvocationDir(repoID, invocationID); err != nil {
		return err
	}
	invMeta := store.NewInvocationMeta(invocationID, "", "wt-1", "/sandbox", meta.CheckoutRoot, "personal", "agency/sandbox-"+invocationID, "abc123", "claude-code", store.RunnerModeHeadless, st.Now())
	invMeta.Status = store.InvocationStatusRunning
	invMeta.TaskID = taskID
	return st.WriteInvocationMeta(repoID, invocationID, invMeta)
}

func seedStartingTaskRetryReservation(st *store.Store, repoID, repoRoot, taskID, clientRequestID, prompt string) error {
	if _, err := st.EnsureTaskDir(repoID, taskID); err != nil {
		return err
	}
	checkoutRoot, err := config.ResolveCheckoutRoot(repoRoot, repoID, "repo-sibling")
	if err != nil {
		return err
	}
	now := st.Now().UTC().Format(time.RFC3339)
	meta := &store.TaskMeta{
		SchemaVersion:      store.SchemaVersion,
		TaskID:             taskID,
		Name:               "feature",
		State:              store.TaskStateRunning,
		RepoID:             repoID,
		RepoRoot:           repoRoot,
		BaseBranch:         "main",
		CheckoutRoot:       checkoutRoot,
		ExecutionProfile:   "personal",
		WorktreeID:         "wt-1",
		Mode:               store.RunnerModeHeadless,
		Runner:             "claude-code",
		ClientRequestID:    "start-1",
		RequestFingerprint: "start-fp",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	req := TaskRetryRequest{
		Mode:             string(store.RunnerModeHeadless),
		Runner:           "claude-code",
		Prompt:           prompt,
		ClientRequestID:  clientRequestID,
		ExecutionProfile: "personal",
		CheckoutRoot:     meta.CheckoutRoot,
	}
	meta.RetryRequests = map[string]store.TaskRetryRecord{
		clientRequestID: {
			RequestFingerprint: taskRetryFingerprint(meta, req.Mode, req.Runner, req, req.Env),
			State:              store.TaskRetryStateStarting,
			CreatedAt:          meta.CreatedAt,
			UpdatedAt:          meta.UpdatedAt,
		},
	}
	return st.WriteTaskMeta(repoID, taskID, meta)
}

func writeTestAgencyConfigWithExecution(t *testing.T, repoRoot, profile, checkoutRoot string) {
	t.Helper()
	scriptsDir := filepath.Join(repoRoot, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0o755))
	for _, script := range []string{"setup", "verify", "archive"} {
		require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, script+".sh"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"), 0o755))
	}
	cfg := map[string]any{
		"version": 4,
		"scripts": map[string]any{
			"setup":   map[string]string{"path": "scripts/setup.sh", "timeout": "10m"},
			"verify":  map[string]string{"path": "scripts/verify.sh", "timeout": "30m"},
			"archive": map[string]string{"path": "scripts/archive.sh", "timeout": "5m"},
		},
		"execution": map[string]string{
			"profile":       profile,
			"checkout_root": checkoutRoot,
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "agency.json"), data, 0o644))
}
