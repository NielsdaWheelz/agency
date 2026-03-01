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

func TestRoot_Help(t *testing.T) {
	tests := []string{"--help", "-h"}
	for _, arg := range tests {
		arg := arg
		t.Run(arg, func(t *testing.T) {
			stdout, _, err := executeCmd(arg)
			require.NoError(t, err, "unexpected error")

			// Check for key elements in help output
			assert.Contains(t, stdout, "agency")
			assert.Contains(t, stdout, "Available Commands")
			// Verify legacy commands are present
			for _, cmd := range []string{"run", "ls", "show", "attach", "stop", "kill"} {
				assert.Contains(t, stdout, cmd, "expected '%s' command in help output", cmd)
			}
			// Verify new v2 commands are present
			for _, cmd := range []string{"worktree", "agent", "watch"} {
				assert.Contains(t, stdout, cmd, "expected '%s' command in help output", cmd)
			}
		})
	}
}

func TestRoot_Version(t *testing.T) {
	tests := []string{"--version", "-v", "version"}
	for _, arg := range tests {
		arg := arg
		t.Run(arg, func(t *testing.T) {
			stdout, _, err := executeCmd(arg)
			require.NoError(t, err, "unexpected error")
			assert.Contains(t, stdout, "agency")
		})
	}
}

func TestRoot_UnknownCommand(t *testing.T) {
	_, _, err := executeCmd("nonexistent")
	require.Error(t, err, "expected error for unknown command")
	// Cobra returns its own error type for unknown commands
	assert.Contains(t, err.Error(), "unknown command")
}

func TestInitCmd_Help(t *testing.T) {
	stdout, _, err := executeCmd("init", "--help")
	require.NoError(t, err, "unexpected error")
	// Check for key elements
	assert.Contains(t, stdout, "init")
	assert.Contains(t, stdout, "--repo")
	assert.Contains(t, stdout, "--force")
}

func TestDoctorCmd_Help(t *testing.T) {
	stdout, _, err := executeCmd("doctor", "--help")
	require.NoError(t, err, "unexpected error")
	assert.Contains(t, stdout, "doctor")
	assert.Contains(t, stdout, "--repo")
}

func TestRunCmd_Help(t *testing.T) {
	stdout, _, err := executeCmd("run", "--help")
	require.NoError(t, err, "unexpected error")
	// Verify flags are documented
	for _, flag := range []string{"--name", "--runner", "--parent", "--detached"} {
		assert.Contains(t, stdout, flag, "expected '%s' in run help output", flag)
	}
}

func TestAttachCmd_Help(t *testing.T) {
	stdout, _, err := executeCmd("attach", "--help")
	require.NoError(t, err, "unexpected error")
	assert.Contains(t, stdout, "attach")
}

func TestAttachCmd_MissingArg(t *testing.T) {
	_, _, err := executeCmd("attach")
	require.Error(t, err, "expected error when run_id is missing")
	// Cobra error for missing args
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

func TestVerifyCmd_Help(t *testing.T) {
	stdout, _, err := executeCmd("verify", "--help")
	require.NoError(t, err, "unexpected error")
	assert.Contains(t, stdout, "verify")
	assert.Contains(t, stdout, "--timeout")
}

func TestVerifyCmd_MissingArg(t *testing.T) {
	_, _, err := executeCmd("verify")
	require.Error(t, err, "expected error when run_id is missing")
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

// TestRun_NotInRepo tests that run fails when not in a git repo.
func TestRunCmd_NotInRepo(t *testing.T) {
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

	_, _, err = executeCmd("run", "--name", "test-run")
	require.Error(t, err, "expected error when not in git repo")
	assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
}

func TestRunCmd_MissingName(t *testing.T) {
	// Save and restore cwd
	originalWd, err := os.Getwd()
	require.NoError(t, err, "failed to get cwd")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalWd), "failed to restore cwd")
	})

	// Change to temp dir that is NOT a git repo (to trigger early exit on validation)
	tmpDir := t.TempDir()
	err = os.Chdir(tmpDir)
	require.NoError(t, err, "failed to chdir")

	_, _, err = executeCmd("run")
	require.Error(t, err, "expected error when --name is missing")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

// Test new v2 shell commands return E_USAGE

func TestWorktreeCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("worktree")
	require.Error(t, err, "expected error when worktree called without subcommand")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestAgentCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("agent")
	require.Error(t, err, "expected error when agent called without subcommand")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestAgentCLI_RegistersCanonicalPathShellEnterSubcommands(t *testing.T) {
	rootCmd := NewRootCmd()
	agentCmd, _, err := rootCmd.Find([]string{"agent"})
	require.NoError(t, err)

	subcmds := make(map[string]bool)
	for _, sub := range agentCmd.Commands() {
		subcmds[sub.Name()] = true
	}

	assert.True(t, subcmds["path"], "agent must include canonical 'path' subcommand")
	assert.True(t, subcmds["shell"], "agent must include canonical 'shell' subcommand")
	assert.True(t, subcmds["enter"], "agent must include canonical 'enter' subcommand")
	assert.True(t, subcmds["attach"], "agent must retain 'attach' for compatibility (PR-05 owns alias rollout)")
	assert.True(t, subcmds["restart"], "agent must include canonical 'restart' subcommand in S3")
}

func TestWatchCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("watch")
	require.Error(t, err, "expected error when watch called (not implemented)")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
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

// Test that global --verbose flag is accessible

func TestGlobalVerboseFlag(t *testing.T) {
	// Reset global opts before test
	globalOpts = GlobalOpts{}

	// Run a command with --verbose
	_, _, _ = executeCmd("--verbose", "version")

	// Check that verbose flag was set
	assert.True(t, GetGlobalOpts().Verbose, "expected verbose flag to be set")
}
