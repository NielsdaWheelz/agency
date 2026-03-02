package runners

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestCanonicalIDs(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		[]string{"claude-code", "codex", "amp", "opencode", "cursor-cli", "droid"},
		CanonicalIDs(),
	)
}

func TestResolve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantID  string
		wantErr bool
	}{
		{name: "legacy claude alias", input: "claude", wantID: "claude-code"},
		{name: "canonical claude-code", input: "claude-code", wantID: "claude-code"},
		{name: "codex", input: "codex", wantID: "codex"},
		{name: "amp", input: "amp", wantID: "amp"},
		{name: "opencode", input: "opencode", wantID: "opencode"},
		{name: "cursor-cli", input: "cursor-cli", wantID: "cursor-cli"},
		{name: "droid", input: "droid", wantID: "droid"},
		{name: "unknown", input: "unknown", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cap, err := Resolve(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, errors.ERunnerNotFound, errors.GetCode(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, cap.ID)
		})
	}
}

func TestValidateArgs(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateArgs("claude-code", []string{"--model", "opus"}))
	require.Error(t, ValidateArgs("claude-code", []string{"--output-format", "json"}))
	require.Error(t, ValidateArgs("codex", []string{"--json"}))
	require.NoError(t, ValidateArgs("amp", []string{"--model", "amp-fast"}))
}

func TestBuildHeadlessArgs(t *testing.T) {
	t.Parallel()

	claudeArgs, err := BuildHeadlessArgs("claude", "fix bug", "/sandbox", []string{"--model", "opus"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-p", "--output-format", "stream-json", "--verbose", "--model", "opus", "fix bug"}, claudeArgs)

	codexArgs, err := BuildHeadlessArgs("codex", "fix bug", "/sandbox", []string{"--model", "gpt-5"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-C", "/sandbox", "exec", "--json", "--model", "gpt-5", "fix bug"}, codexArgs)

	ampArgs, err := BuildHeadlessArgs("amp", "fix bug", "/sandbox", []string{"--model", "amp-fast"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "amp-fast", "fix bug"}, ampArgs)
}

func TestHasSemanticAdapter(t *testing.T) {
	t.Parallel()

	assert.True(t, HasSemanticAdapter("claude"))
	assert.True(t, HasSemanticAdapter("claude-code"))
	assert.True(t, HasSemanticAdapter("codex"))
	assert.False(t, HasSemanticAdapter("amp"))
	assert.False(t, HasSemanticAdapter("opencode"))
	assert.False(t, HasSemanticAdapter("cursor-cli"))
	assert.False(t, HasSemanticAdapter("droid"))
}
