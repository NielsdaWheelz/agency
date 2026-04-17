package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestIDContractMatrix_InvocationMutationAndCheckEndpoints(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	const customRequestID = "contract-matrix-reqid-123"

	testCases := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{
			name:   "control_plane_start_headless",
			method: http.MethodPost,
			path:   "/invocations/start_headless",
			body:   []byte(`{}`),
		},
		{
			name:   "control_plane_start_headed",
			method: http.MethodPost,
			path:   "/invocations/start_headed",
			body:   []byte(`{}`),
		},
		{
			name:   "stop_missing_repo",
			method: http.MethodPost,
			path:   "/invocations/inv-1/stop",
			body:   nil,
		},
		{
			name:   "kill_missing_repo",
			method: http.MethodPost,
			path:   "/invocations/inv-1/kill",
			body:   nil,
		},
		{
			name:   "checkpoint_apply_missing_repo",
			method: http.MethodPost,
			path:   "/invocations/inv-1/checkpoints/apply",
			body:   []byte(`{"checkpoint_id":1}`),
		},
		{
			name:   "land_missing_repo",
			method: http.MethodPost,
			path:   "/invocations/inv-1/land",
			body:   []byte(`{}`),
		},
		{
			name:   "discard_missing_repo",
			method: http.MethodPost,
			path:   "/invocations/inv-1/discard",
			body:   []byte(`{}`),
		},
		{
			name:   "followup_missing_repo",
			method: http.MethodPost,
			path:   "/invocations/inv-1/followup",
			body:   []byte(`{"client_request_id":"req-1","prompt":"continue"}`),
		},
		{
			name:   "recreate_missing_repo",
			method: http.MethodPost,
			path:   "/invocations/inv-1/recreate",
			body:   nil,
		},
		{
			name:   "worktree_pr_sync_missing_repo",
			method: http.MethodPost,
			path:   "/worktrees/wt-1/pr/sync",
			body:   []byte(`{}`),
		},
		{
			name:   "worktree_merge_missing_repo",
			method: http.MethodPost,
			path:   "/worktrees/wt-1/pr/merge",
			body:   []byte(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
		},
		{
			name:   "worktree_rebase_missing_repo",
			method: http.MethodPost,
			path:   "/worktrees/wt-1/rebase",
			body:   []byte(`{}`),
		},
		{
			name:   "check",
			method: http.MethodGet,
			path:   "/invocations/inv-1/check?repo_id=" + env.RepoID,
			body:   nil,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := env.newInvocationRequestWithHeaders(t, tc.method, tc.path, tc.body, map[string]string{
				"X-Request-ID": customRequestID,
			})
			w := httptest.NewRecorder()
			env.apiHandler().ServeHTTP(w, req)

			var payload map[string]any
			require.NoError(t, json.NewDecoder(w.Body).Decode(&payload), "failed to decode response")

			requestID, ok := payload["request_id"].(string)
			require.True(t, ok, "response body must include request_id")
			assert.Equal(t, customRequestID, requestID)
			assert.Equal(t, requestID, w.Header().Get("X-Request-ID"))
		})
	}
}
