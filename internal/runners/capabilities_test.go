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
	require.Error(t, ValidateArgs("claude-code", []string{"--input-format", "text"}))
	require.Error(t, ValidateArgs("codex", []string{"--json"}))
	require.Error(t, ValidateArgs("codex", []string{"resume"}))
	require.Error(t, ValidateArgs("codex", []string{"--last"}))
	require.Error(t, ValidateArgs("amp", []string{"-x"}))
	require.Error(t, ValidateArgs("amp", []string{"--stream-json"}))
	require.Error(t, ValidateArgs("amp", []string{"--stream-json-input"}))
	require.Error(t, ValidateArgs("opencode", []string{"run"}))
	require.Error(t, ValidateArgs("cursor", []string{"-p"}))
	require.Error(t, ValidateArgs("cursor", []string{"--output-format", "json"}))
	require.Error(t, ValidateArgs("cursor", []string{"--resume", "abc-123"}))
	require.Error(t, ValidateArgs("cursor", []string{"--continue"}))
	require.Error(t, ValidateArgs("cursor", []string{"--workspace", "/tmp/outside"}))
	require.Error(t, ValidateArgs("droid", []string{"exec"}))
	require.Error(t, ValidateArgs("droid", []string{"--output-format", "text"}))
	require.Error(t, ValidateArgs("droid", []string{"--input-format", "text"}))
	require.NoError(t, ValidateArgs("amp", []string{"--model", "amp-fast"}))

	// Permission flags are allowed in headed mode (user at terminal).
	require.NoError(t, ValidateArgs("claude-code", []string{"--dangerously-skip-permissions"}))
	require.NoError(t, ValidateArgs("codex", []string{"--full-auto"}))
	require.NoError(t, ValidateArgs("cursor", []string{"--force"}))
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
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--dangerously-bypass-approvals-and-sandbox"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--yolo"}))
	require.Error(t, ValidateHeadlessArgs("cursor", []string{"--force"}))
	require.Error(t, ValidateHeadlessArgs("cursor", []string{"-f"}))
	require.Error(t, ValidateHeadlessArgs("cursor", []string{"--yolo"}))
	require.Error(t, ValidateHeadlessArgs("cursor", []string{"--trust"}))
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
	assert.Equal(t, []string{"-p", "--output-format", "stream-json", "--input-format", "stream-json", "--verbose", "--dangerously-skip-permissions", "--model", "opus"}, claudeArgs)

	codexArgs, err := BuildHeadlessArgs("codex", "fix bug", "/sandbox", []string{"--model", "gpt-5"})
	require.NoError(t, err)
	assert.Equal(t, []string{"exec", "--cd", "/sandbox", "--json", "--full-auto", "--model", "gpt-5", "--disable", "unified_exec", "fix bug"}, codexArgs)

	ampArgs, err := BuildHeadlessArgs("amp", "fix bug", "/sandbox", []string{"--model", "amp-fast"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-x", "--stream-json", "--stream-json-input", "--model", "amp-fast"}, ampArgs)

	opencodeArgs, err := BuildHeadlessArgs("opencode", "fix bug", "/sandbox", []string{"--model", "open"})
	require.NoError(t, err)
	assert.Equal(t, []string{"run", "--mode", "auto", "--model", "open", "fix bug"}, opencodeArgs)

	cursorArgs, err := BuildHeadlessArgs("cursor", "fix bug", "/sandbox", []string{"--model", "cursor-fast"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-p", "--output-format", "stream-json", "--force", "--workspace", "/sandbox", "--model", "cursor-fast", "fix bug"}, cursorArgs)

	droidArgs, err := BuildHeadlessArgs("droid", "fix bug", "/sandbox", []string{"--model", "droid-1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"exec", "--output-format", "stream-json", "--input-format", "stream-json", "--model", "droid-1"}, droidArgs)
}

func TestBuildHeadlessArgs_RequiresPrompt(t *testing.T) {
	t.Parallel()

	_, err := BuildHeadlessArgs("claude-code", "   ", "/sandbox", nil)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
}

func TestBuildResumeArgs(t *testing.T) {
	t.Parallel()

	codexArgs, err := BuildResumeArgs("codex", "continue from previous turn", "", []string{"--model", "gpt-5"})
	require.NoError(t, err)
	assert.Equal(t, []string{"exec", "resume", "--last", "--json", "--full-auto", "--model", "gpt-5", "--disable", "unified_exec", "continue from previous turn"}, codexArgs)

	codexExplicitArgs, err := BuildResumeArgs("codex", "continue from previous turn", "thread_abc123", []string{"--model", "gpt-5"})
	require.NoError(t, err)
	assert.Equal(t, []string{"exec", "resume", "thread_abc123", "--json", "--full-auto", "--model", "gpt-5", "--disable", "unified_exec", "continue from previous turn"}, codexExplicitArgs)

	cursorArgs, err := BuildResumeArgs("cursor", "continue from previous turn", "", []string{"--model", "sonnet-4.6-thinking"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-p", "--output-format", "stream-json", "--force", "--continue", "--model", "sonnet-4.6-thinking", "continue from previous turn"}, cursorArgs)

	cursorExplicitArgs, err := BuildResumeArgs("cursor", "continue from previous turn", "sess_abc123", []string{"--model", "sonnet-4.6-thinking"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-p", "--output-format", "stream-json", "--force", "--resume", "sess_abc123", "--model", "sonnet-4.6-thinking", "continue from previous turn"}, cursorExplicitArgs)

	_, err = BuildResumeArgs("amp", "continue", "", nil)
	require.Error(t, err)
	assert.Equal(t, errors.EInvocationInvalidMode, errors.GetCode(err))

	assert.True(t, SupportsResumeTurns("codex"))
	assert.True(t, SupportsResumeTurns("cursor"))
	assert.False(t, SupportsResumeTurns("amp"))
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

func TestChatMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		runner string
		want   ChatMode
	}{
		{"claude-code", ChatModeStdin},
		{"claude", ChatModeStdin},
		{"codex", ChatModeResume},
		{"amp", ChatModeStdin},
		{"opencode", ChatModeResume},
		{"cursor", ChatModeResume},
		{"cursor-cli", ChatModeResume},
		{"droid", ChatModeStdin},
	}
	for _, tt := range tests {
		t.Run(tt.runner, func(t *testing.T) {
			t.Parallel()
			mode, err := ResolveChatMode(tt.runner)
			require.NoError(t, err)
			assert.Equal(t, tt.want, mode)
		})
	}
}

func TestInitialPromptMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		runner string
		want   InitialPromptMode
	}{
		{"claude-code", InitialPromptStdin},
		{"claude", InitialPromptStdin},
		{"codex", InitialPromptPositional},
		{"amp", InitialPromptStdin},
		{"opencode", InitialPromptPositional},
		{"cursor", InitialPromptPositional},
		{"cursor-cli", InitialPromptPositional},
		{"droid", InitialPromptStdin},
	}
	for _, tt := range tests {
		t.Run(tt.runner, func(t *testing.T) {
			t.Parallel()
			mode, err := ResolveInitialPromptMode(tt.runner)
			require.NoError(t, err)
			assert.Equal(t, tt.want, mode)
		})
	}
}

func TestChatMode_UnknownRunner(t *testing.T) {
	t.Parallel()
	_, err := ResolveChatMode("unknown")
	require.Error(t, err)
	assert.Equal(t, errors.ERunnerNotFound, errors.GetCode(err))
}

func TestHasSemanticAdapter(t *testing.T) {
	t.Parallel()

	assert.True(t, HasSemanticAdapter("claude"))
	assert.True(t, HasSemanticAdapter("claude-code"))
	assert.True(t, HasSemanticAdapter("codex"))
	assert.True(t, HasSemanticAdapter("cursor"))
	assert.True(t, HasSemanticAdapter("cursor-cli"))
	assert.False(t, HasSemanticAdapter("amp"))
	assert.False(t, HasSemanticAdapter("opencode"))
	assert.False(t, HasSemanticAdapter("droid"))
}
