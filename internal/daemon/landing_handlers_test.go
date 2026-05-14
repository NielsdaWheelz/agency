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

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func TestHandleLandDiscardRouting(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	t.Run("GET /land returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/invocations/test-inv/land?repo_id=test-repo", nil)
		w := httptest.NewRecorder()

		s.handleInvocations(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("GET /discard returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/invocations/test-inv/discard?repo_id=test-repo", nil)
		w := httptest.NewRecorder()

		s.handleInvocations(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("POST /land routes correctly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/land?repo_id=test-repo", nil)
		w := httptest.NewRecorder()

		s.handleInvocations(w, req)

		// Should NOT be 404 "unknown action". It will fail at invocation lookup.
		var resp map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		if code, ok := resp["error_code"].(string); ok {
			assert.NotEqual(t, "E_NOT_FOUND", code, "land route should be recognized")
		}
	})

	t.Run("POST /discard routes correctly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/invocations/test-inv/discard?repo_id=test-repo", nil)
		w := httptest.NewRecorder()

		s.handleInvocations(w, req)

		var resp map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		if code, ok := resp["error_code"].(string); ok {
			assert.NotEqual(t, "E_NOT_FOUND", code, "discard route should be recognized")
		}
	})
}

func TestHandleLandDiscard_ErrorResponseIncludesRequestID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	tests := []string{
		"/invocations/test-inv/land",
		"/invocations/test-inv/discard",
	}
	for _, path := range tests {
		path := path
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			w := httptest.NewRecorder()
			s.handleInvocations(w, req)

			var payload map[string]any
			require.NoError(t, json.NewDecoder(w.Body).Decode(&payload), "failed to decode response")
			requestID, ok := payload["request_id"].(string)
			require.True(t, ok, "request_id must be present")
			assert.NotEmpty(t, requestID)
			assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
		})
	}
}

func TestHandleLandDiscard_StrictOptionalBody(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := store.NewStore(fs.NewRealFS(), tmpDir, time.Now)
	s := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), tmpDir)

	tests := []string{
		"/invocations/test-inv/land?repo_id=test-repo",
		"/invocations/test-inv/discard?repo_id=test-repo",
	}
	for _, path := range tests {
		path := path
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{"unknown":true}`)))
			req.ContentLength = -1
			w := httptest.NewRecorder()

			s.handleInvocations(w, req)

			var payload map[string]any
			require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
			assert.Equal(t, "E_INVALID_REQUEST", payload["error_code"])
			assert.Contains(t, payload["message"], `unknown field "unknown"`)
		})
	}
}
