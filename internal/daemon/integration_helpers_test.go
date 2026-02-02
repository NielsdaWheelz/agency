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

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

// fakeRunnerPath returns the path to the compiled fake runner binary.
// Set by TestMain in testmain_test.go via environment variable.
func fakeRunnerPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("TEST_FAKE_RUNNER_PATH")
	if p == "" {
		t.Fatal("TEST_FAKE_RUNNER_PATH not set; TestMain should compile fakerunner")
	}
	return p
}

// testDaemonEnv holds the daemon server, client, and paths for a test.
type testDaemonEnv struct {
	Client  *daemonclient.Client
	Server  *daemon.Server
	DataDir string
	Store   *store.Store
}

// startTestDaemon boots a real daemon server on a temp Unix socket and
// returns a connected client. The server and socket are cleaned up when
// the test finishes.
func startTestDaemon(t *testing.T) *testDaemonEnv {
	t.Helper()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	// Write config.json pointing "claude" at the fake runner binary.
	// Must include "defaults" for LoadUserConfig validation to pass.
	runnerPath := fakeRunnerPath(t)
	cfg := map[string]any{
		"version": 1,
		"defaults": map[string]string{
			"runner": "claude",
			"editor": "code",
		},
		"runners": map[string]string{
			"claude": runnerPath,
		},
	}
	cfgBytes, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), cfgBytes, 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	srv := daemon.NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	// Unix sockets on macOS have a ~104 byte path limit.
	// Use a short temp dir for the socket to avoid exceeding it.
	sockDir, err := os.MkdirTemp("", "dsock")
	if err != nil {
		t.Fatalf("mkdir sockdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })

	socketPath := filepath.Join(sockDir, "d.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	// Serve in background goroutine.
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- srv.Serve(listener)
	}()

	client := daemonclient.NewClient(socketPath)

	// Wait for daemon to be ready.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.WaitForReady(ctx, 5*time.Second); err != nil {
		t.Fatalf("daemon not ready: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		// Wait for Serve to return.
		<-serveDone
	})

	return &testDaemonEnv{
		Client:  client,
		Server:  srv,
		DataDir: dataDir,
		Store:   st,
	}
}

// setupTestGitRepo creates a temporary git repo with one initial commit.
func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	testutil.HermeticGitEnv(t)

	repoDir := t.TempDir()
	cr := exec.NewRealRunner()
	ctx := context.Background()

	result, err := cr.Run(ctx, "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git init failed: %v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	testFile := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git add failed: %v, exit %d", err, result.ExitCode)
	}

	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "Initial commit"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("git commit failed: %v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	return repoDir
}

// createTestWorktree creates an integration worktree via the daemon client.
func createTestWorktree(t *testing.T, client *daemonclient.Client, repoRoot, name string) (worktreeID, treePath, repoID string) {
	t.Helper()
	ctx := context.Background()

	resp, err := client.WorktreeCreate(ctx, daemonclient.WorktreeCreateOpts{
		RepoRoot:     repoRoot,
		Name:         name,
		ParentBranch: "main",
	})
	if err != nil {
		t.Fatalf("worktree create: %v", err)
	}
	if !resp.OK {
		t.Fatalf("worktree create failed: %s - %s", resp.ErrorCode, resp.Message)
	}

	return resp.WorktreeID, resp.TreePath, resp.RepoID
}

// startTestInvocation starts a headless invocation via the control plane.
func startTestInvocation(t *testing.T, client *daemonclient.Client, repoRoot, worktreeRef, mode string) *daemon.ControlPlaneStartResponse {
	t.Helper()
	ctx := context.Background()

	resp, err := client.ControlPlaneStartHeadless(ctx, daemonclient.ControlPlaneStartOpts{
		RepoRoot:    repoRoot,
		WorktreeRef: worktreeRef,
		Runner:      "claude",
		Prompt:      "test prompt",
		Env:         map[string]string{"FAKE_RUNNER_MODE": mode},
	})
	if err != nil {
		t.Fatalf("control plane start: %v", err)
	}
	if !resp.OK {
		t.Fatalf("control plane start failed: %s - %s", resp.ErrorCode, resp.Message)
	}

	return resp
}

// waitForInvocationTerminal polls invocation meta until it reaches a terminal status or times out.
func waitForInvocationTerminal(t *testing.T, st *store.Store, repoID, invocationID string, timeout time.Duration) *store.InvocationMeta {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		meta, err := st.ReadInvocationMeta(repoID, invocationID)
		if err != nil {
			t.Fatalf("read invocation meta: %v", err)
		}
		if meta.Status == store.InvocationStatusFinished || meta.Status == store.InvocationStatusFailed {
			return meta
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("invocation %s did not reach terminal status within %v", invocationID, timeout)
	return nil
}

// gitExec runs a git command in the given directory and returns trimmed stdout.
// Fatals on non-zero exit or execution error.
func gitExec(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cr := exec.NewRealRunner()
	result, err := cr.Run(context.Background(), "git", args, exec.RunOpts{Dir: dir})
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("git %s: exit %d, stderr: %s", strings.Join(args, " "), result.ExitCode, result.Stderr)
	}
	return strings.TrimSpace(result.Stdout)
}
