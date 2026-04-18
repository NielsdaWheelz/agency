package commands

import (
	"testing"

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
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "haiku", "--effort", "high"}, got)
}

func TestResolveEffectiveRunnerArgs_ClaudeLeavesTypedFlagsEmptyWhenUnset(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--allowed-extra"},
		"",
		"",
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra"}, got)
}

func TestResolveEffectiveRunnerArgs_CodexAppendsConfigFlag(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"codex",
		[]string{"--allowed-extra"},
		"gpt-5-codex",
		"high",
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "gpt-5-codex", "--config", "model_reasoning_effort=high"}, got)
}

func TestResolveEffectiveRunnerArgs_CursorAppendsModelOnly(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"cursor",
		[]string{"--allowed-extra"},
		"sonnet-4.6-thinking",
		"",
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
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "supported for runners")
}

func TestResolveEffectiveRunnerArgs_CursorRejectsEffort(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"cursor",
		[]string{"--allowed-extra"},
		"sonnet-4.6-thinking",
		"high",
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not supported for runner cursor")
}

func TestResolveEffectiveRunnerArgs_PassthroughArgsStayOpaque(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--model=opus", "--foo", "bar"},
		"",
		"",
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--model=opus", "--foo", "bar"}, got)
}
