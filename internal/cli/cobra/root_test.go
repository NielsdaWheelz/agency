package cobra

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
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

func TestConfigInitCmd_SucceedsOutsideRepo(t *testing.T) {
	originalWd, err := os.Getwd()
	require.NoError(t, err, "failed to get cwd")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalWd), "failed to restore cwd")
	})

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "agency-config")
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755), "failed to create fake bin dir")
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o755), "failed to create fake codex")
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "code"), []byte("#!/bin/sh\nexit 0\n"), 0o755), "failed to create fake code")

	t.Setenv("PATH", binDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	require.NoError(t, os.Chdir(tmpDir), "failed to chdir to non-repo dir")

	stdout, _, err := executeCmd("config", "init")
	require.NoError(t, err, "expected config init to succeed outside a repo")
	assert.Contains(t, stdout, "user_config: created")
	assert.FileExists(t, filepath.Join(configDir, "config.json"))
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
	assert.Contains(t, stdout, "agency repo <repo-ref> rm --yes")
	assert.NotContains(t, stdout, "\n  rm")
	assert.NotContains(t, stdout, "Subcommands:")
}

func TestNounAliases_ReturnSameUsageAsCanonical(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		canonical string
		alias     string
	}{
		{name: "repo", canonical: "repo", alias: "r"},
		{name: "worktree", canonical: "worktree", alias: "wt"},
		{name: "agent", canonical: "agent", alias: "ag"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wantStdout, wantStderr, wantErr := executeCmd(tt.canonical)
			require.Error(t, wantErr, "expected %s without subcommand to fail", tt.canonical)

			gotStdout, gotStderr, gotErr := executeCmd(tt.alias)
			require.Error(t, gotErr, "expected %s without subcommand to fail", tt.alias)

			assert.Equal(t, errors.GetCode(wantErr), errors.GetCode(gotErr))
			assert.Equal(t, wantErr.Error(), gotErr.Error())
			assert.Equal(t, wantStdout, gotStdout)
			assert.Equal(t, wantStderr, gotStderr)
		})
	}
}

func TestNounAliases_HelpMatchesCanonical(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		canonical string
		alias     string
	}{
		{name: "repo", canonical: "repo", alias: "r"},
		{name: "worktree", canonical: "worktree", alias: "wt"},
		{name: "agent", canonical: "agent", alias: "ag"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wantStdout, wantStderr, wantErr := executeCmd(tt.canonical, "--help")
			require.NoError(t, wantErr, "expected %s help to render", tt.canonical)

			gotStdout, gotStderr, gotErr := executeCmd(tt.alias, "--help")
			require.NoError(t, gotErr, "expected %s help to render", tt.alias)

			assert.Equal(t, wantStdout, gotStdout)
			assert.Equal(t, wantStderr, gotStderr)
		})
	}
}

func TestConfigCmd_HelpShowsInitSubcommand(t *testing.T) {
	stdout, _, err := executeCmd("config", "--help")
	require.NoError(t, err, "expected config help to render")
	assert.Contains(t, stdout, "init")
	assert.Contains(t, stdout, "Manage user-scoped agency configuration")
}

func TestConfigInitCmd_HelpShowsForceAndNoRepoBehavior(t *testing.T) {
	stdout, _, err := executeCmd("config", "init", "--help")
	require.NoError(t, err, "expected config init help to render")
	assert.Contains(t, stdout, "--force")
	assert.Contains(t, stdout, "works without a repo")
	assert.Contains(t, stdout, "writes only AGENCY_CONFIG_DIR/config.json")
}

func TestInitCmd_HelpPointsToConfigInit(t *testing.T) {
	stdout, _, err := executeCmd("init", "--help")
	require.NoError(t, err, "expected init help to render")
	assert.Contains(t, stdout, "requires user config")
	assert.Contains(t, stdout, "agency config init")
	assert.Contains(t, stdout, "--path")
	assert.NotContains(t, stdout, "--repo string")
	assert.NotContains(t, stdout, "--local")
}

func TestDoctorCmd_HelpPointsToConfigInit(t *testing.T) {
	stdout, _, err := executeCmd("doctor", "--help")
	require.NoError(t, err, "expected doctor help to render")
	assert.Contains(t, stdout, "agency config init")
	assert.Contains(t, stdout, "--path")
	assert.NotContains(t, stdout, "--repo")
}

func TestRepoAddCmd_HelpUsesPositionalPath(t *testing.T) {
	stdout, _, err := executeCmd("repo", "add", "--help")
	require.NoError(t, err, "expected repo add help to render")
	assert.Contains(t, stdout, "agency repo add [path]")
	assert.Contains(t, stdout, "agency repo add /home/user/myrepo")
	assert.NotContains(t, stdout, "--path")
}

