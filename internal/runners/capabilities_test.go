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
		[]string{"claude-code", "codex", "amp", "opencode", "cursor", "droid"},
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
		{name: "cursor", input: "cursor", wantID: "cursor"},
		{name: "legacy cursor-cli alias", input: "cursor-cli", wantID: "cursor"},
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
	require.Error(t, ValidateArgs("amp", []string{"-x"}))
	require.Error(t, ValidateArgs("opencode", []string{"run"}))
	require.Error(t, ValidateArgs("cursor", []string{"-p"}))
	require.Error(t, ValidateArgs("droid", []string{"exec"}))
	require.NoError(t, ValidateArgs("amp", []string{"--model", "amp-fast"}))

	// Permission flags are allowed in headed mode (user at terminal).
	require.NoError(t, ValidateArgs("claude-code", []string{"--dangerously-skip-permissions"}))
	require.NoError(t, ValidateArgs("codex", []string{"--full-auto"}))
	require.NoError(t, ValidateArgs("opencode", []string{"--mode", "safe"}))
}

func TestValidateHeadlessArgs(t *testing.T) {
	t.Parallel()

	// Structural flags rejected in headless mode.
	require.Error(t, ValidateHeadlessArgs("claude-code", []string{"--output-format", "json"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--json"}))
	require.Error(t, ValidateHeadlessArgs("opencode", []string{"run"}))

	// Permission/approval flags rejected in headless mode — Agency controls them.
	require.Error(t, ValidateHeadlessArgs("claude-code", []string{"--dangerously-skip-permissions"}))
	require.Error(t, ValidateHeadlessArgs("claude-code", []string{"--permission-mode", "default"}))
	require.Error(t, ValidateHeadlessArgs("claude-code", []string{"--permission-mode=acceptEdits"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--full-auto"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--approval-mode", "suggest"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--approval-mode=auto-edit"}))
	require.Error(t, ValidateHeadlessArgs("opencode", []string{"--mode", "safe"}))
	require.Error(t, ValidateHeadlessArgs("opencode", []string{"--mode=auto"}))

	// Non-conflicting flags pass.
	require.NoError(t, ValidateHeadlessArgs("claude-code", []string{"--model", "opus"}))
	require.NoError(t, ValidateHeadlessArgs("codex", []string{"--model", "gpt-5"}))
	require.NoError(t, ValidateHeadlessArgs("opencode", []string{"--model", "open"}))
}

func TestBuildHeadlessArgs(t *testing.T) {
	t.Parallel()

	claudeArgs, err := BuildHeadlessArgs("claude", "fix bug", "/sandbox", []string{"--model", "opus"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-p", "--output-format", "stream-json", "--verbose", "--dangerously-skip-permissions", "--model", "opus", "fix bug"}, claudeArgs)

	codexArgs, err := BuildHeadlessArgs("codex", "fix bug", "/sandbox", []string{"--model", "gpt-5"})
	require.NoError(t, err)
	assert.Equal(t, []string{"exec", "--cd", "/sandbox", "--json", "--full-auto", "--model", "gpt-5", "fix bug"}, codexArgs)

	ampArgs, err := BuildHeadlessArgs("amp", "fix bug", "/sandbox", []string{"--model", "amp-fast"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-x", "--model", "amp-fast", "fix bug"}, ampArgs)

	opencodeArgs, err := BuildHeadlessArgs("opencode", "fix bug", "/sandbox", []string{"--model", "open"})
	require.NoError(t, err)
	assert.Equal(t, []string{"run", "--mode", "auto", "--model", "open", "fix bug"}, opencodeArgs)

	cursorArgs, err := BuildHeadlessArgs("cursor", "fix bug", "/sandbox", []string{"--model", "cursor-fast"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-p", "--model", "cursor-fast", "fix bug"}, cursorArgs)

	droidArgs, err := BuildHeadlessArgs("droid", "fix bug", "/sandbox", []string{"--model", "droid-1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"exec", "--model", "droid-1", "fix bug"}, droidArgs)
}

func TestBuildHeadedArgs(t *testing.T) {
	t.Parallel()

	claudeArgs, err := BuildHeadedArgs("claude", []string{"--model", "opus"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "opus"}, claudeArgs)

	codexArgs, err := BuildHeadedArgs("codex", []string{"--model", "gpt-5"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "gpt-5"}, codexArgs)

	ampArgs, err := BuildHeadedArgs("amp", []string{"--model", "amp-fast"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "amp-fast"}, ampArgs)

	opencodeArgs, err := BuildHeadedArgs("opencode", []string{"--model", "open"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "open"}, opencodeArgs)

	cursorArgs, err := BuildHeadedArgs("cursor", []string{"--model", "cursor-fast"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "cursor-fast"}, cursorArgs)

	droidArgs, err := BuildHeadedArgs("droid", []string{"--model", "droid-1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "droid-1"}, droidArgs)
}

func TestHasSemanticAdapter(t *testing.T) {
	t.Parallel()

	assert.True(t, HasSemanticAdapter("claude"))
	assert.True(t, HasSemanticAdapter("claude-code"))
	assert.True(t, HasSemanticAdapter("codex"))
	assert.False(t, HasSemanticAdapter("amp"))
	assert.False(t, HasSemanticAdapter("opencode"))
	assert.False(t, HasSemanticAdapter("cursor"))
	assert.False(t, HasSemanticAdapter("cursor-cli"))
	assert.False(t, HasSemanticAdapter("droid"))
}
