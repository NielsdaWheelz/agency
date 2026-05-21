package daemon

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStrictJSONDecodeErrorMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   string
		dst    any
		decode func(string, any) error
		want   string
	}{
		{
			name: "unknown field",
			body: `{"unknown":true}`,
			dst: &struct {
				Known bool `json:"known"`
			}{},
			decode: decodeRequiredJSONForTest,
			want:   `invalid request body: unknown field "unknown"`,
		},
		{
			name: "malformed json",
			body: `{"known":`,
			dst: &struct {
				Known bool `json:"known"`
			}{},
			decode: decodeRequiredJSONForTest,
			want:   "invalid request body: malformed JSON",
		},
		{
			name: "typed field",
			body: `{"known":"bad"}`,
			dst: &struct {
				Known bool `json:"known"`
			}{},
			decode: decodeRequiredJSONForTest,
			want:   `invalid request body: field "known" must be bool`,
		},
		{
			name:   "invalid value type",
			body:   `"bad"`,
			dst:    &struct{}{},
			decode: decodeRequiredJSONForTest,
			want:   "invalid request body: invalid value type",
		},
		{
			name:   "trailing object",
			body:   `{} {}`,
			dst:    &struct{}{},
			decode: decodeRequiredJSONForTest,
			want:   "invalid request body: expected a single JSON object",
		},
		{
			name:   "null object",
			body:   `null`,
			dst:    &struct{}{},
			decode: decodeOptionalJSONForTest,
			want:   "invalid request body: expected a JSON object",
		},
		{
			name:   "request shape error",
			body:   `{"env":null}`,
			dst:    &TaskStartRequest{},
			decode: decodeRequiredJSONForTest,
			want:   "invalid request body: env must be an object",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.decode(tt.body, tt.dst)
			require.Error(t, err)
			assert.Equal(t, tt.want, strictJSONDecodeErrorMessage(err))
		})
	}
}

func decodeRequiredJSONForTest(body string, dst any) error {
	return decodeStrictJSON(strings.NewReader(body), dst)
}

func decodeOptionalJSONForTest(body string, dst any) error {
	return decodeOptionalStrictJSON(strings.NewReader(body), dst)
}
