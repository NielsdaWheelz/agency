package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

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
			if got := payload["error_code"]; got != "E_INVALID_REQUEST" {
				t.Fatalf("error_code = %v, want E_INVALID_REQUEST", got)
			}
			if got := payload["message"]; got != tt.want {
				t.Fatalf("message = %v, want %q", got, tt.want)
			}
		})
	}
}