func TestRoot_HelpExamplesUseTargetFirstGrammar(t *testing.T) {
	stdout, _, err := executeCmd("--help")
	require.NoError(t, err, "expected root help to render")
	assert.Contains(t, stdout, "agency worktree create fix-help --repo agency --base main")
	assert.Contains(t, stdout, "agency context use fix-help --repo agency")
	assert.Contains(t, stdout, "agency agent start")
	assert.NotContains(t, stdout, "agency worktree create --repo agency --name fix-help --base main")
	assert.NotContains(t, stdout, "agency agent start fix-help --repo agency")
}

func TestRepoRmCmd_HelpShowsConfirmationFlags(t *testing.T) {
	stdout, _, err := executeCmd("repo", "agency", "rm", "--help")
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
	assert.Contains(t, stdout, "agency worktree <worktree-ref>")
	assert.Contains(t, stdout, "agency worktree ls")
	assert.NotContains(t, stdout, "agency worktree ls/show")
	assert.NotContains(t, stdout, "agency worktree show")
	assert.Contains(t, stdout, "agency worktree <worktree-ref> pr sync")
	assert.NotContains(t, stdout, "\n  pr")
	assert.NotContains(t, stdout, "Subcommands:")
}

func TestWorktreePRCmd_HelpShowsSyncAndMerge(t *testing.T) {
	syncHelp, _, err := executeCmd("worktree", "my-feature", "pr", "sync", "--help")
	require.NoError(t, err, "expected worktree pr sync help to render")
	assert.Contains(t, syncHelp, "agency worktree my-feature pr sync")
	assert.Contains(t, syncHelp, "--force-with-lease")

	mergeHelp, _, err := executeCmd("worktree", "my-feature", "pr", "merge", "--help")
	require.NoError(t, err, "expected worktree pr merge help to render")
	assert.Contains(t, mergeHelp, "agency worktree my-feature pr merge")
	assert.Contains(t, mergeHelp, "--yes")
}

func TestAgentCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("agent")
	require.Error(t, err, "expected error when agent called without subcommand")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestAgentCmd_HelpShowsGroupsWithoutDuplicateCatalog(t *testing.T) {
	stdout, _, err := executeCmd("agent", "--help")
	require.NoError(t, err, "expected agent help to render")
	assert.Contains(t, stdout, "agency agent <invocation-ref>")
	assert.Contains(t, stdout, "agency agent ls")
	assert.NotContains(t, stdout, "agency agent ls/show")
	assert.NotContains(t, stdout, "agency agent show")
	assert.NotContains(t, stdout, "Manage invocation checkpoints")
	assert.NotContains(t, stdout, "Restart headless invocation from checkpoint/history")
	assert.NotContains(t, stdout, "View invocation logs")
	assert.NotContains(t, stdout, "Subcommands:")
}

func TestAgentCheckpointCmd_LegacyCommandRemoved(t *testing.T) {
	_, _, err := executeCmd("agent", "checkpoint")
	require.Error(t, err, "expected error when legacy agent checkpoint is called")
	assert.Contains(t, err.Error(), "unknown command")
}

func TestAgentRestartCmd_LegacyCommandRemoved(t *testing.T) {
	_, _, err := executeCmd("agent", "restart")
	require.Error(t, err, "expected error when legacy agent restart is called")
	assert.Contains(t, err.Error(), "unknown command")
}

func TestAgentLogsCmd_LegacyTopLevelCommandRemoved(t *testing.T) {
	_, _, err := executeCmd("agent", "logs")
	require.Error(t, err, "expected error when legacy top-level agent logs is called")
	assert.Contains(t, err.Error(), "unknown command")
}

func TestWorktreeVerbFirstTargetActionsRemoved(t *testing.T) {
	_, _, showErr := executeCmd("worktree", "show", "wt-1")
	require.Error(t, showErr, "expected legacy worktree show to be removed")
	assert.Contains(t, showErr.Error(), "unknown command")

	_, _, openErr := executeCmd("worktree", "open", "wt-1")
	require.Error(t, openErr, "expected legacy worktree open to be removed")
	assert.Contains(t, openErr.Error(), "unknown command")
}

func TestAgentVerbFirstTargetActionsRemoved(t *testing.T) {
	_, _, showErr := executeCmd("agent", "show", "inv-1")
	require.Error(t, showErr, "expected legacy agent show to be removed")
	assert.Contains(t, showErr.Error(), "unknown command")

	_, _, killErr := executeCmd("agent", "kill", "inv-1")
	require.Error(t, killErr, "expected legacy agent kill to be removed")
	assert.Contains(t, killErr.Error(), "unknown command")
}

