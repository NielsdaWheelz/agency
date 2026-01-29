package cobra

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
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
		t.Run(arg, func(t *testing.T) {
			stdout, _, err := executeCmd(arg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check for key elements in help output
			if !strings.Contains(stdout, "agency") {
				t.Error("expected 'agency' in help output")
			}
			if !strings.Contains(stdout, "Available Commands") {
				t.Error("expected 'Available Commands' in help output")
			}
			// Verify legacy commands are present
			for _, cmd := range []string{"run", "ls", "show", "attach", "stop", "kill"} {
				if !strings.Contains(stdout, cmd) {
					t.Errorf("expected '%s' command in help output", cmd)
				}
			}
			// Verify new v2 commands are present
			for _, cmd := range []string{"worktree", "agent", "watch"} {
				if !strings.Contains(stdout, cmd) {
					t.Errorf("expected '%s' command in help output", cmd)
				}
			}
		})
	}
}

func TestRoot_Version(t *testing.T) {
	tests := []string{"--version", "-v", "version"}
	for _, arg := range tests {
		t.Run(arg, func(t *testing.T) {
			stdout, _, err := executeCmd(arg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout, "agency") {
				t.Error("expected 'agency' in version output")
			}
		})
	}
}

func TestRoot_UnknownCommand(t *testing.T) {
	_, _, err := executeCmd("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	// Cobra returns its own error type for unknown commands
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected 'unknown command' in error, got: %v", err)
	}
}

func TestInitCmd_Help(t *testing.T) {
	stdout, _, err := executeCmd("init", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Check for key elements
	if !strings.Contains(stdout, "init") {
		t.Error("expected 'init' in help output")
	}
	if !strings.Contains(stdout, "--repo") {
		t.Error("expected '--repo' flag in help output")
	}
	if !strings.Contains(stdout, "--force") {
		t.Error("expected '--force' flag in help output")
	}
}

func TestDoctorCmd_Help(t *testing.T) {
	stdout, _, err := executeCmd("doctor", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "doctor") {
		t.Error("expected 'doctor' in help output")
	}
	if !strings.Contains(stdout, "--repo") {
		t.Error("expected '--repo' flag in help output")
	}
}

func TestRunCmd_Help(t *testing.T) {
	stdout, _, err := executeCmd("run", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify flags are documented
	for _, flag := range []string{"--name", "--runner", "--parent", "--detached"} {
		if !strings.Contains(stdout, flag) {
			t.Errorf("expected '%s' in run help output", flag)
		}
	}
}

func TestAttachCmd_Help(t *testing.T) {
	stdout, _, err := executeCmd("attach", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "attach") {
		t.Error("expected 'attach' in help output")
	}
}

func TestAttachCmd_MissingArg(t *testing.T) {
	_, _, err := executeCmd("attach")
	if err == nil {
		t.Fatal("expected error when run_id is missing")
	}
	// Cobra error for missing args
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Errorf("expected arg count error, got: %v", err)
	}
}

func TestVerifyCmd_Help(t *testing.T) {
	stdout, _, err := executeCmd("verify", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "verify") {
		t.Error("expected 'verify' in help output")
	}
	if !strings.Contains(stdout, "--timeout") {
		t.Error("expected '--timeout' flag in help output")
	}
}

func TestVerifyCmd_MissingArg(t *testing.T) {
	_, _, err := executeCmd("verify")
	if err == nil {
		t.Fatal("expected error when run_id is missing")
	}
}

// TestInit_NotInRepo tests that init fails when not in a git repo.
func TestInitCmd_NotInRepo(t *testing.T) {
	// Save and restore cwd
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWd); err != nil {
			t.Errorf("failed to restore cwd: %v", err)
		}
	})

	// Change to temp dir that is NOT a git repo
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	_, _, err = executeCmd("init")
	if err == nil {
		t.Fatal("expected error when not in git repo")
	}
	if errors.GetCode(err) != errors.ENoRepo {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.ENoRepo)
	}
}

