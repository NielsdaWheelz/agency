package daemon_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dataDir := t.TempDir()

	// Plant a fake repo and invocation with status=running but an invalid PID.
	repoID := "fake-repo-id"
	invocationID := "20260101120000-abcd"
	deadPID := -1
	deadPGID := -1

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)

	// Create repo_index.json so recovery scan finds our repo.
	repoIndex := store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			repoID: {
				RepoID: repoID,
				Paths:  []string{"/tmp/fake-repo"},
			},
		},
	}
	indexBytes, _ := json.Marshal(repoIndex)
	require.NoError(t, os.WriteFile(st.RepoIndexPath(), indexBytes, 0o644), "write repo index")

	// Create invocation directory and meta.json with status=running.
	invDir := st.InvocationDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(invDir, 0o700), "mkdir")

	meta := &store.InvocationMeta{
		SchemaVersion:         store.SchemaVersion,
		InvocationID:          invocationID,
		IntegrationWorktreeID: "test-worktree",
		SandboxPath:           "/tmp/sandbox/" + invocationID,
		CheckoutRoot:          "/tmp/checkouts/" + repoID,
		ExecutionProfile:      "work",
		SandboxBranch:         "agency/sandbox-" + invocationID,
		BaseCommit:            "abc123",
		Runner:                "claude-code",
		Mode:                  store.RunnerModeHeadless,
		Status:                store.InvocationStatusRunning,
		PID:                   &deadPID,
		PGID:                  &deadPGID,
		DaemonInstanceID:      "old-daemon-instance",
		StartedAt:             time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	}
	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, meta), "write meta")

	// Boot a daemon — recovery scan should detect the dead PID.
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755), "mkdir config")
	srv := daemon.NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	// Unix sockets on macOS have a ~104 byte path limit.
	// Use a short temp dir for the socket to avoid exceeding it.
	sockDir, err := os.MkdirTemp("", "dsock")
	require.NoError(t, err, "mkdir sockdir")
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })

	socketPath := filepath.Join(sockDir, "d.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err, "listen")

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()

	client := daemonclient.NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.WaitForReady(ctx, 5*time.Second), "daemon not ready")

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-serveDone
	})

	// Verify recovery scan updated the meta.
	recovered, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err, "read meta")
	assert.Equal(t, store.InvocationStatusFailed, recovered.Status)
	assert.True(t, recovered.Flags.Orphaned, "expected orphaned=true")
	assert.True(t, recovered.Flags.NeedsAttention, "expected needs_attention=true")
}

// TestDaemonRecoveryStaleStarting verifies that the recovery scan detects
// invocations stuck in "starting" status for >60s with no PID.
func TestDaemonRecoveryStaleStarting(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dataDir := t.TempDir()

	repoID := "fake-repo-id"
	invocationID := "20260101120000-efgh"

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)

	// Create repo_index.json.
	repoIndex := store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			repoID: {
				RepoID: repoID,
				Paths:  []string{"/tmp/fake-repo"},
			},
		},
	}
	indexBytes, _ := json.Marshal(repoIndex)
	require.NoError(t, os.WriteFile(st.RepoIndexPath(), indexBytes, 0o644), "write repo index")

	// Create invocation with status=starting, no PID, started 1 hour ago.
	invDir := st.InvocationDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(invDir, 0o700), "mkdir")

	meta := &store.InvocationMeta{
		SchemaVersion:         store.SchemaVersion,
		InvocationID:          invocationID,
		IntegrationWorktreeID: "test-worktree",
		SandboxPath:           "/tmp/sandbox/" + invocationID,
		CheckoutRoot:          "/tmp/checkouts/" + repoID,
		ExecutionProfile:      "work",
		SandboxBranch:         "agency/sandbox-" + invocationID,
		BaseCommit:            "abc123",
		Runner:                "claude-code",
		Mode:                  store.RunnerModeHeadless,
		Status:                store.InvocationStatusStarting,
		StartedAt:             time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
		// No PID set.
	}
	require.NoError(t, st.WriteInvocationMeta(repoID, invocationID, meta), "write meta")

	// Boot daemon.
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755), "mkdir config")
	srv := daemon.NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	// Unix sockets on macOS have a ~104 byte path limit.
	sockDir, err := os.MkdirTemp("", "dsock")
	require.NoError(t, err, "mkdir sockdir")
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })

	socketPath := filepath.Join(sockDir, "d.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err, "listen")

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()

	client := daemonclient.NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.WaitForReady(ctx, 5*time.Second), "daemon not ready")

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-serveDone
	})

	// Verify recovery scan marked it as failed.
	recovered, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err, "read meta")
	assert.Equal(t, store.InvocationStatusFailed, recovered.Status)
	assert.Equal(t, "start_incomplete", recovered.FailureReason)
	assert.True(t, recovered.Flags.NeedsAttention, "expected needs_attention=true")
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
	assert.Equal(t, store.InvocationStatusFailed, meta.Status)
	assert.Equal(t, "runner_exit_nonzero", meta.FailureReason)

	// Verify process is no longer tracked — health should show no active invocations.
	health, err := env.Client.Health(ctx)
	require.NoError(t, err, "health")
	assert.True(t, health.OK, "health should still be OK after runner exit")
}
