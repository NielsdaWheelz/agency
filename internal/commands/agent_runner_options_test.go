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
			Model:    "opus",
			Thinking: "medium",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "haiku", "--thinking", "high"}, got)
}

func TestResolveEffectiveRunnerArgs_ClaudeUsesDefaultsWhenFlagsMissing(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"claude",
		[]string{"--allowed-extra"},
		"",
		"",
		config.UserDefaults{
			Model:    "opus",
			Thinking: "medium",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "opus", "--thinking", "medium"}, got)
}

func TestResolveEffectiveRunnerArgs_PreservesLegacyRunnerArgsWithoutDefaults(t *testing.T) {
	t.Parallel()

	input := []string{"--allowed-extra", "--model=opus", "--thinking=high"}
	got, err := resolveEffectiveRunnerArgs(
		"claude-code",
		input,
		"",
		"",
		config.UserDefaults{},
	)
	require.NoError(t, err)
	assert.Equal(t, input, got)
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

func TestResolveEffectiveRunnerArgs_NonClaudeRejectsExplicitModelThinking(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"codex",
		[]string{"--allowed-extra"},
		"gpt-5",
		"",
		config.UserDefaults{},
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "only supported")
}

func TestResolveEffectiveRunnerArgs_NonClaudeIgnoresDefaults(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"codex",
		[]string{"--allowed-extra"},
		"",
		"",
		config.UserDefaults{
			Model:    "opus",
			Thinking: "high",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra"}, got)
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

func TestHasClaudeOptionRunnerArgs_StopsAtDoubleDash(t *testing.T) {
	t.Parallel()

	assert.True(t, hasClaudeOptionRunnerArgs([]string{"--model=opus"}))
	assert.True(t, hasClaudeOptionRunnerArgs([]string{"--thinking", "high"}))
	assert.False(t, hasClaudeOptionRunnerArgs([]string{"--", "--model=opus"}))
}
