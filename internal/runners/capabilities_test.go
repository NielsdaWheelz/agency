package runners

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestCanonicalize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantID  string
		wantErr bool
	}{
		{name: "canonical claude-code", input: "claude-code", wantID: "claude-code"},
		{name: "codex", input: "codex", wantID: "codex"},
		{name: "amp", input: "amp", wantID: "amp"},
		{name: "opencode", input: "opencode", wantID: "opencode"},
		{name: "cursor", input: "cursor", wantID: "cursor"},
		{name: "droid", input: "droid", wantID: "droid"},
		{name: "unknown", input: "unknown", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			canonical, err := Canonicalize(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, errors.ERunnerNotFound, errors.GetCode(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, canonical)
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

	// Approval flags remain runner-specific in headed mode, except for Claude's
	// bypass shortcut which Agency owns through typed permission-mode handling.
	require.Error(t, ValidateArgs("claude-code", []string{"--dangerously-skip-permissions"}))
	require.NoError(t, ValidateArgs("codex", []string{"--full-auto"}))
	require.NoError(t, ValidateArgs("codex", []string{"--ask-for-approval", "never"}))
	require.NoError(t, ValidateArgs("codex", []string{"--sandbox", "workspace-write"}))
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
	require.Error(t, ValidateHeadlessArgs("codex", []string{"-a", "never"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--ask-for-approval", "never"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--ask-for-approval=never"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"-s", "workspace-write"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--sandbox", "workspace-write"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--sandbox=workspace-write"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--full-auto"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--dangerously-bypass-approvals-and-sandbox"}))
	require.Error(t, ValidateHeadlessArgs("codex", []string{"--yolo"}))
	require.Error(t, ValidateHeadlessArgs("cursor", []string{"--force"}))
	require.Error(t, ValidateHeadlessArgs("cursor", []string{"-f"}))
	require.Error(t, ValidateHeadlessArgs("cursor", []string{"--yolo"}))
	require.Error(t, ValidateHeadlessArgs("cursor", []string{"--trust"}))
	require.Error(t, ValidateHeadlessArgs("droid", []string{"--auto", "high"}))
	require.Error(t, ValidateHeadlessArgs("droid", []string{"--auto=low"}))
	require.Error(t, ValidateHeadlessArgs("droid", []string{"--skip-permissions-unsafe"}))
	require.Error(t, ValidateHeadlessArgs("opencode", []string{"--mode", "safe"}))
	require.Error(t, ValidateHeadlessArgs("opencode", []string{"--mode=auto"}))

	// Non-conflicting flags pass.
	require.NoError(t, ValidateHeadlessArgs("claude-code", []string{"--model", "opus"}))
	require.NoError(t, ValidateHeadlessArgs("claude-code", []string{"--permission-mode", "bypassPermissions"}))
	require.NoError(t, ValidateHeadlessArgs("codex", []string{"--model", "gpt-5"}))
	require.NoError(t, ValidateHeadlessArgs("opencode", []string{"--model", "open"}))
}

func TestBuildHeadlessArgs(t *testing.T) {
	t.Parallel()

	claudeArgs, err := BuildHeadlessArgs("claude-code", "fix bug", "/sandbox", []string{"--model", "opus", "--permission-mode", "bypassPermissions"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-p", "--output-format", "stream-json", "--input-format", "text", "--verbose", "--model", "opus", "--permission-mode", "bypassPermissions", "fix bug"}, claudeArgs)

	codexArgs, err := BuildHeadlessArgs("codex", "fix bug", "/sandbox", []string{"--model", "gpt-5"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--ask-for-approval", "never", "--sandbox", "workspace-write", "exec", "--cd", "/sandbox", "--json", "--model", "gpt-5", "--disable", "unified_exec", "fix bug"}, codexArgs)

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
	assert.Equal(t, []string{"exec", "--auto", "medium", "--output-format", "stream-json", "--input-format", "stream-json", "--model", "droid-1"}, droidArgs)
}

func TestBuildHeadlessArgs_RequiresPrompt(t *testing.T) {
	t.Parallel()

	_, err := BuildHeadlessArgs("claude-code", "   ", "/sandbox", nil)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
}

func TestBuildResumeArgs(t *testing.T) {
	t.Parallel()

	claudeArgs, err := BuildResumeArgs("claude-code", "continue from previous turn", "", []string{"--model", "opus", "--permission-mode", "bypassPermissions"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-p", "--output-format", "stream-json", "--input-format", "text", "--verbose", "--continue", "--model", "opus", "--permission-mode", "bypassPermissions", "continue from previous turn"}, claudeArgs)

	claudeExplicitArgs, err := BuildResumeArgs("claude-code", "continue from previous turn", "sess_abc123", []string{"--model", "opus", "--permission-mode", "bypassPermissions"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-p", "--output-format", "stream-json", "--input-format", "text", "--verbose", "--resume", "sess_abc123", "--model", "opus", "--permission-mode", "bypassPermissions", "continue from previous turn"}, claudeExplicitArgs)

	codexArgs, err := BuildResumeArgs("codex", "continue from previous turn", "", []string{"--model", "gpt-5"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--ask-for-approval", "never", "--sandbox", "workspace-write", "exec", "resume", "--last", "--json", "--model", "gpt-5", "--disable", "unified_exec", "continue from previous turn"}, codexArgs)

	codexExplicitArgs, err := BuildResumeArgs("codex", "continue from previous turn", "thread_abc123", []string{"--model", "gpt-5"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--ask-for-approval", "never", "--sandbox", "workspace-write", "exec", "resume", "thread_abc123", "--json", "--model", "gpt-5", "--disable", "unified_exec", "continue from previous turn"}, codexExplicitArgs)

	cursorArgs, err := BuildResumeArgs("cursor", "continue from previous turn", "", []string{"--model", "sonnet-4.6-thinking"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-p", "--output-format", "stream-json", "--force", "--continue", "--model", "sonnet-4.6-thinking", "continue from previous turn"}, cursorArgs)

	cursorExplicitArgs, err := BuildResumeArgs("cursor", "continue from previous turn", "sess_abc123", []string{"--model", "sonnet-4.6-thinking"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-p", "--output-format", "stream-json", "--force", "--resume", "sess_abc123", "--model", "sonnet-4.6-thinking", "continue from previous turn"}, cursorExplicitArgs)

	_, err = BuildResumeArgs("amp", "continue", "", nil)
	require.Error(t, err)
	assert.Equal(t, errors.EInvocationInvalidMode, errors.GetCode(err))

	assert.True(t, SupportsResumeTurns("claude-code"))
	assert.True(t, SupportsResumeTurns("codex"))
	assert.True(t, SupportsResumeTurns("cursor"))
	assert.False(t, SupportsResumeTurns("amp"))
	assert.False(t, SupportsResumeTurns("opencode"))
}

func TestBuildHeadedArgs(t *testing.T) {
	t.Parallel()

	claudeArgs, err := BuildHeadedArgs("claude-code", []string{"--model", "opus"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "opus"}, claudeArgs)

	codexArgs, err := BuildHeadedArgs("codex", []string{"--model", "gpt-5"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "gpt-5", "--enable", "codex_hooks"}, codexArgs)

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
