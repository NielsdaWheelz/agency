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

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
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
			wantCode: string(errors.EInvalidArgument),
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

func TestDecodeStrictJSONRejectsUnknownFieldsAndTrailingObjects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		dst  any
		want string
	}{
		{
			name: "custom request unknown field",
			body: `{"client_request_id":"req-1","unknown":true}`,
			dst:  &ControlPlaneStartRequest{},
			want: `unknown field "unknown"`,
		},
		{
			name: "trailing object",
			body: `{"client_request_id":"req-1"} {}`,
			dst:  &TaskRetryRequest{},
			want: "expected a single JSON object",
		},
		{
			name: "required null",
			body: `null`,
			dst:  &TaskRetryRequest{},
			want: "expected a JSON object",
		},
		{
			name: "optional empty",
			body: ``,
			dst:  &struct{}{},
		},
		{
			name: "optional null",
			body: `null`,
			dst:  &struct{}{},
			want: "expected a JSON object",
		},
		{
			name: "optional trailing object",
			body: `{} {}`,
			dst:  &struct{}{},
			want: "expected a single JSON object",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var err error
			if strings.HasPrefix(tt.name, "optional") {
				err = decodeOptionalStrictJSON(strings.NewReader(tt.body), tt.dst)
			} else {
				err = decodeStrictJSON(strings.NewReader(tt.body), tt.dst)
			}
			if tt.want == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
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
			State:              "running",
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
