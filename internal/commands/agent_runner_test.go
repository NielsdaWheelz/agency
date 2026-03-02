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
		want    string
		wantErr bool
	}{
		{name: "default empty resolves to canonical claude-code", input: "", want: "claude-code"},
		{name: "legacy claude alias resolves to canonical", input: "claude", want: "claude-code"},
		{name: "canonical claude-code", input: "claude-code", want: "claude-code"},
		{name: "codex", input: "codex", want: "codex"},
		{name: "amp", input: "amp", want: "amp"},
		{name: "opencode", input: "opencode", want: "opencode"},
		{name: "cursor-cli", input: "cursor-cli", want: "cursor-cli"},
		{name: "droid", input: "droid", want: "droid"},
		{name: "unknown rejected", input: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveAgentRunner(tt.input)
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
