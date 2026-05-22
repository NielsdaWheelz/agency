package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestRepoMutationDecodeFailuresReturnInvalidRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		body string
	}{
		{
			name: "register",
			path: "/repos/register",
			body: `{"repo_root":"/tmp/repo","unknown":true}`,
		},
		{
			name: "rm",
			path: "/repos/rm",
			body: `{"repo_ref":"repo-1","unknown":true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := setupReadTestEnv(t)
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader([]byte(tt.body)))
			w := httptest.NewRecorder()

			env.apiHandler().ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code)
			var resp rawAPIResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			if resp.OK {
				t.Fatalf("response OK = true, want false")
			}
			if resp.ErrorCode != string(errors.EInvalidRequest) {
				t.Fatalf("error code = %q, want %q", resp.ErrorCode, errors.EInvalidRequest)
			}
		})
	}
}
