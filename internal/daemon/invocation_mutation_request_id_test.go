package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/store"
)

func TestInvocationMutations_RequestIDOnStopKillSuccessAndFailure(t *testing.T) {
	t.Parallel()

	t.Run("stop_failure_missing_repo_id", func(t *testing.T) {
		t.Parallel()

		env := setupReadTestEnv(t)
		w := env.doInvocationRequest(t, http.MethodPost, "/invocations/inv-1/stop")

		var payload map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
		requestID, ok := payload["request_id"].(string)
		require.True(t, ok, "request_id must be present")
		assert.NotEmpty(t, requestID)
		assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
	})

	t.Run("stop_success", func(t *testing.T) {
		t.Parallel()

		env := setupReadTestEnv(t)
		require.NoError(t, env.Store.UpdateInvocationMeta(env.RepoID, "inv-1", func(meta *store.InvocationMeta) {
			meta.Status = store.InvocationStatusFinished
		}))

		w := env.doInvocationRequest(t, http.MethodPost, "/invocations/inv-1/stop?repo_id="+env.RepoID)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
		requestID, ok := payload["request_id"].(string)
		require.True(t, ok, "request_id must be present")
		assert.NotEmpty(t, requestID)
		assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
	})

	t.Run("kill_failure_missing_repo_id", func(t *testing.T) {
		t.Parallel()

		env := setupReadTestEnv(t)
		w := env.doInvocationRequest(t, http.MethodPost, "/invocations/inv-1/kill")

		var payload map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
		requestID, ok := payload["request_id"].(string)
		require.True(t, ok, "request_id must be present")
		assert.NotEmpty(t, requestID)
		assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
	})

	t.Run("kill_success", func(t *testing.T) {
		t.Parallel()

		env := setupReadTestEnv(t)
		w := env.doInvocationRequest(t, http.MethodPost, "/invocations/inv-1/kill?repo_id="+env.RepoID)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
		requestID, ok := payload["request_id"].(string)
		require.True(t, ok, "request_id must be present")
		assert.NotEmpty(t, requestID)
		assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
	})
}

func TestInvocationMutations_RequestIDOnFollowUpErrors(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)

	wFollowUp := env.doInvocationRequestWithBody(
		t,
		http.MethodPost,
		"/invocations/inv-1/followup",
		[]byte(`{"client_request_id":"req-1","prompt":"continue"}`),
	)
	var followUpPayload map[string]any
	require.NoError(t, json.NewDecoder(wFollowUp.Body).Decode(&followUpPayload))
	followUpRequestID, ok := followUpPayload["request_id"].(string)
	require.True(t, ok, "followup request_id must be present")
	assert.NotEmpty(t, followUpRequestID)
	assert.Equal(t, followUpRequestID, wFollowUp.Header().Get("X-Request-ID"))
}

func TestInvocationNoBodyMutations_StrictOptionalBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		body []byte
		want string
	}{
		{
			name: "stop unknown field",
			path: "/invocations/inv-1/stop?repo_id=repo-1",
			body: []byte(`{"unknown":true}`),
			want: `invalid request body: unknown field "unknown"`,
		},
		{
			name: "kill trailing object",
			path: "/invocations/inv-1/kill?repo_id=repo-1",
			body: []byte(`{} {}`),
			want: "invalid request body: expected a single JSON object",
		},
		{
			name: "recreate unknown field",
			path: "/invocations/inv-1/recreate?repo_id=repo-1",
			body: []byte(`{"unknown":true}`),
			want: `invalid request body: unknown field "unknown"`,
		},
		{
			name: "recreate trailing object",
			path: "/invocations/inv-1/recreate?repo_id=repo-1",
			body: []byte(`{} {}`),
			want: "invalid request body: expected a single JSON object",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := setupReadTestEnv(t)
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.ContentLength = -1
			w := httptest.NewRecorder()

			env.apiHandler().ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code)
			var payload map[string]any
			require.NoError(t, json.NewDecoder(w.Body).Decode(&payload))
			assert.Equal(t, "E_INVALID_REQUEST", payload["error_code"])
			assert.Equal(t, tt.want, payload["message"])
		})
	}
}
