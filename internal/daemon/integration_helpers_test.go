package daemon_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

type testDaemonTmuxClient interface {
	HasSession(ctx context.Context, name string) (bool, error)
	NewSession(ctx context.Context, name, cwd string, argv []string, env map[string]string) error
	KillSession(ctx context.Context, name string) error
	InterruptSession(ctx context.Context, name string) error
	CaptureScrollback(ctx context.Context, target string) (string, error)
	PipePane(ctx context.Context, target, logPath string) error
	ListAttachedClients(ctx context.Context, name string) ([]tmux.AttachedClient, error)
}

// fakeRunnerPath returns the path to the compiled fake runner binary.
// Set by TestMain in testmain_test.go via environment variable.
func fakeRunnerPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("TEST_FAKE_RUNNER_PATH")
	require.NotEmpty(t, p, "TEST_FAKE_RUNNER_PATH not set; TestMain should compile fakerunner")
	return p
}

// testDaemonEnv holds the daemon server, client, and paths for a test.
type testDaemonEnv struct {
	Client     *daemonclient.Client
	Server     *daemon.Server
	DataDir    string
	Store      *store.Store
	SocketPath string
}

type fakeRunnerLaunchCapture struct {
	Args           []string          `json:"args"`
	CWD            string            `json:"cwd"`
	Mode           string            `json:"mode"`
	FirstStdinLine string            `json:"first_stdin_line,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
}

// startTestDaemon boots a real daemon server on a temp Unix socket and
// returns a connected client. The server and socket are cleaned up when
// the test finishes.
func startTestDaemon(t *testing.T, tmuxClients ...testDaemonTmuxClient) *testDaemonEnv {
	t.Helper()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755), "mkdir config")

	writeDaemonUserConfig(t, configDir, map[string]map[string]string{
		"personal": testutil.GitIdentityEnv(),
	})

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	var tmuxClient testDaemonTmuxClient
	if len(tmuxClients) > 0 {
		tmuxClient = tmuxClients[0]
	}
	runner := exec.NewRealRunner()
	if tmuxClient != nil {
		runner = testutil.NewFakeTmuxCommandRunner(runner, tmuxClient)
	}
	srv := daemon.NewServer(st, runner, fs.NewRealFS(), configDir)

	// Unix sockets on macOS have a ~104 byte path limit.
	// Use a short temp dir for the socket to avoid exceeding it.
	sockDir, err := os.MkdirTemp("/tmp", "dsock")
	require.NoError(t, err, "mkdir sockdir")
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })

	socketPath := filepath.Join(sockDir, "d.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err, "listen")

	// Serve in background goroutine.
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- srv.Serve(listener)
	}()

	client := daemonclient.NewClient(socketPath)

	// Wait for daemon to be ready.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.WaitForReady(ctx, 5*time.Second), "daemon not ready")

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		// Wait for Serve to return.
		<-serveDone
	})

	return &testDaemonEnv{
		Client:     client,
		Server:     srv,
		DataDir:    dataDir,
		Store:      st,
		SocketPath: socketPath,
	}
}

func writeDaemonUserConfig(t *testing.T, configDir string, profiles map[string]map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(configDir, 0o755), "mkdir config")

	profileData := map[string]any{}
	for name, env := range profiles {
		profileData[name] = map[string]any{"env": env}
	}
	if _, ok := profileData["personal"]; !ok {
		profileData["personal"] = map[string]any{"env": testutil.GitIdentityEnv()}
	}

	cfg := map[string]any{
		"version": 4,
		"defaults": map[string]string{
			"runner":            "claude-code",
			"editor":            "code",
			"execution_profile": "personal",
		},
		"runners": map[string]string{
			"claude-code": fakeRunnerPath(t),
			"codex":       fakeRunnerPath(t),
			"amp":         fakeRunnerPath(t),
			"opencode":    fakeRunnerPath(t),
			"cursor":      fakeRunnerPath(t),
			"droid":       fakeRunnerPath(t),
		},
		"execution_profiles": profileData,
	}
	cfgBytes, _ := json.Marshal(cfg)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), cfgBytes, 0o644), "write config.json")
}

// setupTestGitRepo creates a temporary git repo with one initial commit.
func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	return setupTestGitRepoParallel(t)
}

// hermeticGitEnv returns per-command env vars that isolate git from the host
// config. Unlike HermeticGitEnv, this does NOT call t.Setenv, so the calling
// test can use t.Parallel().
func hermeticGitEnv(t *testing.T) map[string]string {
	t.Helper()
	env := map[string]string{
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL":   filepath.Join(t.TempDir(), "gitconfig"),
	}
	for k, v := range testutil.GitIdentityEnv() {
		env[k] = v
	}
	return env
}

// setupTestGitRepoParallel creates a temp git repo without touching process
// env (no t.Setenv), making it safe for use in t.Parallel() tests. Git config
// isolation is achieved via per-command RunOpts.Env.
func setupTestGitRepoParallel(t *testing.T) string {
	t.Helper()

	gitEnv := hermeticGitEnv(t)
	repoDir := t.TempDir()
	cr := exec.NewRealRunner()
	ctx := context.Background()

	result, err := cr.Run(ctx, "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoDir, Env: gitEnv})
	if err != nil || result.ExitCode != 0 {
		require.FailNow(t, "git init failed", "err=%v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	testFile := filepath.Join(repoDir, "README.md")
	require.NoError(t, os.WriteFile(testFile, []byte("# Test Repo\n"), 0o644))
	writeTestAgencyConfig(t, repoDir)

	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir, Env: gitEnv})
	if err != nil || result.ExitCode != 0 {
		require.FailNow(t, "git add failed", "err=%v, exit %d", err, result.ExitCode)
	}

	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "Initial commit"}, exec.RunOpts{Dir: repoDir, Env: gitEnv})
	if err != nil || result.ExitCode != 0 {
		require.FailNow(t, "git commit failed", "err=%v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	return repoDir
}

func writeTestAgencyConfig(t *testing.T, repoRoot string) {
	t.Helper()
	scriptsDir := filepath.Join(repoRoot, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0o755))
	for _, script := range []string{"setup", "verify", "archive"} {
		require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, script+".sh"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"), 0o755))
	}
	agencyJSON := `{"version":4,"scripts":{"setup":{"path":"scripts/setup.sh","timeout":"10m"},"verify":{"path":"scripts/verify.sh","timeout":"30m"},"archive":{"path":"scripts/archive.sh","timeout":"5m"}},"execution":{"checkout_root":"repo-sibling"}}`
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "agency.json"), []byte(agencyJSON), 0o644))
}

// createTestWorktree creates an integration worktree via the daemon client.
func createTestWorktree(t *testing.T, client *daemonclient.Client, repoRoot, name string) (worktreeID, treePath, repoID string) {
	t.Helper()
	ctx := context.Background()

	resp, err := client.WorktreeCreate(ctx, daemon.WorktreeCreateRequest{
		RepoRoot:   repoRoot,
		Name:       name,
		BaseBranch: "main",
	})
	require.NoError(t, err, "worktree create")
	require.True(t, resp.OK, "worktree create failed: %s - %s", resp.ErrorCode, resp.Message)

	return resp.WorktreeID, resp.TreePath, resp.RepoID
}

// startTestInvocation starts a headless invocation via the control plane.
func startTestInvocation(t *testing.T, client *daemonclient.Client, repoRoot, worktreeRef, mode string) *daemon.ControlPlaneStartResponse {
	t.Helper()
	return startTestInvocationWithRunner(t, client, repoRoot, worktreeRef, "claude-code", mode)
}

// startTestInvocationWithRunner starts a headless invocation with an explicit runner.
func startTestInvocationWithRunner(t *testing.T, client *daemonclient.Client, repoRoot, worktreeRef, runner, mode string) *daemon.ControlPlaneStartResponse {
	t.Helper()
	ctx := context.Background()

	resp, err := client.ControlPlaneStartHeadless(ctx, daemon.ControlPlaneStartRequest{
		RepoRoot:    repoRoot,
		WorktreeRef: worktreeRef,
		Runner:      runner,
		Prompt:      "test prompt",
		Env:         map[string]string{"FAKE_RUNNER_MODE": mode},
	})
	require.NoError(t, err, "control plane start")
	require.True(t, resp.OK, "control plane start failed: %s - %s", resp.ErrorCode, resp.Message)

	return resp
}

// waitForInvocationTerminal polls invocation meta until it reaches a terminal status or times out.
func waitForInvocationTerminal(t *testing.T, st *store.Store, repoID, invocationID string, timeout time.Duration) *store.InvocationMeta {
	t.Helper()
	var result *store.InvocationMeta
	require.Eventually(t, func() bool {
		meta, err := st.ReadInvocationMeta(repoID, invocationID)
		if err != nil {
			return false
		}
		if meta.Status == store.InvocationStatusFinished || meta.Status == store.InvocationStatusFailed {
			result = meta
			return true
		}
		return false
	}, timeout, 50*time.Millisecond, "invocation %s did not reach terminal status within %v", invocationID, timeout)
	return result
}

// gitExec runs a git command in the given directory and returns trimmed stdout.
// Fatals on non-zero exit or execution error.
func gitExec(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cr := exec.NewRealRunner()
	env := hermeticGitEnv(t)
	for attempt := 0; ; attempt++ {
		result, err := cr.Run(context.Background(), "git", args, exec.RunOpts{Dir: dir, Env: env})
		if err == nil && result.ExitCode == 0 {
			return strings.TrimSpace(result.Stdout)
		}
		if err == nil && result.ExitCode == 128 && strings.Contains(result.Stderr, "index.lock") && attempt < 40 {
			time.Sleep(25 * time.Millisecond)
			continue
		}
		require.NoError(t, err, "git %s", strings.Join(args, " "))
		require.Equal(t, 0, result.ExitCode, "git %s: exit %d, stderr: %s", strings.Join(args, " "), result.ExitCode, result.Stderr)
		return strings.TrimSpace(result.Stdout)
	}
}

// startTestHeadedInvocation starts a headed invocation via the daemon client.
// Fatals on error. Caller is responsible for killing the invocation.
func startTestHeadedInvocation(t *testing.T, client *daemonclient.Client, repoRoot, worktreeRef string) *daemon.ControlPlaneStartHeadedResponse {
	t.Helper()
	ctx := context.Background()

	resp, err := client.ControlPlaneStartHeaded(ctx, daemon.ControlPlaneStartRequest{
		RepoRoot:    repoRoot,
		WorktreeRef: worktreeRef,
		Runner:      "claude-code",
	})
	require.NoError(t, err, "control plane start headed")
	require.True(t, resp.OK, "control plane start headed failed: %s - %s", resp.ErrorCode, resp.Message)

	return resp
}

func readFakeRunnerLaunchCapture(t *testing.T, capturePath string) fakeRunnerLaunchCapture {
	t.Helper()
	var capture fakeRunnerLaunchCapture
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(capturePath)
		if err != nil || len(strings.TrimSpace(string(data))) == 0 {
			return false
		}
		return json.Unmarshal(data, &capture) == nil
	}, 5*time.Second, 50*time.Millisecond, "launch capture was not written: %s", capturePath)
	return capture
}

func normalizePathForAssert(t *testing.T, path string) string {
	t.Helper()
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return clean
	}
	return filepath.Clean(resolved)
}
