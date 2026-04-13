package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestResolveEffectiveRunnerArgs_ClaudeUsesCLIOverDefaults(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--allowed-extra"},
		"haiku",
		"high",
		config.UserDefaults{
			Model:  "opus",
			Effort: "medium",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "haiku", "--effort", "high"}, got)
}

func TestResolveEffectiveRunnerArgs_ClaudeUsesDefaultsWhenFlagsMissing(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--allowed-extra"},
		"",
		"",
		config.UserDefaults{
			Model:  "opus",
			Effort: "medium",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "opus", "--effort", "medium"}, got)
}

func TestResolveEffectiveRunnerArgs_InvalidRunnerRejectsEvenWithoutTypedFlags(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"unknown-runner",
		[]string{"--allowed-extra"},
		"",
		"",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "invalid runner")
}

func TestResolveEffectiveRunnerArgs_ConflictingModelAcrossSourcesFails(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--model=opus"},
		"haiku",
		"",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "conflicts")
}

func TestResolveEffectiveRunnerArgs_DuplicateModelInRunnerArgsFails(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--model=opus", "--model=haiku"},
		"",
		"",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "conflicting --model values")
}

func TestResolveEffectiveRunnerArgs_DuplicateModelSameValueFails(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--model=opus", "--model=opus"},
		"",
		"",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "duplicate --model")
}

func TestResolveEffectiveRunnerArgs_NonTypedRunnerRejectsExplicitModelEffort(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"amp",
		[]string{"--allowed-extra"},
		"amp-fast",
		"",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "supported for runners")
}

func TestResolveEffectiveRunnerArgs_NonTypedRunnerIgnoresDefaults(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"amp",
		[]string{"--allowed-extra"},
		"",
		"",
		config.UserDefaults{
			Model:  "opus",
			Effort: "high",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra"}, got)
}

func TestResolveEffectiveRunnerArgs_CodexUsesCLIOverDefaults(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"codex",
		[]string{"--allowed-extra"},
		"gpt-5-codex",
		"high",
		config.UserDefaults{
			Model:  "gpt-5",
			Effort: "medium",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "gpt-5-codex", "--config", "model_reasoning_effort=high"}, got)
}

func TestResolveEffectiveRunnerArgs_CodexUsesDefaultsWhenFlagsMissing(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"codex",
		[]string{"--allowed-extra"},
		"",
		"",
		config.UserDefaults{
			Model:  "gpt-5-codex",
			Effort: "minimal",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "gpt-5-codex", "--config", "model_reasoning_effort=minimal"}, got)
}

func TestResolveEffectiveRunnerArgs_CodexConflictingModelAcrossSourcesFails(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"codex",
		[]string{"--model=gpt-5"},
		"gpt-5-codex",
		"",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "conflicts")
}

func TestResolveEffectiveRunnerArgs_CodexConflictingEffortAcrossSourcesFails(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"codex",
		[]string{"--config", "model_reasoning_effort=low"},
		"",
		"high",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "conflicts")
}

func TestResolveEffectiveRunnerArgs_CursorUsesCLIModelOverDefaults(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"cursor",
		[]string{"--allowed-extra"},
		"sonnet-4.6-thinking",
		"",
		config.UserDefaults{
			Model:  "opus-4.6-thinking",
			Effort: "high",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "sonnet-4.6-thinking"}, got)
}

func TestResolveEffectiveRunnerArgs_CursorUsesModelDefaultAndIgnoresEffortDefault(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"cursor",
		[]string{"--allowed-extra"},
		"",
		"",
		config.UserDefaults{
			Model:  "sonnet-4.6-thinking",
			Effort: "high",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "sonnet-4.6-thinking"}, got)
}

func TestResolveEffectiveRunnerArgs_CursorConflictingModelAcrossSourcesFails(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"cursor",
		[]string{"--model=opus-4.6-thinking"},
		"sonnet-4.6-thinking",
		"",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "conflicts")
}

func TestResolveEffectiveRunnerArgs_CursorRejectsEffortFlag(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"cursor",
		[]string{"--allowed-extra"},
		"",
		"high",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not supported")
}

func TestResolveEffectiveRunnerArgs_CursorRejectsEffortRunnerArg(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"cursor",
		[]string{"--effort=high"},
		"",
		"",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not supported")
}

func TestResolveEffectiveRunnerArgs_RejectsMissingRunnerArgValue(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--model"},
		"",
		"",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "requires a value")
}

func TestResolveEffectiveRunnerArgs_CodexRejectsMissingShortModelRunnerArgValue(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"codex",
		[]string{"-m"},
		"",
		"",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "requires a value")
}

func TestHasTypedOptionRunnerArgs_StopsAtDoubleDash(t *testing.T) {
	t.Parallel()

	assert.True(t, hasTypedOptionRunnerArgs([]string{"--model=opus"}))
	assert.True(t, hasTypedOptionRunnerArgs([]string{"--effort", "high"}))
	assert.True(t, hasTypedOptionRunnerArgs([]string{"-m", "gpt-5"}))
	assert.True(t, hasTypedOptionRunnerArgs([]string{"--config", "model_reasoning_effort=high"}))
	assert.False(t, hasTypedOptionRunnerArgs([]string{"--thinking", "high"}))
	assert.False(t, hasTypedOptionRunnerArgs([]string{"--", "--model=opus"}))
}
