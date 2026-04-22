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
	assert.Contains(t, err.Error(), "unknown command")
}

func TestRepoCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("repo")
	require.Error(t, err, "expected error when repo called without target")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestInitCmd_NotInRepo(t *testing.T) {
	originalWd, err := os.Getwd()
	require.NoError(t, err, "failed to get cwd")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalWd), "failed to restore cwd")
	})

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

func TestDoctorCmd_NotInRepo(t *testing.T) {
	originalWd, err := os.Getwd()
	require.NoError(t, err, "failed to get cwd")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalWd), "failed to restore cwd")
	})

	tmpDir := t.TempDir()
	err = os.Chdir(tmpDir)
	require.NoError(t, err, "failed to chdir")

	_, _, err = executeCmd("doctor")
	require.Error(t, err, "expected error when not in git repo")
	assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
}

func TestWorktreeCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("worktree")
	require.Error(t, err, "expected error when worktree called without target")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestAgentCmd_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCmd("agent")
	require.Error(t, err, "expected error when agent called without target")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestWatchCmd_NonInteractiveReturnsENotInteractive(t *testing.T) {
	_, _, err := executeCmd("watch")
	require.Error(t, err, "expected error when watch called non-interactively")
	assert.Equal(t, errors.ENotInteractive, errors.GetCode(err))
}

func TestCompletionCmd_Bash(t *testing.T) {
	stdout, _, err := executeCmd("completion", "bash")
	require.NoError(t, err, "completion bash failed")
	assert.Contains(t, stdout, "__agency", "bash completion script missing function name")
	assert.Contains(t, stdout, "complete", "bash completion script missing 'complete' directive")
}

func TestCompletionCmd_Zsh(t *testing.T) {
	stdout, _, err := executeCmd("completion", "zsh")
	require.NoError(t, err, "completion zsh failed")
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

func TestCompletionRepoCanonicalTargets(t *testing.T) {
	stdout, _, err := executeCmd("__complete", "repo", "a")
	require.NoError(t, err, "expected repo completion to succeed")
	assert.Contains(t, stdout, "add")
}

func TestCompletionWorktreeCanonicalTargets(t *testing.T) {
	stdout, _, err := executeCmd("__complete", "worktree", "c")
	require.NoError(t, err, "expected worktree completion to succeed")
	assert.Contains(t, stdout, "create")
}

func TestCompletionAgentCanonicalTargets(t *testing.T) {
	stdout, _, err := executeCmd("__complete", "agent", "s")
	require.NoError(t, err, "expected agent completion to succeed")
	assert.Contains(t, stdout, "start")
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
		"version": 3,
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
