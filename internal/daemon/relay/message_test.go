package relay

import (
	"encoding/json"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatStdinMessage_ClaudeCode(t *testing.T) {
	t.Parallel()

	line, err := FormatStdinMessage(runners.RunnerClaudeCode, "fix the bug")
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(line, &parsed))

	assert.Equal(t, "user", parsed["type"])

	msg := parsed["message"].(map[string]any)
	assert.Equal(t, "user", msg["role"])

	content := msg["content"].([]any)
	require.Len(t, content, 1)
	block := content[0].(map[string]any)
	assert.Equal(t, "text", block["type"])
	assert.Equal(t, "fix the bug", block["text"])
}

func TestFormatStdinMessage_Amp(t *testing.T) {
	t.Parallel()

	line, err := FormatStdinMessage(runners.RunnerAmp, "refactor this")
	require.NoError(t, err)

	var parsed claudeStyleMessage
	require.NoError(t, json.Unmarshal(line, &parsed))
	assert.Equal(t, "user", parsed.Type)
	assert.Equal(t, "user", parsed.Message.Role)
	require.Len(t, parsed.Message.Content, 1)
	assert.Equal(t, "refactor this", parsed.Message.Content[0].Text)
}

func TestFormatStdinMessage_Droid(t *testing.T) {
	t.Parallel()

	line, err := FormatStdinMessage(runners.RunnerDroid, "deploy it")
	require.NoError(t, err)

	var parsed claudeStyleMessage
	require.NoError(t, json.Unmarshal(line, &parsed))
	assert.Equal(t, "user", parsed.Type)
	assert.Equal(t, "deploy it", parsed.Message.Content[0].Text)
}

func TestFormatStdinMessage_UnsupportedRunner(t *testing.T) {
	t.Parallel()

	_, err := FormatStdinMessage(runners.RunnerCodex, "hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support stdin message format")
}

func TestFormatStdinMessage_ValidJSON(t *testing.T) {
	t.Parallel()

	// Ensure special characters are properly escaped.
	line, err := FormatStdinMessage(runners.RunnerClaudeCode, `prompt with "quotes" and \backslash and 日本語`)
	require.NoError(t, err)
	assert.True(t, json.Valid(line), "output must be valid JSON")

	var parsed claudeStyleMessage
	require.NoError(t, json.Unmarshal(line, &parsed))
	assert.Equal(t, `prompt with "quotes" and \backslash and 日本語`, parsed.Message.Content[0].Text)
}

func TestFormatStdinMessage_EmptyPrompt(t *testing.T) {
	t.Parallel()

	// Empty prompt is technically valid JSON — the caller should validate before calling.
	line, err := FormatStdinMessage(runners.RunnerClaudeCode, "")
	require.NoError(t, err)
	assert.True(t, json.Valid(line))
}

func TestFormatStdinMessage_NoNewline(t *testing.T) {
	t.Parallel()

	// FormatStdinMessage returns the JSON payload WITHOUT a trailing newline.
	// The caller (StdinRelay.Send) appends the newline.
	line, err := FormatStdinMessage(runners.RunnerClaudeCode, "hello")
	require.NoError(t, err)
	assert.NotEqual(t, byte('\n'), line[len(line)-1], "FormatStdinMessage must not include trailing newline")
}

func TestFormatStdinMessage_AllStdinRunners(t *testing.T) {
	t.Parallel()

	stdinRunners := []string{runners.RunnerClaudeCode, runners.RunnerAmp, runners.RunnerDroid}
	for _, runner := range stdinRunners {
		t.Run(runner, func(t *testing.T) {
			t.Parallel()
			line, err := FormatStdinMessage(runner, "test prompt")
			require.NoError(t, err)
			assert.True(t, json.Valid(line))
		})
	}
}

func TestFormatStdinMessage_AllResumeRunners(t *testing.T) {
	t.Parallel()

	resumeRunners := []string{runners.RunnerCodex, runners.RunnerOpenCode, runners.RunnerCursor}
	for _, runner := range resumeRunners {
		t.Run(runner, func(t *testing.T) {
			t.Parallel()
			_, err := FormatStdinMessage(runner, "test prompt")
			require.Error(t, err, "resume-based runners must not support stdin format")
		})
	}
}
