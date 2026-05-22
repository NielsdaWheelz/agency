package store

import (
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestValidateCanonicalStoreTimestamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "empty", value: ""},
		{name: "utc rfc3339", value: "2026-01-01T00:00:00Z"},
		{name: "malformed", value: "not-a-timestamp", wantErr: true},
		{name: "offset", value: "2026-01-01T00:00:00+01:00", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateCanonicalStoreTimestamp("record.json", "record_path", "/tmp/record.json", "updated_at", tt.value)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			if got := errors.GetCode(err); got != errors.EStoreCorrupt {
				t.Fatalf("error code = %s, want %s", got, errors.EStoreCorrupt)
			}
		})
	}
}