// TestDoctor_NotInRepo tests that doctor fails when not in a git repo.
func TestDoctorCmd_NotInRepo(t *testing.T) {
	// Save and restore cwd
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWd); err != nil {
			t.Errorf("failed to restore cwd: %v", err)
		}
	})

	// Change to temp dir that is NOT a git repo
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	_, _, err = executeCmd("doctor")
	if err == nil {
		t.Fatal("expected error when not in git repo")
	}
	if errors.GetCode(err) != errors.ENoRepo {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.ENoRepo)
	}
}

// TestRun_NotInRepo tests that run fails when not in a git repo.
func TestRunCmd_NotInRepo(t *testing.T) {
	// Save and restore cwd
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWd); err != nil {
			t.Errorf("failed to restore cwd: %v", err)
		}
	})

	// Change to temp dir that is NOT a git repo
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	_, _, err = executeCmd("run", "--name", "test-run")
	if err == nil {
		t.Fatal("expected error when not in git repo")
	}
	if errors.GetCode(err) != errors.ENoRepo {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.ENoRepo)
	}
}

func TestRunCmd_MissingName(t *testing.T) {
	// Save and restore cwd
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWd); err != nil {
			t.Errorf("failed to restore cwd: %v", err)
		}
	})

	// Change to temp dir that is NOT a git repo (to trigger early exit on validation)
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	_, _, err = executeCmd("run")
	if err == nil {
		t.Fatal("expected error when --name is missing")
	}
	if errors.GetCode(err) != errors.EUsage {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.EUsage)
	}
}

// Test new v2 shell commands return E_USAGE

func TestWorktreeCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("worktree")
	if err == nil {
		t.Fatal("expected error when worktree called without subcommand")
	}
	if errors.GetCode(err) != errors.EUsage {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.EUsage)
	}
}

func TestAgentCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("agent")
	if err == nil {
		t.Fatal("expected error when agent called without subcommand")
	}
	if errors.GetCode(err) != errors.EUsage {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.EUsage)
	}
}

func TestWatchCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("watch")
	if err == nil {
		t.Fatal("expected error when watch called (not implemented)")
	}
	if errors.GetCode(err) != errors.EUsage {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.EUsage)
	}
}

// Completion tests

func TestCompletionCmd_Bash(t *testing.T) {
	stdout, _, err := executeCmd("completion", "bash")
	if err != nil {
		t.Fatalf("completion bash failed: %v", err)
	}
	// Check for key bash completion elements
	if !strings.Contains(stdout, "__agency") {
		t.Error("bash completion script missing function name")
	}
	if !strings.Contains(stdout, "complete") {
		t.Error("bash completion script missing 'complete' directive")
	}
}

func TestCompletionCmd_Zsh(t *testing.T) {
	stdout, _, err := executeCmd("completion", "zsh")
	if err != nil {
		t.Fatalf("completion zsh failed: %v", err)
	}
	// Check for key zsh completion elements
	if !strings.Contains(stdout, "#compdef") {
		t.Error("zsh completion script missing #compdef directive")
	}
}

func TestCompletionCmd_InvalidShell(t *testing.T) {
	_, _, err := executeCmd("completion", "fish")
	if err == nil {
		t.Fatal("expected error for unsupported shell")
	}
	if errors.GetCode(err) != errors.EUsage {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.EUsage)
	}
}

func TestCompletionCmd_MissingArg(t *testing.T) {
	_, _, err := executeCmd("completion")
	if err == nil {
		t.Fatal("expected error when shell is missing")
	}
}

// Test that global --verbose flag is accessible

func TestGlobalVerboseFlag(t *testing.T) {
	// Reset global opts before test
	globalOpts = GlobalOpts{}

	// Run a command with --verbose
	_, _, _ = executeCmd("--verbose", "version")

	// Check that verbose flag was set
	if !GetGlobalOpts().Verbose {
		t.Error("expected verbose flag to be set")
	}
}
