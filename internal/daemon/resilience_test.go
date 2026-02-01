package daemon_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// TestDaemonRecoveryOrphanedInvocation verifies that the recovery scan
// on daemon startup detects stale "running" invocations with dead PIDs
// and marks them as failed/orphaned.
func TestDaemonRecoveryOrphanedInvocation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dataDir := t.TempDir()

	// Plant a fake repo and invocation with status=running but a dead PID.
	repoID := "fake-repo-id"
	invocationID := "20260101120000-abcd"
	deadPID := 99999
	deadPGID := 99999

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)

	// Create repo_index.json so recovery scan finds our repo.
	repoIndex := store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			repoID: {
				RepoID: repoID,
				Paths:  []string{"/tmp/fake-repo"},
			},
		},
	}
	indexBytes, _ := json.Marshal(repoIndex)
	if err := os.WriteFile(st.RepoIndexPath(), indexBytes, 0o644); err != nil {
		t.Fatalf("write repo index: %v", err)
	}

	// Create invocation directory and meta.json with status=running.
	invDir := st.InvocationDir(repoID, invocationID)
	if err := os.MkdirAll(invDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	meta := &store.InvocationMeta{
		SchemaVersion:    "1.0",
		InvocationID:     invocationID,
		Mode:             store.RunnerModeHeadless,
		Status:           store.InvocationStatusRunning,
		PID:              &deadPID,
		PGID:             &deadPGID,
		DaemonInstanceID: "old-daemon-instance",
		StartedAt:        time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	}
	if err := st.WriteInvocationMeta(repoID, invocationID, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	// Boot a daemon — recovery scan should detect the dead PID.
	configDir := filepath.Join(dataDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	srv := daemon.NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)
	// Override PIDChecker to report PID 99999 as dead.
	srv.PIDChecker = func(pid int) bool { return false }

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

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()

	client := daemonclient.NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.WaitForReady(ctx, 5*time.Second); err != nil {
		t.Fatalf("daemon not ready: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-serveDone
	})

	// Verify recovery scan updated the meta.
	recovered, err := st.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if recovered.Status != store.InvocationStatusFailed {
		t.Errorf("status = %q, want %q", recovered.Status, store.InvocationStatusFailed)
	}
	if !recovered.Flags.Orphaned {
		t.Error("expected orphaned=true")
	}
	if !recovered.Flags.NeedsAttention {
		t.Error("expected needs_attention=true")
	}
}

// TestDaemonRecoveryStaleStarting verifies that the recovery scan detects
// invocations stuck in "starting" status for >60s with no PID.
func TestDaemonRecoveryStaleStarting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dataDir := t.TempDir()

	repoID := "fake-repo-id"
	invocationID := "20260101120000-efgh"

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)

	// Create repo_index.json.
	repoIndex := store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			repoID: {
				RepoID: repoID,
				Paths:  []string{"/tmp/fake-repo"},
			},
		},
	}
	indexBytes, _ := json.Marshal(repoIndex)
	if err := os.WriteFile(st.RepoIndexPath(), indexBytes, 0o644); err != nil {
		t.Fatalf("write repo index: %v", err)
	}

	// Create invocation with status=starting, no PID, started 1 hour ago.
	invDir := st.InvocationDir(repoID, invocationID)
	if err := os.MkdirAll(invDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	meta := &store.InvocationMeta{
		SchemaVersion: "1.0",
		InvocationID:  invocationID,
		Mode:          store.RunnerModeHeadless,
		Status:        store.InvocationStatusStarting,
		StartedAt:     time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
		// No PID set.
	}
	if err := st.WriteInvocationMeta(repoID, invocationID, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	// Boot daemon.
	configDir := filepath.Join(dataDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	srv := daemon.NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	// Unix sockets on macOS have a ~104 byte path limit.
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

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()

	client := daemonclient.NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.WaitForReady(ctx, 5*time.Second); err != nil {
		t.Fatalf("daemon not ready: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-serveDone
	})

	// Verify recovery scan marked it as failed.
	recovered, err := st.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if recovered.Status != store.InvocationStatusFailed {
		t.Errorf("status = %q, want %q", recovered.Status, store.InvocationStatusFailed)
	}
	if recovered.FailureReason != "start_incomplete" {
		t.Errorf("failure_reason = %q, want %q", recovered.FailureReason, "start_incomplete")
	}
	if !recovered.Flags.NeedsAttention {
		t.Error("expected needs_attention=true")
	}
}

// TestDaemonRunnerUnexpectedExit verifies that when a runner process crashes,
// the daemon correctly updates meta and removes it from the supervised map.
func TestDaemonRunnerUnexpectedExit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "exit-test")
	startResp := startTestInvocation(t, env.Client, repoRoot, "exit-test", "exit-error")

	// Wait for meta to reach terminal state.
	meta := waitForInvocationTerminal(t, env.Store, startResp.RepoID, startResp.InvocationID, 5*time.Second)
	if meta.Status != store.InvocationStatusFailed {
		t.Errorf("status = %q, want %q", meta.Status, store.InvocationStatusFailed)
	}
	if meta.FailureReason != "runner_exit_nonzero" {
		t.Errorf("failure_reason = %q, want %q", meta.FailureReason, "runner_exit_nonzero")
	}

	// Verify process is no longer tracked — health should show no active invocations.
	health, err := env.Client.Health(ctx)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if !health.OK {
		t.Error("health should still be OK after runner exit")
	}
}
