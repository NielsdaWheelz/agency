package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/NielsdaWheelz/agency/internal/runners"
)

func TestMergeRestartRunnerArgs_NoRequestArgs_UsesStoredArgs(t *testing.T) {
	t.Parallel()

	stored := []string{"--foo", "bar"}
	got := mergeRestartRunnerArgs(runners.RunnerClaudeCode, stored, nil)

	assert.Equal(t, stored, got)
}

func TestMergeRestartRunnerArgs_GenericRequestArgs_AreAppended(t *testing.T) {
	t.Parallel()

	stored := []string{"--model", "sonnet", "--foo", "bar"}
	request := []string{"--cache", "on"}
	got := mergeRestartRunnerArgs(runners.RunnerCursor, stored, request)

	assert.Equal(t, []string{"--model", "sonnet", "--foo", "bar", "--cache", "on"}, got)
}

func TestMergeRestartRunnerArgs_ClaudeTypedArgs_ReplaceStored(t *testing.T) {
	t.Parallel()

	stored := []string{"--model", "sonnet", "--effort", "medium", "--foo", "bar"}
	request := []string{"--model", "opus", "--effort", "high"}
	got := mergeRestartRunnerArgs(runners.RunnerClaudeCode, stored, request)

	assert.Equal(t, request, got)
}

func TestMergeRestartRunnerArgs_CursorTypedArgs_ReplaceStored(t *testing.T) {
	t.Parallel()

	stored := []string{"--model", "sonnet", "--foo", "bar"}
	request := []string{"--model", "sonnet-4.6-thinking"}
	got := mergeRestartRunnerArgs(runners.RunnerCursor, stored, request)

	assert.Equal(t, request, got)
}

func TestMergeRestartRunnerArgs_CodexEffortConfig_ReplaceStored(t *testing.T) {
	t.Parallel()

	stored := []string{"--model", "gpt-5", "--foo", "bar"}
	request := []string{"--config", "model_reasoning_effort=high", "--sandbox", "workspace-write"}
	got := mergeRestartRunnerArgs(runners.RunnerCodex, stored, request)

	assert.Equal(t, request, got)
}

func TestMergeRestartRunnerArgs_StopTokenPreventsTypedDetection(t *testing.T) {
	t.Parallel()

	stored := []string{"--model", "sonnet"}
	request := []string{"--", "--model", "opus"}
	got := mergeRestartRunnerArgs(runners.RunnerClaudeCode, stored, request)

	assert.Equal(t, []string{"--model", "sonnet", "--", "--model", "opus"}, got)
}
