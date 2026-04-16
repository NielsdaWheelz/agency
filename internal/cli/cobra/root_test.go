package cobra

import (
	"bytes"
	"os"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executeCmd runs the root command with the given args and returns stdout, stderr, and error.
func executeCmd(args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	rootCmd := NewRootCmd()
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestRoot_UnknownCommand(t *testing.T) {
	_, _, err := executeCmd("nonexistent")
	require.Error(t, err, "expected error for unknown command")
	// Cobra returns its own error type for unknown commands
	assert.Contains(t, err.Error(), "unknown command")
}

// TestInit_NotInRepo tests that init fails when not in a git repo.
func TestInitCmd_NotInRepo(t *testing.T) {
	// Save and restore cwd
	originalWd, err := os.Getwd()
	require.NoError(t, err, "failed to get cwd")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalWd), "failed to restore cwd")
	})

	// Change to temp dir that is NOT a git repo
	tmpDir := t.TempDir()
	err = os.Chdir(tmpDir)
	require.NoError(t, err, "failed to chdir")

	_, _, err = executeCmd("init")
	require.Error(t, err, "expected error when not in git repo")
	assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
}

// TestDoctor_NotInRepo tests that doctor fails when not in a git repo.
func TestDoctorCmd_NotInRepo(t *testing.T) {
	// Save and restore cwd
	originalWd, err := os.Getwd()
	require.NoError(t, err, "failed to get cwd")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalWd), "failed to restore cwd")
	})

	// Change to temp dir that is NOT a git repo
	tmpDir := t.TempDir()
	err = os.Chdir(tmpDir)
	require.NoError(t, err, "failed to chdir")

	_, _, err = executeCmd("doctor")
	require.Error(t, err, "expected error when not in git repo")
	assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
}

func TestWorktreeCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("worktree")
	require.Error(t, err, "expected error when worktree called without subcommand")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestWorktreeCmd_LegacyMergeRemoved(t *testing.T) {
	_, _, err := executeCmd("worktree", "merge")
	require.Error(t, err, "expected error when legacy top-level merge is called")
	assert.Contains(t, err.Error(), "unknown command")
}

func TestRepoCmd_HelpShowsRmSubcommand(t *testing.T) {
	stdout, _, err := executeCmd("repo", "--help")
	require.NoError(t, err, "expected repo help to render")
	assert.Contains(t, stdout, "rm")
	assert.Contains(t, stdout, "Remove a registered repository")
	assert.NotContains(t, stdout, "Subcommands:")
}

func TestRepoRmCmd_HelpShowsConfirmationFlags(t *testing.T) {
	stdout, _, err := executeCmd("repo", "rm", "--help")
	require.NoError(t, err, "expected repo rm help to render")
	assert.Contains(t, stdout, "--yes")
	assert.Contains(t, stdout, "--json")
}

func TestDaemonCmd_HelpShowsCommandsWithoutDuplicateCatalog(t *testing.T) {
	stdout, _, err := executeCmd("daemon", "--help")
	require.NoError(t, err, "expected daemon help to render")
	assert.Contains(t, stdout, "start")
	assert.Contains(t, stdout, "status")
	assert.NotContains(t, stdout, "Subcommands:")
}

func TestWorktreeCmd_HelpPointsToPRCommand(t *testing.T) {
	stdout, _, err := executeCmd("worktree", "--help")
	require.NoError(t, err, "expected worktree help to render")
	assert.Contains(t, stdout, "pr          Manage a worktree pull request")
	assert.NotContains(t, stdout, "pr merge")
	assert.NotContains(t, stdout, "Subcommands:")
}

func TestWorktreePRCmd_HelpShowsSyncAndMerge(t *testing.T) {
	stdout, _, err := executeCmd("worktree", "pr", "--help")
	require.NoError(t, err, "expected worktree pr help to render")
	assert.Contains(t, stdout, "sync")
	assert.Contains(t, stdout, "merge")
}

func TestAgentCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("agent")
	require.Error(t, err, "expected error when agent called without subcommand")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestAgentCmd_HelpShowsGroupsWithoutDuplicateCatalog(t *testing.T) {
	stdout, _, err := executeCmd("agent", "--help")
	require.NoError(t, err, "expected agent help to render")
	assert.Contains(t, stdout, "Run\n  chat")
	assert.Contains(t, stdout, "Inspect\n  diff")
	assert.Contains(t, stdout, "Navigate\n  enter")
	assert.Contains(t, stdout, "Recover\n  checkpoint")
	assert.Contains(t, stdout, "Finish\n  discard")
	assert.NotContains(t, stdout, "Subcommands:")
}

func TestAgentCheckpointCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("agent", "checkpoint")
	require.Error(t, err, "expected error when agent checkpoint called without subcommand")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestAgentCheckpointCmd_HelpShowsCommandsWithoutDuplicateCatalog(t *testing.T) {
	stdout, _, err := executeCmd("agent", "checkpoint", "--help")
	require.NoError(t, err, "expected agent checkpoint help to render")
	assert.Contains(t, stdout, "apply")
	assert.Contains(t, stdout, "ls")
	assert.NotContains(t, stdout, "Subcommands:")
}

func TestCheckpointCmd_UnknownAtRoot(t *testing.T) {
	_, _, err := executeCmd("checkpoint")
	require.Error(t, err, "expected error when checkpoint called at root")
	assert.Contains(t, err.Error(), "unknown command")
}

func TestAgentCheckpointApply_InvalidCheckpointID_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("agent", "checkpoint", "apply", "inv-1", "abc")
	require.Error(t, err, "expected error for non-numeric checkpoint id")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestWatchCmd_NonInteractiveReturnsENotInteractive(t *testing.T) {
	_, _, err := executeCmd("watch")
	require.Error(t, err, "expected error when watch called non-interactively")
	assert.Equal(t, errors.ENotInteractive, errors.GetCode(err))
}

// Completion tests

func TestCompletionCmd_Bash(t *testing.T) {
	stdout, _, err := executeCmd("completion", "bash")
	require.NoError(t, err, "completion bash failed")
	// Check for key bash completion elements
	assert.Contains(t, stdout, "__agency", "bash completion script missing function name")
	assert.Contains(t, stdout, "complete", "bash completion script missing 'complete' directive")
}

func TestCompletionCmd_Zsh(t *testing.T) {
	stdout, _, err := executeCmd("completion", "zsh")
	require.NoError(t, err, "completion zsh failed")
	// Check for key zsh completion elements
	assert.Contains(t, stdout, "#compdef", "zsh completion script missing #compdef directive")
}

func TestCompletionCmd_InvalidShell(t *testing.T) {
	_, _, err := executeCmd("completion", "fish")
	require.Error(t, err, "expected error for unsupported shell")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestCompletionCmd_MissingArg(t *testing.T) {
	_, _, err := executeCmd("completion")
	require.Error(t, err, "expected error when shell is missing")
}
