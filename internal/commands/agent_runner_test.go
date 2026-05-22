package commands

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestResolveAgentRunner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		def     string
		want    string
		wantErr bool
	}{
		{name: "empty input resolves using defaults runner", input: "", def: "claude-code", want: "claude-code"},
		{name: "empty input errors when defaults runner is missing", input: "", def: "", wantErr: true},
		{name: "canonical claude-code", input: "claude-code", def: "codex", want: "claude-code"},
		{name: "codex", input: "codex", def: "claude-code", want: "codex"},
		{name: "amp", input: "amp", def: "claude-code", want: "amp"},
		{name: "opencode", input: "opencode", def: "claude-code", want: "opencode"},
		{name: "cursor canonical", input: "cursor", def: "claude-code", want: "cursor"},
		{name: "droid", input: "droid", def: "claude-code", want: "droid"},
		{name: "unknown rejected", input: "unknown", def: "claude-code", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveAgentRunner(tt.input, tt.def)
			if tt.wantErr {
				require.Error(t, err)
				if gotCode := errors.GetCode(err); gotCode != errors.EUsage {
					t.Fatalf("error code = %s, want %s", gotCode, errors.EUsage)
				}
				return
			}
			require.NoError(t, err)
			if got != tt.want {
				t.Fatalf("resolveAgentRunner(%q, %q) = %q, want %q", tt.input, tt.def, got, tt.want)
			}
		})
	}
}
