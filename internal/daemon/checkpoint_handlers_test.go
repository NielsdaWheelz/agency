package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func TestHandleCheckpointApply_ValidationErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		repoID      string
		body        any
		wantCode    string
		wantStatus  int
		wantMessage string
	}{
		{
			name:       "missing repo_id",
			repoID:     "",
			body:       CheckpointApplyRequest{CheckpointID: 1},
			wantCode:   "E_INVALID_REQUEST",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:        "invalid JSON body",
			repoID:      "test-repo",
			body:        nil, // will send garbage
			wantCode:    "E_INVALID_REQUEST",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "invalid request body: malformed JSON",
		},
		{
			name:       "checkpoint_id=0",
			repoID:     "test-repo",
			body:       CheckpointApplyRequest{CheckpointID: 0},
			wantCode:   "E_INVALID_ARGUMENT",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "checkpoint_id=-1",
			repoID:     "test-repo",
			body:       CheckpointApplyRequest{CheckpointID: -1},
			wantCode:   "E_INVALID_ARGUMENT",
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

			var bodyBytes []byte
			if tc.body == nil {
				bodyBytes = []byte("not valid json{{{")
			} else {
				bodyBytes, _ = json.Marshal(tc.body)
			}

			url := "/invocations/test-inv/checkpoints/apply"
			if tc.repoID != "" {
				url += "?repo_id=" + tc.repoID
			}

			req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			s.handleCheckpointApply(w, req, "test-inv")

			var resp CheckpointApplyResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCode, resp.ErrorCode)
			if tc.wantMessage != "" {
				assert.Equal(t, tc.wantMessage, resp.Message)
			}
			assert.False(t, resp.OK, "expected OK=false")
		})
	}
}

func TestHandleCheckpointApply_InvocationNotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)
	registerRepoForCheckpointTests(t, st, "test-repo")

	body, _ := json.Marshal(CheckpointApplyRequest{CheckpointID: 1})
	req := httptest.NewRequest(http.MethodPost, "/invocations/nonexistent/checkpoints/apply?repo_id=test-repo", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleCheckpointApply(w, req, "nonexistent")

	var resp CheckpointApplyResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "E_INVOCATION_NOT_FOUND", resp.ErrorCode)
}

func TestHandleCheckpointApply_ErrorResponseIncludesRequestID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	body, _ := json.Marshal(CheckpointApplyRequest{CheckpointID: 1})
	req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/checkpoints/apply", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleCheckpointApply(w, req, "test-inv")

	var payload map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&payload), "failed to decode response")
	requestID, ok := payload["request_id"].(string)
	require.True(t, ok, "request_id must be present")
	assert.NotEmpty(t, requestID)
	assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
}

// setupInvocationMeta is a helper that writes an invocation meta.json for testing.
func setupInvocationMeta(t *testing.T, st *store.Store, repoID, invocationID string, mode store.RunnerMode, status store.InvocationStatus) {
	t.Helper()

	// Create directory structure
	_, err := st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)

	meta := store.NewInvocationMeta(
		invocationID,
		"",
		"wt-001",
		"/sandbox/path",
		"/checkouts/test-repo",
		"work",
		"agency/sandbox-"+invocationID,
		"basecommit",
		"claude-code",
		mode,
		time.Now(),
	)
	meta.Status = status
	if status == store.InvocationStatusFinished || status == store.InvocationStatusFailed {
		meta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}

	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, meta))
}

func registerRepoForCheckpointTests(t *testing.T, st *store.Store, repoID string) {
	t.Helper()

	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			repoID: {
				RepoID:     repoID,
				Paths:      []string{"/tmp/" + repoID},
				LastSeenAt: "2026-01-15T12:00:00Z",
			},
		},
	}))
}

func TestHandleCheckpointApply_WrongMode(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)
	registerRepoForCheckpointTests(t, st, "test-repo")

	setupInvocationMeta(t, st, "test-repo", "test-inv", store.RunnerModeHeaded, store.InvocationStatusFinished)

	body, _ := json.Marshal(CheckpointApplyRequest{CheckpointID: 1})
	req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/checkpoints/apply?repo_id=test-repo", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleCheckpointApply(w, req, "test-inv")

	var resp CheckpointApplyResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "E_INVOCATION_INVALID_MODE", resp.ErrorCode)
}

func TestHandleCheckpointApply_StillRunning(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)
	registerRepoForCheckpointTests(t, st, "test-repo")

	setupInvocationMeta(t, st, "test-repo", "test-inv", store.RunnerModeHeadless, store.InvocationStatusRunning)

	body, _ := json.Marshal(CheckpointApplyRequest{CheckpointID: 1})
	req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/checkpoints/apply?repo_id=test-repo", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleCheckpointApply(w, req, "test-inv")

	var resp CheckpointApplyResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "E_INVOCATION_STILL_RUNNING", resp.ErrorCode)
}

func TestHandleCheckpointApply_CorruptCheckpointsReturnsStoreCorrupt(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	writeTestUserConfig(t, configDir)
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)
	registerRepoForCheckpointTests(t, st, "test-repo")
	setupInvocationMeta(t, st, "test-repo", "test-inv", store.RunnerModeHeadless, store.InvocationStatusFinished)
	require.NoError(t, os.WriteFile(st.InvocationCheckpointsPath("test-repo", "test-inv"), []byte("{malformed"), 0o644))

	body, _ := json.Marshal(CheckpointApplyRequest{CheckpointID: 1})
	req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/checkpoints/apply?repo_id=test-repo", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleCheckpointApply(w, req, "test-inv")

	var resp CheckpointApplyResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, string(agencyerrors.EStoreCorrupt), resp.ErrorCode)
	assert.Contains(t, resp.Message, "checkpoints.json")
}

func TestHandleCheckpointApply_RespectsRepoLock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)
	registerRepoForCheckpointTests(t, st, "test-repo")

	setupInvocationMeta(t, st, "test-repo", "test-inv", store.RunnerModeHeadless, store.InvocationStatusFinished)

	unlock, err := s.repoLock.Lock("test-repo", "checkpoint-apply-lock-holder")
	require.NoError(t, err, "acquire competing repo lock")
	t.Cleanup(func() {
		_ = unlock()
	})

	body, _ := json.Marshal(CheckpointApplyRequest{CheckpointID: 1})
	req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/checkpoints/apply?repo_id=test-repo", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleCheckpointApply(w, req, "test-inv")

	var resp CheckpointApplyResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "E_REPO_LOCKED", resp.ErrorCode)
}

func TestHandleCheckpointApply_Starting(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)
	registerRepoForCheckpointTests(t, st, "test-repo")

	setupInvocationMeta(t, st, "test-repo", "test-inv", store.RunnerModeHeadless, store.InvocationStatusStarting)

	body, _ := json.Marshal(CheckpointApplyRequest{CheckpointID: 1})
	req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/checkpoints/apply?repo_id=test-repo", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleCheckpointApply(w, req, "test-inv")

	var resp CheckpointApplyResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), "failed to decode response")

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "E_INVOCATION_STILL_RUNNING", resp.ErrorCode)
}
