package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// 2.0 TestHandleInvocations_CheckpointRouting
// This test verifies that /invocations/{id}/checkpoints/apply reaches
// handleCheckpoints instead of returning "unknown action" 404.
// This was a critical routing bug: action was "checkpoints/apply" but
// the switch only matched "checkpoints".
func TestHandleInvocations_CheckpointRouting(t *testing.T) {
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	// Send POST /invocations/test-inv/checkpoints/apply
	// Without proper routing fix, this returns 404 "unknown action: checkpoints/apply"
	req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/checkpoints/apply?repo_id=test-repo", nil)
	w := httptest.NewRecorder()

	s.handleInvocations(w, req)

	// Should NOT be a 404 "unknown action".
	// It might be 400 (missing body) or 404 (invocation not found), but not "unknown action".
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v, body: %s", err, w.Body.String())
	}

	// The key assertion: the error code should NOT be "E_NOT_FOUND" with "unknown action" message
	if code, ok := resp["error_code"].(string); ok {
		if code == "E_NOT_FOUND" {
			msg, _ := resp["message"].(string)
			if msg == "unknown action: checkpoints/apply" {
				t.Fatal("routing bug: checkpoints/apply not routed correctly; got 'unknown action: checkpoints/apply'")
			}
		}
	}

	// The request should reach handleCheckpointApply, which will fail
	// on invalid body (no JSON), returning E_INVALID_REQUEST
	if w.Code == http.StatusNotFound {
		msg, _ := resp["message"].(string)
		if msg != "" && msg != "unknown action: checkpoints/apply" {
			// This is fine - it could be "invocation not found" or similar
			return
		}
	}
}

// 2.1 TestHandleCheckpointApply_ValidationErrors
func TestHandleCheckpointApply_ValidationErrors(t *testing.T) {
	tests := []struct {
		name       string
		repoID     string
		body       any
		wantCode   string
		wantStatus int
	}{
		{
			name:       "missing repo_id",
			repoID:     "",
			body:       CheckpointApplyRequest{CheckpointID: 1},
			wantCode:   "E_INVALID_REQUEST",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid JSON body",
			repoID:     "test-repo",
			body:       nil, // will send garbage
			wantCode:   "E_INVALID_REQUEST",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "checkpoint_id=0",
			repoID:     "test-repo",
			body:       CheckpointApplyRequest{CheckpointID: 0},
			wantCode:   "E_INVALID_REQUEST",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "checkpoint_id=-1",
			repoID:     "test-repo",
			body:       CheckpointApplyRequest{CheckpointID: -1},
			wantCode:   "E_INVALID_REQUEST",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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

// 2.2 TestHandleCheckpointApply_InvocationNotFound
func TestHandleCheckpointApply_InvocationNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	body, _ := json.Marshal(CheckpointApplyRequest{CheckpointID: 1})
	req := httptest.NewRequest(http.MethodPost, "/invocations/nonexistent/checkpoints/apply?repo_id=test-repo", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleCheckpointApply(w, req, "nonexistent")

	var resp CheckpointApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if resp.ErrorCode != "E_INVOCATION_NOT_FOUND" {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, "E_INVOCATION_NOT_FOUND")
	}
}

// setupInvocationMeta is a helper that writes an invocation meta.json for testing.
func setupInvocationMeta(t *testing.T, st *store.Store, repoID, invocationID string, mode store.RunnerMode, status store.InvocationStatus) {
	t.Helper()

	// Create directory structure
	if _, err := st.EnsureInvocationDir(repoID, invocationID); err != nil {
		t.Fatal(err)
	}

	meta := store.NewInvocationMeta(
		invocationID,
		"",
		"wt-001",
		"/sandbox/path",
		"agency/sandbox-"+invocationID,
		"basecommit",
		"claude",
		mode,
		time.Now(),
	)
	meta.Status = status
	if status == store.InvocationStatusFinished || status == store.InvocationStatusFailed {
		meta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}

	if err := st.WriteInvocationMeta(repoID, invocationID, meta); err != nil {
		t.Fatal(err)
	}
}

// 2.3 TestHandleCheckpointApply_WrongMode
func TestHandleCheckpointApply_WrongMode(t *testing.T) {
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	setupInvocationMeta(t, st, "test-repo", "test-inv", store.RunnerModeHeaded, store.InvocationStatusFinished)

	body, _ := json.Marshal(CheckpointApplyRequest{CheckpointID: 1})
	req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/checkpoints/apply?repo_id=test-repo", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleCheckpointApply(w, req, "test-inv")

	var resp CheckpointApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if resp.ErrorCode != "E_INVOCATION_INVALID_MODE" {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, "E_INVOCATION_INVALID_MODE")
	}
}

// 2.4 TestHandleCheckpointApply_StillRunning
func TestHandleCheckpointApply_StillRunning(t *testing.T) {
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	setupInvocationMeta(t, st, "test-repo", "test-inv", store.RunnerModeHeadless, store.InvocationStatusRunning)

	body, _ := json.Marshal(CheckpointApplyRequest{CheckpointID: 1})
	req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/checkpoints/apply?repo_id=test-repo", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleCheckpointApply(w, req, "test-inv")

	var resp CheckpointApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
	if resp.ErrorCode != "E_INVOCATION_STILL_RUNNING" {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, "E_INVOCATION_STILL_RUNNING")
	}
}

// 2.5 TestHandleCheckpointApply_Starting
func TestHandleCheckpointApply_Starting(t *testing.T) {
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	setupInvocationMeta(t, st, "test-repo", "test-inv", store.RunnerModeHeadless, store.InvocationStatusStarting)

	body, _ := json.Marshal(CheckpointApplyRequest{CheckpointID: 1})
	req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/checkpoints/apply?repo_id=test-repo", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleCheckpointApply(w, req, "test-inv")

	var resp CheckpointApplyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
	if resp.ErrorCode != "E_INVOCATION_STILL_RUNNING" {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, "E_INVOCATION_STILL_RUNNING")
	}
}
