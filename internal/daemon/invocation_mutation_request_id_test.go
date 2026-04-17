package daemon

import (
	"encoding/json"
	"net/http"
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
