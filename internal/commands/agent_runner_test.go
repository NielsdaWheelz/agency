package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		{name: "empty input resolves using defaults runner", input: "", def: "claude", want: "claude-code"},
		{name: "empty input falls back to claude-code when defaults missing", input: "", def: "", want: "claude-code"},
		{name: "legacy claude alias resolves to canonical", input: "claude", def: "codex", want: "claude-code"},
		{name: "canonical claude-code", input: "claude-code", def: "codex", want: "claude-code"},
		{name: "codex", input: "codex", def: "claude", want: "codex"},
		{name: "amp", input: "amp", def: "claude", want: "amp"},
		{name: "opencode", input: "opencode", def: "claude", want: "opencode"},
		{name: "cursor canonical", input: "cursor", def: "claude", want: "cursor"},
		{name: "cursor-cli compatibility alias", input: "cursor-cli", def: "claude", want: "cursor"},
		{name: "droid", input: "droid", def: "claude", want: "droid"},
		{name: "unknown rejected", input: "unknown", def: "claude", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveAgentRunner(tt.input, tt.def)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, errors.EUsage, errors.GetCode(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