func TestCheckpointCmd_UnknownAtRoot(t *testing.T) {
	_, _, err := executeCmd("checkpoint")
	require.Error(t, err, "expected error when checkpoint called at root")
	assert.Contains(t, err.Error(), "unknown command")
}

func TestAgentRestoreCmd_HelpShowsRestoreOnlySelectors(t *testing.T) {
	stdout, _, err := executeCmd("agent", "inv-1", "restore", "--help")
	require.NoError(t, err, "expected agent restore help to render")
	assert.Contains(t, stdout, "--checkpoint")
	assert.Contains(t, stdout, "--turn")
	assert.Contains(t, stdout, "--repo")
	assert.Contains(t, stdout, "--json")
	assert.NotContains(t, stdout, "--history")
	assert.NotContains(t, stdout, "--runner-arg")
	assert.NotContains(t, stdout, "--model")
	assert.NotContains(t, stdout, "--effort")
	assert.NotContains(t, stdout, "--env")
}

func TestAgentHistoryCmd_HelpShowsLogsSubcommand(t *testing.T) {
	stdout, _, err := executeCmd("agent", "inv-1", "history", "--help")
	require.NoError(t, err, "expected agent history help to render")
	assert.Contains(t, stdout, "logs")
	assert.Contains(t, stdout, "--json")
	assert.Contains(t, stdout, "canonical inspection surface")
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

func TestNounAliases_CompletionMatchesCanonical(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		canonical string
		alias     string
		prefix    string
	}{
		{name: "repo", canonical: "repo", alias: "r", prefix: "a"},
		{name: "worktree", canonical: "worktree", alias: "wt", prefix: "c"},
		{name: "agent", canonical: "agent", alias: "ag", prefix: "s"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wantStdout, wantStderr, wantErr := executeCmd("__complete", tt.canonical, tt.prefix)
			require.NoError(t, wantErr, "expected %s completion to succeed", tt.canonical)

			gotStdout, gotStderr, gotErr := executeCmd("__complete", tt.alias, tt.prefix)
			require.NoError(t, gotErr, "expected %s completion to succeed", tt.alias)

			assert.Equal(t, wantStdout, gotStdout)
			assert.Equal(t, wantStderr, gotStderr)
		})
	}
}

func TestCompletion_DynamicRepoFlag(t *testing.T) {
	dataDir, configDir, client := startCompletionTestDaemon(t)
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	repoDir := setupCompletionTestRepo(t)
	_, err := client.RegisterRepo(context.Background(), repoDir)
	require.NoError(t, err, "expected repo registration to succeed")

	stdout, _, err := executeCmd("__complete", "agent", "start", "--repo", "ag")
	require.NoError(t, err, "expected dynamic repo completion to succeed")
	assert.Contains(t, stdout, "agency")
}

func TestCompletion_EnumFlag(t *testing.T) {
	stdout, _, err := executeCmd("__complete", "agent", "inv-1", "history", "l")
	require.NoError(t, err, "expected nested target-first completion to succeed")
	assert.Contains(t, stdout, "logs")
}

func startCompletionTestDaemon(t *testing.T) (string, string, *daemonclient.Client) {
	t.Helper()

	dataDir, err := os.MkdirTemp("", "agency-complete-data-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })

	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	cfg := map[string]any{
		"version": 2,
		"defaults": map[string]string{
			"runner": "claude-code",
			"editor": "code",
		},
		"runners": map[string]string{
			"claude-code": "/bin/echo",
		},
	}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), cfgBytes, 0o644))

	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)
	srv := daemon.NewServer(st, exec.NewRealRunner(), fsys, configDir)

	socketPath := st.DaemonSocketPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(socketPath), 0o755))
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()

	client := daemonclient.NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.WaitForReady(ctx, 5*time.Second), "daemon not ready")

	t.Cleanup(func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
		<-serveDone
	})

	return dataDir, configDir, client
}

func setupCompletionTestRepo(t *testing.T) string {
	t.Helper()
	testutil.HermeticGitEnv(t)

	repoDir := t.TempDir()
	cr := exec.NewRealRunner()
	ctx := context.Background()

	result, err := cr.Run(ctx, "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git init: %s", result.Stderr)

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test\n"), 0o644))

	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode)

	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "init"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git commit: %s", result.Stderr)

	result, err = cr.Run(ctx, "git", []string{"remote", "add", "origin", "git@github.com:owner/agency.git"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git remote add origin: %s", result.Stderr)

	return repoDir
}
