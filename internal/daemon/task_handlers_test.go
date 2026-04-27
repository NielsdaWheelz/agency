package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestTaskRetryIdempotentDuplicate(t *testing.T) {
	st := store.NewStore(fs.NewRealFS(), t.TempDir(), func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), t.TempDir())

	require.NoError(t, seedTaskRetryRecord(st, "repo-1", "task-1", "inv-1", "retry-1", "same prompt"))

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
	st := store.NewStore(fs.NewRealFS(), t.TempDir(), func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	})
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), t.TempDir())

	require.NoError(t, seedTaskRetryRecord(st, "repo-1", "task-1", "inv-1", "retry-1", "original prompt"))

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

func seedTaskRetryRecord(st *store.Store, repoID, taskID, invocationID, clientRequestID, prompt string) error {
	if _, err := st.EnsureTaskDir(repoID, taskID); err != nil {
		return err
	}
	meta := store.NewTaskMeta(taskID, "feature", repoID, "/repo", "main", store.RunnerModeHeadless, "claude-code", "start-1", "start-fp", st.Now())
	meta.State = store.TaskStateRunning
	meta.WorktreeID = "wt-1"
	meta.PrimaryInvocationID = invocationID
	req := TaskRetryRequest{
		Mode:            string(store.RunnerModeHeadless),
		Runner:          "claude-code",
		Prompt:          prompt,
		ClientRequestID: clientRequestID,
	}
	meta.RetryRequests = map[string]store.TaskRetryRecord{
		clientRequestID: {
			RequestFingerprint: taskRetryFingerprint(meta, req.Mode, req.Runner, req),
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
	invMeta := store.NewInvocationMeta(invocationID, "", "wt-1", "/sandbox", "agency/sandbox-"+invocationID, "abc123", "claude-code", store.RunnerModeHeadless, st.Now())
	invMeta.Status = store.InvocationStatusRunning
	invMeta.TaskID = taskID
	return st.WriteInvocationMeta(repoID, invocationID, invMeta)
}
