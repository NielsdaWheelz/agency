package cobra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestWorktreeCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("worktree")
	require.Error(t, err, "expected error when worktree called without target")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestWorktreeHelpSurfacesNavigationActions(t *testing.T) {
	stdout, _, err := executeCmd("worktree", "--help")
	require.NoError(t, err)
	assert.Contains(t, stdout, "agency worktree <worktree-ref> path")
	assert.Contains(t, stdout, "agency worktree <worktree-ref> shell")
}

func TestWorktreeLS_CreateFlagsAreRejected(t *testing.T) {
	_, _, err := executeCmd("worktree", "ls", "--base", "main")
	require.Error(t, err, "expected create-only flags to be rejected by worktree ls")
	assert.Contains(t, err.Error(), "unknown flag: --base")
}

func TestWorktreeTarget_CreateFlagsAreRejected(t *testing.T) {
	_, _, err := executeCmd("worktree", "wt-1", "--base", "main")
	require.Error(t, err, "expected create-only flags to be rejected by target-first worktree commands")
	assert.Contains(t, err.Error(), "unknown flag: --base")
}

func TestWorktreePRMerge_StrategyFlagsAreMutuallyExclusive(t *testing.T) {
	_, _, err := executeCmd("worktree", "wt-1", "pr", "merge", "--squash", "--merge")
	require.Error(t, err, "expected --squash and --merge to be rejected together")
	assert.Contains(t, err.Error(), "[squash merge rebase]")
}

func TestWorktreeTarget_ActionFlagsAreRejectedOutsideAction(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantMessage string
	}{
		{
			name:        "show rejects force",
			args:        []string{"worktree", "wt-1", "--force"},
			wantMessage: "--force is not valid for agency worktree <worktree-ref>",
		},
		{
			name:        "path rejects editor",
			args:        []string{"worktree", "wt-1", "path", "--editor", "code"},
			wantMessage: "--editor is not valid for agency worktree path",
		},
		{
			name:        "pr sync rejects merge strategy",
			args:        []string{"worktree", "wt-1", "pr", "sync", "--squash"},
			wantMessage: "--squash is not valid for agency worktree pr sync",
		},
		{
			name:        "pr merge rejects sync flag",
			args:        []string{"worktree", "wt-1", "pr", "merge", "--allow-dirty"},
			wantMessage: "--allow-dirty is not valid for agency worktree pr merge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := executeCmd(tt.args...)
			require.Error(t, err)
			assert.Equal(t, errors.EUsage, errors.GetCode(err))
			assert.Contains(t, err.Error(), tt.wantMessage)
		})
	}
}

func TestCompletionWorktreeCanonicalTargets(t *testing.T) {
	dataDir, configDir := setIsolatedCompletionEnv(t)

	stdout, _, err := executeCmd("__complete", "worktree", "c")
	require.NoError(t, err, "expected worktree completion to succeed")
	assert.Contains(t, stdout, "create")
	assert.NoDirExists(t, dataDir)
	assert.NoDirExists(t, configDir)
}

func TestCompletionWorktreeTargetActions(t *testing.T) {
	dataDir, configDir := setIsolatedCompletionEnv(t)

	stdout, _, err := executeCmd("__complete", "worktree", "wt-1", "")
	require.NoError(t, err, "expected worktree target action completion to succeed")
	assert.ElementsMatch(t, worktreeTargetActionCompletions(), completionValues(stdout))
	assert.NoDirExists(t, dataDir)
	assert.NoDirExists(t, configDir)
}

func TestCompletionWorktreePRTargetActions(t *testing.T) {
	dataDir, configDir := setIsolatedCompletionEnv(t)

	stdout, _, err := executeCmd("__complete", "worktree", "wt-1", "pr", "")
	require.NoError(t, err, "expected worktree pr target action completion to succeed")
	assert.ElementsMatch(t, worktreeTargetPRActionCompletions(), completionValues(stdout))
	assert.NoDirExists(t, dataDir)
	assert.NoDirExists(t, configDir)
}
