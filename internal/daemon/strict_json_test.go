package daemon

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStrictJSONDecodeErrorMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		dst      any
		optional bool
		want     string
	}{
		{
			name: "unknown field",
			body: `{"unknown":true}`,
			dst: &struct {
				Known bool `json:"known"`
			}{},
			want: `invalid request body: unknown field "unknown"`,
		},
		{
			name: "malformed json",
			body: `{"known":`,
			dst: &struct {
				Known bool `json:"known"`
			}{},
			want: "invalid request body: malformed JSON",
		},
		{
			name: "typed field",
			body: `{"known":"bad"}`,
			dst: &struct {
				Known bool `json:"known"`
			}{},
			want: `invalid request body: field "known" must be bool`,
		},
		{
			name: "invalid value type",
			body: `"bad"`,
			dst:  &struct{}{},
			want: "invalid request body: invalid value type",
		},
		{
			name: "trailing object",
			body: `{} {}`,
			dst:  &struct{}{},
			want: "invalid request body: expected a single JSON object",
		},
		{
			name:     "null object",
			body:     `null`,
			dst:      &struct{}{},
			optional: true,
			want:     "invalid request body: expected a JSON object",
		},
		{
			name: "request shape error",
			body: `{"env":null}`,
			dst:  &TaskStartRequest{},
			want: "invalid request body: env must be an object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var err error
			if tt.optional {
				err = decodeOptionalStrictJSON(strings.NewReader(tt.body), tt.dst)
			} else {
				err = decodeStrictJSON(strings.NewReader(tt.body), tt.dst)
			}
			require.Error(t, err)
			if got := strictJSONDecodeErrorMessage(err); got != tt.want {
				t.Fatalf("strictJSONDecodeErrorMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
