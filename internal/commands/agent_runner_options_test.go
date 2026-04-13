package commands

import (
	"testing"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveEffectiveRunnerArgs_ClaudeAppendsTypedFlagsAndKeepsPassthrough(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--allowed-extra"},
		"haiku",
		"high",
		config.UserDefaults{},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "haiku", "--effort", "high"}, got)
}

func TestResolveEffectiveRunnerArgs_ClaudeFallsBackToDefaults(t *testing.T) {
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

func TestResolveEffectiveRunnerArgs_CodexAppendsConfigFlag(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"codex",
		[]string{"--allowed-extra"},
		"gpt-5-codex",
		"high",
		config.UserDefaults{},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "gpt-5-codex", "--config", "model_reasoning_effort=high"}, got)
}

func TestResolveEffectiveRunnerArgs_CursorIgnoresEffortDefault(t *testing.T) {
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

func TestResolveEffectiveRunnerArgs_NonTypedRunnerRejectsTypedFlags(t *testing.T) {
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

func TestResolveEffectiveRunnerArgs_PassthroughArgsStayOpaque(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--model=opus", "--foo", "bar"},
		"",
		"",
		config.UserDefaults{},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--model=opus", "--foo", "bar"}, got)
}
