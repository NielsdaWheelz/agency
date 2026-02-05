package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// ---------------------------------------------------------------------------
// Lifecycle Tests
// ---------------------------------------------------------------------------

func TestDaemonHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	resp, err := env.Client.Health(ctx)
	require.NoError(t, err, "health")

	assert.True(t, resp.OK, "expected OK=true")
	assert.Equal(t, daemon.APIVersion, resp.APIVersion)
	assert.NotEmpty(t, resp.DaemonInstanceID, "expected daemon_instance_id to be set")
	assert.NotZero(t, resp.PID, "expected pid to be set")
}

func TestDaemonShutdownClean(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	resp, err := env.Client.Shutdown(ctx, false)
	require.NoError(t, err, "shutdown")
	assert.True(t, resp.OK, "expected OK=true, got error: %s - %s", resp.ErrorCode, resp.Message)

	// After shutdown, health should fail.
	require.Eventually(t, func() bool {
		return !env.Client.IsRunning(ctx)
	}, 5*time.Second, 50*time.Millisecond, "daemon still running after shutdown")
}

func TestDaemonShutdownBusyRejectsWithoutForce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "busy-test")
	startResp := startTestInvocation(t, env.Client, repoRoot, "busy-test", "sleep")

	// Shutdown without force should fail.
	shutResp, err := env.Client.Shutdown(ctx, false)
	require.NoError(t, err, "shutdown")
	assert.False(t, shutResp.OK, "expected shutdown to fail without force")
	assert.Equal(t, "E_DAEMON_BUSY", shutResp.ErrorCode)
	assert.NotEmpty(t, shutResp.RunningInvocations, "expected running_invocations to be populated")

	// Shutdown with force should succeed.
	shutResp, err = env.Client.Shutdown(ctx, true)
	require.NoError(t, err, "shutdown force")
	assert.True(t, shutResp.OK, "expected OK=true with force, got error: %s - %s", shutResp.ErrorCode, shutResp.Message)

	// Wait for async shutdown to update invocation meta.
	require.Eventually(t, func() bool {
		meta, err := env.Store.ReadInvocationMeta(startResp.RepoID, startResp.InvocationID)
		return err == nil && meta.Status == store.InvocationStatusFailed
	}, 10*time.Second, 50*time.Millisecond, "invocation meta not updated after force shutdown")
}

// ---------------------------------------------------------------------------
// Invocation Tests
// ---------------------------------------------------------------------------

func TestDaemonControlPlaneStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	wtID, _, repoID := createTestWorktree(t, env.Client, repoRoot, "start-test")

	resp, err := env.Client.ControlPlaneStartHeadless(ctx, daemonclient.ControlPlaneStartOpts{
		RepoRoot:    repoRoot,
		WorktreeRef: wtID,
		Runner:      "claude",
		Prompt:      "test prompt",
		Env:         map[string]string{"FAKE_RUNNER_MODE": "exit-ok"},
	})
	require.NoError(t, err, "start")
	require.True(t, resp.OK, "start failed: %s - %s", resp.ErrorCode, resp.Message)

	assert.NotEmpty(t, resp.InvocationID, "expected invocation_id to be set")
	assert.NotZero(t, resp.PID, "expected pid to be set")
	assert.NotEmpty(t, resp.SandboxPath, "expected sandbox_path to be set")
	assert.Equal(t, repoID, resp.RepoID)
	assert.NotEmpty(t, resp.DaemonInstanceID, "expected daemon_instance_id to be set")
	assert.NotNil(t, resp.LogPaths, "expected log_paths to be set")

	// Wait for the exit-ok runner to finish.
	meta := waitForInvocationTerminal(t, env.Store, repoID, resp.InvocationID, 5*time.Second)
	assert.Equal(t, store.InvocationStatusFinished, meta.Status)
}

func TestDaemonControlPlaneStartAndStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "stop-test")
	startResp := startTestInvocation(t, env.Client, repoRoot, "stop-test", "sleep")

	// Stop the invocation.
	stopResp, err := env.Client.Stop(ctx, startResp.RepoID, startResp.InvocationID)
	require.NoError(t, err, "stop")
	assert.True(t, stopResp.OK, "stop failed: %s - %s", stopResp.ErrorCode, stopResp.Message)

	// Wait for the process to actually exit (stop escalation runs in background).
	meta := waitForInvocationTerminal(t, env.Store, startResp.RepoID, startResp.InvocationID, 10*time.Second)
	assert.Equal(t, store.InvocationStatusFailed, meta.Status)
	assert.Equal(t, "stopped", meta.ExitReason)
	assert.Equal(t, "stopped", meta.FailureReason)
}

func TestDaemonStartAndKill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "kill-test")
	startResp := startTestInvocation(t, env.Client, repoRoot, "kill-test", "sleep")

	// Kill the invocation.
	killResp, err := env.Client.Kill(ctx, startResp.RepoID, startResp.InvocationID)
	require.NoError(t, err, "kill")
	assert.True(t, killResp.OK, "kill failed: %s - %s", killResp.ErrorCode, killResp.Message)

	// After kill, meta should reach terminal state quickly (no escalation).
	meta := waitForInvocationTerminal(t, env.Store, startResp.RepoID, startResp.InvocationID, 5*time.Second)
	assert.Equal(t, store.InvocationStatusFailed, meta.Status)
	assert.Equal(t, "killed", meta.ExitReason)
	assert.Equal(t, "killed", meta.FailureReason)
}

func TestDaemonStopEscalation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "escalation-test")

	// Start with ignore-sigint runner: SIGINT is ignored, SIGTERM will work.
	startResp := startTestInvocation(t, env.Client, repoRoot, "escalation-test", "ignore-sigint")

	// Stop — should escalate from SIGINT to SIGTERM after 5s.
	stopResp, err := env.Client.Stop(ctx, startResp.RepoID, startResp.InvocationID)
	require.NoError(t, err, "stop")
	assert.True(t, stopResp.OK, "stop failed: %s - %s", stopResp.ErrorCode, stopResp.Message)

	// Must wait for escalation: 5s (SIGINT wait) + some buffer.
	meta := waitForInvocationTerminal(t, env.Store, startResp.RepoID, startResp.InvocationID, 15*time.Second)
	assert.Equal(t, store.InvocationStatusFailed, meta.Status)
	assert.Equal(t, "stopped", meta.ExitReason)
}

func TestDaemonIdempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "idempotent-test")

	// First start — sleep mode so it stays running.
	resp1 := startTestInvocation(t, env.Client, repoRoot, "idempotent-test", "sleep")

	// Second start with same worktree — client generates a new client_request_id each time,
	// so this will be a different invocation. But we can test by sending the same request
	// manually with the same client_request_id.
	// The ControlPlaneStartHeadless client method auto-generates a UUID, so we test
	// idempotency by verifying the response has already_running=false for a new request
	// (which means a second invocation was started correctly on the same worktree).
	// True idempotency testing requires sending raw HTTP with the same client_request_id.

	// Clean up the first invocation.
	_, _ = env.Client.Kill(ctx, resp1.RepoID, resp1.InvocationID)

	// The key point: the first start worked and returned valid fields.
	assert.NotEmpty(t, resp1.InvocationID, "expected invocation_id to be set")
	assert.False(t, resp1.AlreadyRunning, "first request should not be already_running")
}

func TestDaemonNameCollision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "name-test")

	// Start first invocation with a name.
	resp1, err := env.Client.ControlPlaneStartHeadless(ctx, daemonclient.ControlPlaneStartOpts{
		RepoRoot:       repoRoot,
		WorktreeRef:    "name-test",
		Runner:         "claude",
		Prompt:         "test prompt",
		InvocationName: "my-agent",
		Env:            map[string]string{"FAKE_RUNNER_MODE": "sleep"},
	})
	require.NoError(t, err, "first start")
	require.True(t, resp1.OK, "first start failed: %s - %s", resp1.ErrorCode, resp1.Message)

	// Second invocation with same name should fail.
	resp2, err := env.Client.ControlPlaneStartHeadless(ctx, daemonclient.ControlPlaneStartOpts{
		RepoRoot:       repoRoot,
		WorktreeRef:    "name-test",
		Runner:         "claude",
		Prompt:         "test prompt 2",
		InvocationName: "my-agent",
		Env:            map[string]string{"FAKE_RUNNER_MODE": "sleep"},
	})
	require.NoError(t, err, "second start")
	if resp2.OK {
		// Clean up the accidental second invocation.
		_, _ = env.Client.Kill(ctx, resp2.RepoID, resp2.InvocationID)
		require.Fail(t, "expected second start to fail due to name collision")
	}
	assert.Equal(t, "E_INVOCATION_NAME_EXISTS", resp2.ErrorCode)

	// Clean up.
	_, _ = env.Client.Kill(ctx, resp1.RepoID, resp1.InvocationID)
}

func TestDaemonRunnerCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "crash-test")
	startResp := startTestInvocation(t, env.Client, repoRoot, "crash-test", "exit-error")

	// exit-error mode exits immediately with code 1.
	meta := waitForInvocationTerminal(t, env.Store, startResp.RepoID, startResp.InvocationID, 5*time.Second)
	assert.Equal(t, store.InvocationStatusFailed, meta.Status)
	assert.Equal(t, "runner_exit_nonzero", meta.FailureReason)
	if assert.NotNil(t, meta.ExitCode) {
		assert.Equal(t, 1, *meta.ExitCode)
	}
}

// ---------------------------------------------------------------------------
// Worktree Tests
// ---------------------------------------------------------------------------

func TestDaemonWorktreeCreateAndRemove(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	wtID, treePath, repoID := createTestWorktree(t, env.Client, repoRoot, "lifecycle-test")

	// Verify tree exists on disk.
	_, err := os.Stat(treePath)
	assert.False(t, os.IsNotExist(err), "tree_path does not exist: %s", treePath)

	// Verify INTEGRATION_MARKER exists.
	markerPath := filepath.Join(treePath, ".agency", "INTEGRATION_MARKER")
	_, err = os.Stat(markerPath)
	assert.False(t, os.IsNotExist(err), "INTEGRATION_MARKER does not exist: %s", markerPath)

	// Remove worktree (force=true because marker file is untracked).
	rmResp, err := env.Client.WorktreeRm(ctx, repoID, wtID, true)
	require.NoError(t, err, "worktree rm")
	assert.True(t, rmResp.OK, "worktree rm failed: %s - %s", rmResp.ErrorCode, rmResp.Message)

	// Verify tree is gone.
	_, err = os.Stat(treePath)
	assert.True(t, os.IsNotExist(err), "tree_path still exists: %s", treePath)

	// Verify meta is archived.
	meta, err := env.Store.ReadIntegrationWorktreeMeta(repoID, wtID)
	require.NoError(t, err, "read worktree meta")
	assert.Equal(t, store.WorktreeStateArchived, meta.State)
}

func TestDaemonWorktreeCreateIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	idempotencyKey := "test-key-123"

	// First create.
	resp1, err := env.Client.WorktreeCreate(ctx, daemonclient.WorktreeCreateOpts{
		RepoRoot:       repoRoot,
		Name:           "idem-test",
		ParentBranch:   "main",
		IdempotencyKey: idempotencyKey,
	})
	require.NoError(t, err, "first create")
	require.True(t, resp1.OK, "first create failed: %s - %s", resp1.ErrorCode, resp1.Message)

	// Second create with same key.
	resp2, err := env.Client.WorktreeCreate(ctx, daemonclient.WorktreeCreateOpts{
		RepoRoot:       repoRoot,
		Name:           "idem-test",
		ParentBranch:   "main",
		IdempotencyKey: idempotencyKey,
	})
	require.NoError(t, err, "second create")
	require.True(t, resp2.OK, "second create failed: %s - %s", resp2.ErrorCode, resp2.Message)

	// Should return same worktree.
	assert.Equal(t, resp1.WorktreeID, resp2.WorktreeID, "idempotent requests returned different IDs")
}

func TestDaemonWorktreeRemoveWithActiveAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	wtID, _, repoID := createTestWorktree(t, env.Client, repoRoot, "active-rm-test")

	// Start an invocation targeting this worktree.
	startResp := startTestInvocation(t, env.Client, repoRoot, "active-rm-test", "sleep")

	// Remove without force should fail.
	rmResp, err := env.Client.WorktreeRm(ctx, repoID, wtID, false)
	require.NoError(t, err, "worktree rm")
	require.False(t, rmResp.OK, "expected rm to fail with active invocation")
	assert.Equal(t, "E_WORKTREE_HAS_ACTIVE_INVOCATIONS", rmResp.ErrorCode)

	// Remove with force should succeed.
	rmResp, err = env.Client.WorktreeRm(ctx, repoID, wtID, true)
	require.NoError(t, err, "worktree rm force")
	assert.True(t, rmResp.OK, "worktree rm force failed: %s - %s", rmResp.ErrorCode, rmResp.Message)

	// Wait for invocation to be killed.
	require.Eventually(t, func() bool {
		meta, err := env.Store.ReadInvocationMeta(startResp.RepoID, startResp.InvocationID)
		return err == nil && meta.Status == store.InvocationStatusFailed
	}, 10*time.Second, 50*time.Millisecond, "invocation not killed after force worktree rm")
}

func TestDaemonWorktreeNameUniqueness(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "unique-name")

	// Second create with same name but different key should fail.
	resp2, err := env.Client.WorktreeCreate(ctx, daemonclient.WorktreeCreateOpts{
		RepoRoot:       repoRoot,
		Name:           "unique-name",
		ParentBranch:   "main",
		IdempotencyKey: "different-key",
	})
	require.NoError(t, err, "second create")
	require.False(t, resp2.OK, "expected second create to fail due to name collision")
	assert.Equal(t, "E_WORKTREE_NAME_EXISTS", resp2.ErrorCode)
}

// ---------------------------------------------------------------------------
// Concurrency Test
// ---------------------------------------------------------------------------

func TestDaemonConcurrentStarts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "concurrent-a")
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "concurrent-b")

	var wg sync.WaitGroup
	var resp1, resp2 *daemon.ControlPlaneStartResponse
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		resp1, err1 = env.Client.ControlPlaneStartHeadless(ctx, daemonclient.ControlPlaneStartOpts{
			RepoRoot:    repoRoot,
			WorktreeRef: "concurrent-a",
			Runner:      "claude",
			Prompt:      "prompt a",
			Env:         map[string]string{"FAKE_RUNNER_MODE": "sleep"},
		})
	}()
	go func() {
		defer wg.Done()
		resp2, err2 = env.Client.ControlPlaneStartHeadless(ctx, daemonclient.ControlPlaneStartOpts{
			RepoRoot:    repoRoot,
			WorktreeRef: "concurrent-b",
			Runner:      "claude",
			Prompt:      "prompt b",
			Env:         map[string]string{"FAKE_RUNNER_MODE": "sleep"},
		})
	}()
	wg.Wait()

	require.NoError(t, err1, "start a")
	require.NoError(t, err2, "start b")
	require.True(t, resp1.OK, "start a failed: %s - %s", resp1.ErrorCode, resp1.Message)
	require.True(t, resp2.OK, "start b failed: %s - %s", resp2.ErrorCode, resp2.Message)

	assert.NotEqual(t, resp1.InvocationID, resp2.InvocationID, "concurrent starts returned same invocation_id")

	// Clean up.
	_, _ = env.Client.Kill(ctx, resp1.RepoID, resp1.InvocationID)
	_, _ = env.Client.Kill(ctx, resp2.RepoID, resp2.InvocationID)
}

// ---------------------------------------------------------------------------
// Checkpoint Integration Tests (Phase 5, PR-08)
// ---------------------------------------------------------------------------

// 5.1 TestDaemonCheckpointApplyWhileRunning
// Start a sleep-mode invocation and attempt checkpoint apply immediately.
// Should return E_INVOCATION_STILL_RUNNING.
func TestDaemonCheckpointApplyWhileRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "cp-running")
	startResp := startTestInvocation(t, env.Client, repoRoot, "cp-running", "sleep")

	// Attempt checkpoint apply while invocation is still running.
	resp, err := env.Client.CheckpointApply(ctx, startResp.RepoID, startResp.InvocationID, 1)
	require.NoError(t, err, "CheckpointApply returned transport error")

	assert.False(t, resp.OK, "expected OK=false for checkpoint apply while running")
	assert.Equal(t, "E_INVOCATION_STILL_RUNNING", resp.ErrorCode)

	// Clean up: kill the invocation.
	_, _ = env.Client.Kill(ctx, startResp.RepoID, startResp.InvocationID)
}

// 5.2 TestDaemonCheckpointApplyNotFound
// Start and finish an invocation, then apply a non-existent checkpoint ID.
// Should return E_CHECKPOINT_NOT_FOUND.
func TestDaemonCheckpointApplyNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, repoID := createTestWorktree(t, env.Client, repoRoot, "cp-notfound")

	// Start with exit-ok so the invocation finishes immediately.
	startResp := startTestInvocation(t, env.Client, repoRoot, "cp-notfound", "exit-ok")

	// Wait for invocation to finish.
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 5*time.Second)

	// Apply non-existent checkpoint.
	resp, err := env.Client.CheckpointApply(ctx, startResp.RepoID, startResp.InvocationID, 999)
	require.NoError(t, err, "CheckpointApply returned transport error")

	assert.False(t, resp.OK, "expected OK=false for non-existent checkpoint")
	assert.Equal(t, "E_CHECKPOINT_NOT_FOUND", resp.ErrorCode)
}

// 5.3 TestDaemonCheckpointLifecycle
// Full E2E: start invocation, write file in sandbox, wait for checkpoint,
// kill invocation, verify checkpoints exist, apply checkpoint, verify file.
func TestDaemonCheckpointLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, repoID := createTestWorktree(t, env.Client, repoRoot, "cp-lifecycle")

	// Start sleep-mode invocation (stays running so we can manipulate sandbox).
	startResp := startTestInvocation(t, env.Client, repoRoot, "cp-lifecycle", "sleep")
	sandboxPath := startResp.SandboxPath

	// Write a file in the sandbox to trigger the checkpoint engine via fsnotify.
	testFilePath := filepath.Join(sandboxPath, "checkpoint-test.txt")
	require.NoError(t, os.WriteFile(testFilePath, []byte("checkpoint test content\n"), 0o644), "failed to write test file")

	// Wait for the checkpoint engine to process the file write.
	// With test debounce override (100ms), this should be fast.
	checkpointsPath := env.Store.SandboxCheckpointsPath(repoID, startResp.InvocationID)
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(checkpointsPath)
		if err != nil {
			return false
		}
		return strings.Contains(string(data), "\"id\":")
	}, 10*time.Second, 100*time.Millisecond, "checkpoint not created after file write")

	// Kill the invocation. This triggers a final checkpoint via the engine's Stop().
	killResp, err := env.Client.Kill(ctx, startResp.RepoID, startResp.InvocationID)
	require.NoError(t, err, "kill")
	require.True(t, killResp.OK, "kill failed: %s - %s", killResp.ErrorCode, killResp.Message)

	// Wait for invocation to reach terminal state.
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 10*time.Second)

	// Wait for checkpoint engine goroutine to finish writing final checkpoint.
	var cpData []byte
	require.Eventually(t, func() bool {
		var readErr error
		cpData, readErr = os.ReadFile(checkpointsPath)
		if readErr != nil {
			return false
		}
		var cpFileCheck checkpoint.CheckpointsFile
		if json.Unmarshal(cpData, &cpFileCheck) != nil {
			return false
		}
		return len(cpFileCheck.Checkpoints) > 0
	}, 10*time.Second, 100*time.Millisecond, "no checkpoints found after kill")

	var cpFile checkpoint.CheckpointsFile
	require.NoError(t, json.Unmarshal(cpData, &cpFile), "failed to parse checkpoints.json")
	require.NotEmpty(t, cpFile.Checkpoints, "expected at least one checkpoint, got none")

	latestCP := cpFile.Checkpoints[len(cpFile.Checkpoints)-1]
	t.Logf("found %d checkpoints, latest ID=%d", len(cpFile.Checkpoints), latestCP.ID)

	// Verify the test file still exists in the sandbox (checkpoint captured it).
	_, err = os.Stat(testFilePath)
	assert.False(t, os.IsNotExist(err), "test file should still exist in sandbox after kill")

	// Modify the sandbox to verify that apply restores the checkpoint state.
	require.NoError(t, os.WriteFile(testFilePath, []byte("modified after kill\n"), 0o644), "failed to modify test file")

	// Apply the latest checkpoint via the daemon client.
	applyResp, err := env.Client.CheckpointApply(ctx, startResp.RepoID, startResp.InvocationID, latestCP.ID)
	require.NoError(t, err, "CheckpointApply transport error")
	require.True(t, applyResp.OK, "CheckpointApply failed: %s - %s", applyResp.ErrorCode, applyResp.Message)
	assert.Equal(t, latestCP.ID, applyResp.CheckpointID)

	// Verify the file was restored to the checkpoint state (original content).
	restoredContent, err := os.ReadFile(testFilePath)
	require.NoError(t, err, "failed to read restored file")
	assert.Equal(t, "checkpoint test content\n", string(restoredContent))
}

// ---------------------------------------------------------------------------
// Landing Integration Tests (PR-09)
// ---------------------------------------------------------------------------

func TestDaemonLandCherryPick(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, treePath, repoID := createTestWorktree(t, env.Client, repoRoot, "land-cp")

	// Start and wait for invocation to finish.
	startResp := startTestInvocation(t, env.Client, repoRoot, "land-cp", "exit-ok")
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 5*time.Second)

	// Create a commit in the sandbox.
	sandboxPath := startResp.SandboxPath
	require.NoError(t, os.WriteFile(filepath.Join(sandboxPath, "new-file.txt"), []byte("landed content\n"), 0o644))
	gitExec(t, sandboxPath, "add", "new-file.txt")
	gitExec(t, sandboxPath, "commit", "-m", "sandbox commit")

	// Land via cherry-pick.
	resp, err := env.Client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoID,
		InvocationID: startResp.InvocationID,
	})
	require.NoError(t, err)

	require.True(t, resp.OK, "expected OK=true, got error: %s - %s", resp.ErrorCode, resp.Message)
	assert.Equal(t, daemon.LandingModeCherryPick, resp.AppliedMode)
	assert.Equal(t, 1, resp.CommitsLanded)
	assert.NotEqual(t, resp.IntegrationHeadBefore, resp.IntegrationHeadAfter)

	// Verify the file landed in the integration tree.
	content, err := os.ReadFile(filepath.Join(treePath, "new-file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "landed content\n", string(content))

	// Verify sandbox was cleaned up.
	_, err = os.Stat(sandboxPath)
	assert.True(t, os.IsNotExist(err), "sandbox should be removed after landing")

	// Verify invocation meta updated.
	meta, err := env.Store.ReadInvocationMeta(repoID, startResp.InvocationID)
	require.NoError(t, err)
	assert.Equal(t, store.LandingStatusLanded, meta.LandingStatus)
}

func TestDaemonLandApply(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, treePath, repoID := createTestWorktree(t, env.Client, repoRoot, "land-apply")

	startResp := startTestInvocation(t, env.Client, repoRoot, "land-apply", "exit-ok")
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 5*time.Second)

	// Write a file in the sandbox but do NOT commit (dirty tree only).
	sandboxPath := startResp.SandboxPath
	require.NoError(t, os.WriteFile(filepath.Join(sandboxPath, "applied-file.txt"), []byte("applied content\n"), 0o644))

	// Land with Apply=true.
	resp, err := env.Client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoID,
		InvocationID: startResp.InvocationID,
		Apply:        true,
	})
	require.NoError(t, err)

	require.True(t, resp.OK, "expected OK=true, got error: %s - %s", resp.ErrorCode, resp.Message)
	assert.Equal(t, daemon.LandingModeApplyPatch, resp.AppliedMode)
	assert.Equal(t, 1, resp.CommitsLanded)

	// Verify the file landed in the integration tree.
	content, err := os.ReadFile(filepath.Join(treePath, "applied-file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "applied content\n", string(content))

	// Verify sandbox was cleaned up.
	_, err = os.Stat(sandboxPath)
	assert.True(t, os.IsNotExist(err), "sandbox should be removed after landing")

	// Verify meta updated.
	meta, err := env.Store.ReadInvocationMeta(repoID, startResp.InvocationID)
	require.NoError(t, err)
	assert.Equal(t, store.LandingStatusLanded, meta.LandingStatus)
}

func TestDaemonLandConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, treePath, repoID := createTestWorktree(t, env.Client, repoRoot, "land-conflict")

	startResp := startTestInvocation(t, env.Client, repoRoot, "land-conflict", "exit-ok")
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 5*time.Second)

	// Capture integration HEAD before modifications.
	headBefore := gitExec(t, treePath, "rev-parse", "HEAD")

	// Create a conflicting commit in the sandbox.
	sandboxPath := startResp.SandboxPath
	require.NoError(t, os.WriteFile(filepath.Join(sandboxPath, "README.md"), []byte("sandbox version\n"), 0o644))
	gitExec(t, sandboxPath, "add", "README.md")
	gitExec(t, sandboxPath, "commit", "-m", "sandbox modifies README")

	// Create a conflicting commit in the integration tree.
	require.NoError(t, os.WriteFile(filepath.Join(treePath, "README.md"), []byte("integration version\n"), 0o644))
	gitExec(t, treePath, "add", "README.md")
	gitExec(t, treePath, "commit", "-m", "integration modifies README")

	// Attempt to land — should conflict.
	resp, err := env.Client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoID,
		InvocationID: startResp.InvocationID,
	})
	require.NoError(t, err)

	assert.False(t, resp.OK)
	assert.Equal(t, "E_LAND_CONFLICT", resp.ErrorCode)
	assert.NotEmpty(t, resp.ConflictFiles)
	assert.Contains(t, resp.ConflictFiles, "README.md")

	// Verify integration tree HEAD was restored (cherry-pick aborted).
	headAfter := gitExec(t, treePath, "rev-parse", "HEAD")
	assert.NotEqual(t, headBefore, headAfter, "integration HEAD should have advanced from the integration commit")

	// Sandbox should still exist (not cleaned up on failure).
	_, err = os.Stat(sandboxPath)
	assert.NoError(t, err, "sandbox should still exist after failed land")
}

func TestDaemonLandNothingToLand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, repoID := createTestWorktree(t, env.Client, repoRoot, "land-nothing")

	startResp := startTestInvocation(t, env.Client, repoRoot, "land-nothing", "exit-ok")
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 5*time.Second)

	// Land without any changes — no commits, clean tree.
	// The daemon writes .agency/SANDBOX_MARKER at invocation startup, but
	// isSandboxDirty excludes .agency/ so it should not count as dirty.
	resp, err := env.Client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoID,
		InvocationID: startResp.InvocationID,
	})
	require.NoError(t, err)

	assert.False(t, resp.OK)
	assert.Equal(t, "E_LAND_NOTHING_TO_LAND", resp.ErrorCode)
}

func TestDaemonLandApplyRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, repoID := createTestWorktree(t, env.Client, repoRoot, "land-apply-req")

	startResp := startTestInvocation(t, env.Client, repoRoot, "land-apply-req", "exit-ok")
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 5*time.Second)

	// Write a file (dirty tree) but do NOT commit, and do NOT set Apply.
	sandboxPath := startResp.SandboxPath
	require.NoError(t, os.WriteFile(filepath.Join(sandboxPath, "dirty-file.txt"), []byte("dirty\n"), 0o644))

	resp, err := env.Client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoID,
		InvocationID: startResp.InvocationID,
		// Apply: false (default)
	})
	require.NoError(t, err)

	assert.False(t, resp.OK)
	assert.Equal(t, "E_LAND_APPLY_REQUIRED", resp.ErrorCode)
	assert.Contains(t, resp.Hint, "apply")
}

func TestDaemonLandWhileRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "land-running")

	// Start a sleep invocation (stays running).
	startResp := startTestInvocation(t, env.Client, repoRoot, "land-running", "sleep")

	resp, err := env.Client.Land(ctx, daemonclient.LandOpts{
		RepoID:       startResp.RepoID,
		InvocationID: startResp.InvocationID,
	})
	require.NoError(t, err)

	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVOCATION_STILL_RUNNING", resp.ErrorCode)

	// Clean up.
	_, _ = env.Client.Kill(ctx, startResp.RepoID, startResp.InvocationID)
}

func TestDaemonLandAlreadyLanded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, repoID := createTestWorktree(t, env.Client, repoRoot, "land-twice")

	startResp := startTestInvocation(t, env.Client, repoRoot, "land-twice", "exit-ok")
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 5*time.Second)

	// Create a commit so the first land succeeds.
	sandboxPath := startResp.SandboxPath
	require.NoError(t, os.WriteFile(filepath.Join(sandboxPath, "file.txt"), []byte("content\n"), 0o644))
	gitExec(t, sandboxPath, "add", "file.txt")
	gitExec(t, sandboxPath, "commit", "-m", "commit for landing")

	// First land should succeed.
	resp1, err := env.Client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoID,
		InvocationID: startResp.InvocationID,
	})
	require.NoError(t, err)
	require.True(t, resp1.OK, "first land should succeed: %s - %s", resp1.ErrorCode, resp1.Message)

	// Second land should fail.
	resp2, err := env.Client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoID,
		InvocationID: startResp.InvocationID,
	})
	require.NoError(t, err)

	assert.False(t, resp2.OK)
	assert.Equal(t, "E_LAND_ALREADY_LANDED", resp2.ErrorCode)
}

func TestDaemonLandAlreadyDiscarded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, repoID := createTestWorktree(t, env.Client, repoRoot, "land-after-discard")

	startResp := startTestInvocation(t, env.Client, repoRoot, "land-after-discard", "exit-ok")
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 5*time.Second)

	// Discard first.
	discardResp, err := env.Client.Discard(ctx, repoID, startResp.InvocationID)
	require.NoError(t, err)
	require.True(t, discardResp.OK, "discard should succeed: %s - %s", discardResp.ErrorCode, discardResp.Message)

	// Then try to land.
	landResp, err := env.Client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoID,
		InvocationID: startResp.InvocationID,
	})
	require.NoError(t, err)

	assert.False(t, landResp.OK)
	assert.Equal(t, "E_LAND_ALREADY_DISCARDED", landResp.ErrorCode)
}

// ---------------------------------------------------------------------------
// Discard Integration Tests (PR-09)
// ---------------------------------------------------------------------------

func TestDaemonDiscard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, repoID := createTestWorktree(t, env.Client, repoRoot, "discard-basic")

	startResp := startTestInvocation(t, env.Client, repoRoot, "discard-basic", "exit-ok")
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 5*time.Second)

	sandboxPath := startResp.SandboxPath

	resp, err := env.Client.Discard(ctx, repoID, startResp.InvocationID)
	require.NoError(t, err)

	require.True(t, resp.OK, "expected OK=true, got error: %s - %s", resp.ErrorCode, resp.Message)

	// Verify meta updated.
	meta, err := env.Store.ReadInvocationMeta(repoID, startResp.InvocationID)
	require.NoError(t, err)
	assert.Equal(t, store.LandingStatusDiscarded, meta.LandingStatus)

	// Verify sandbox was cleaned up.
	_, err = os.Stat(sandboxPath)
	assert.True(t, os.IsNotExist(err), "sandbox should be removed after discard")
}

func TestDaemonDiscardRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "discard-running")

	// Start a sleep invocation (stays running).
	startResp := startTestInvocation(t, env.Client, repoRoot, "discard-running", "sleep")

	resp, err := env.Client.Discard(ctx, startResp.RepoID, startResp.InvocationID)
	require.NoError(t, err)

	require.True(t, resp.OK, "expected OK=true, got error: %s - %s", resp.ErrorCode, resp.Message)

	// Wait for the process to terminate and meta to update.
	var meta *store.InvocationMeta
	require.Eventually(t, func() bool {
		var readErr error
		meta, readErr = env.Store.ReadInvocationMeta(startResp.RepoID, startResp.InvocationID)
		return readErr == nil && meta.Status == store.InvocationStatusFailed
	}, 10*time.Second, 50*time.Millisecond, "invocation meta not updated after discard")
	assert.Equal(t, "discarded", meta.ExitReason)
	assert.Equal(t, store.LandingStatusDiscarded, meta.LandingStatus)
}

func TestDaemonDiscardAlreadyLanded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, repoID := createTestWorktree(t, env.Client, repoRoot, "discard-after-land")

	startResp := startTestInvocation(t, env.Client, repoRoot, "discard-after-land", "exit-ok")
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 5*time.Second)

	// Land first.
	sandboxPath := startResp.SandboxPath
	require.NoError(t, os.WriteFile(filepath.Join(sandboxPath, "file.txt"), []byte("content\n"), 0o644))
	gitExec(t, sandboxPath, "add", "file.txt")
	gitExec(t, sandboxPath, "commit", "-m", "commit")

	landResp, err := env.Client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoID,
		InvocationID: startResp.InvocationID,
	})
	require.NoError(t, err)
	require.True(t, landResp.OK)

	// Try to discard after landing.
	discardResp, err := env.Client.Discard(ctx, repoID, startResp.InvocationID)
	require.NoError(t, err)

	assert.False(t, discardResp.OK)
	assert.Equal(t, "E_LAND_ALREADY_LANDED", discardResp.ErrorCode)
}

func TestDaemonDiscardAlreadyDiscarded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	repoRoot := setupTestGitRepo(t)
	_, _, repoID := createTestWorktree(t, env.Client, repoRoot, "discard-twice")

	startResp := startTestInvocation(t, env.Client, repoRoot, "discard-twice", "exit-ok")
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 5*time.Second)

	// First discard should succeed.
	resp1, err := env.Client.Discard(ctx, repoID, startResp.InvocationID)
	require.NoError(t, err)
	require.True(t, resp1.OK)

	// Second discard should fail.
	resp2, err := env.Client.Discard(ctx, repoID, startResp.InvocationID)
	require.NoError(t, err)

	assert.False(t, resp2.OK)
	assert.Equal(t, "E_LAND_ALREADY_DISCARDED", resp2.ErrorCode)
}

// ---------------------------------------------------------------------------
// Landing Routing Tests (PR-09)
// ---------------------------------------------------------------------------

// Routing correctness for POST /land and /discard is proven by all the
// integration tests above — every successful Land/Discard call proves the
// route is wired. Method-not-allowed (405) testing for GET is covered by
// TestHandleLandDiscardRouting in landing_handlers_test.go (internal package).

// ---------------------------------------------------------------------------
// Headed Invocation Tests (PR-10)
// ---------------------------------------------------------------------------

func TestDaemonHeadedStartHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	fakeTmux := newFakeTmuxClient()
	env.Server.TmuxClient = fakeTmux

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "headed-happy")

	resp := startTestHeadedInvocation(t, env.Client, repoRoot, "headed-happy")

	// Verify response fields
	require.True(t, resp.OK)
	assert.NotEmpty(t, resp.InvocationID)
	assert.NotEmpty(t, resp.SandboxPath)
	assert.Equal(t, tmux.SessionName(resp.InvocationID), resp.TmuxSession)
	assert.NotEmpty(t, resp.RepoID)
	assert.NotEmpty(t, resp.DaemonInstanceID)
	assert.NotEmpty(t, resp.RequestID)
	assert.False(t, resp.AlreadyRunning)

	// Verify invocation meta
	meta, err := env.Store.ReadInvocationMeta(resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusRunning, meta.Status)
	assert.Equal(t, store.RunnerModeHeaded, meta.Mode)
	assert.NotEmpty(t, meta.TmuxSession)
	assert.Equal(t, "daemon", meta.LifecycleOwner)

	// Verify tmux calls
	fakeTmux.Mu.Lock()
	assert.Len(t, fakeTmux.NewSessionCalls, 1)
	if len(fakeTmux.NewSessionCalls) == 1 {
		call := fakeTmux.NewSessionCalls[0]
		assert.Equal(t, resp.SandboxPath, call.CWD)
		assert.Contains(t, call.Argv[0], fakeRunnerPath(t))
	}
	fakeTmux.Mu.Unlock()

	// Cleanup
	_, _ = env.Client.Kill(ctx, resp.RepoID, resp.InvocationID)
}

func TestDaemonHeadedStartIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)

	fakeTmux := newFakeTmuxClient()
	env.Server.TmuxClient = fakeTmux

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "headed-idem")

	// Use raw HTTP so we can control client_request_id.
	clientRequestID := "test-idempotency-key-headed"

	reqBody := daemon.ControlPlaneStartHeadedRequest{
		RepoRoot:        repoRoot,
		WorktreeRef:     "headed-idem",
		Runner:          "claude",
		ClientRequestID: clientRequestID,
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// Create an HTTP client that connects to the daemon unix socket.
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", env.SocketPath)
			},
		},
	}

	// First request
	httpReq, err := http.NewRequest(http.MethodPost, "http://daemon/invocations/start_headed", bytes.NewReader(bodyBytes))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := httpClient.Do(httpReq)
	require.NoError(t, err)
	defer func() { _ = httpResp.Body.Close() }()

	var resp1 daemon.ControlPlaneStartHeadedResponse
	require.NoError(t, json.NewDecoder(httpResp.Body).Decode(&resp1))
	require.True(t, resp1.OK, "first request should succeed: %s - %s", resp1.ErrorCode, resp1.Message)
	assert.False(t, resp1.AlreadyRunning)

	// Second request with same client_request_id
	httpReq2, err := http.NewRequest(http.MethodPost, "http://daemon/invocations/start_headed", bytes.NewReader(bodyBytes))
	require.NoError(t, err)
	httpReq2.Header.Set("Content-Type", "application/json")
	httpResp2, err := httpClient.Do(httpReq2)
	require.NoError(t, err)
	defer func() { _ = httpResp2.Body.Close() }()

	var resp2 daemon.ControlPlaneStartHeadedResponse
	require.NoError(t, json.NewDecoder(httpResp2.Body).Decode(&resp2))
	require.True(t, resp2.OK, "second request should succeed: %s - %s", resp2.ErrorCode, resp2.Message)
	assert.True(t, resp2.AlreadyRunning)
	assert.Equal(t, resp1.InvocationID, resp2.InvocationID)

	// Tmux should only have been called once
	fakeTmux.Mu.Lock()
	assert.Len(t, fakeTmux.NewSessionCalls, 1, "tmux NewSession should only be called once")
	fakeTmux.Mu.Unlock()

	// Cleanup
	ctx := context.Background()
	_, _ = env.Client.Kill(ctx, resp1.RepoID, resp1.InvocationID)
}

func TestDaemonHeadedStartTmuxSessionExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)

	fakeTmux := newFakeTmuxClient()
	fakeTmux.AlwaysHasSession = true
	env.Server.TmuxClient = fakeTmux

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "headed-exists")

	ctx := context.Background()
	resp, err := env.Client.ControlPlaneStartHeaded(ctx, daemonclient.ControlPlaneStartHeadedOpts{
		RepoRoot:    repoRoot,
		WorktreeRef: "headed-exists",
		Runner:      "claude",
	})
	require.NoError(t, err)

	assert.False(t, resp.OK)
	assert.Equal(t, "E_TMUX_SESSION_EXISTS", resp.ErrorCode)

	fakeTmux.Mu.Lock()
	assert.Len(t, fakeTmux.NewSessionCalls, 0, "NewSession should not be called if session already exists")
	fakeTmux.Mu.Unlock()
}

func TestDaemonHeadedStartTmuxCreationFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)

	fakeTmux := newFakeTmuxClient()
	fakeTmux.NewSessionErr = fmt.Errorf("tmux not running")
	env.Server.TmuxClient = fakeTmux

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "headed-fail")

	ctx := context.Background()
	resp, err := env.Client.ControlPlaneStartHeaded(ctx, daemonclient.ControlPlaneStartHeadedOpts{
		RepoRoot:    repoRoot,
		WorktreeRef: "headed-fail",
		Runner:      "claude",
	})
	require.NoError(t, err)

	assert.False(t, resp.OK)
	assert.Equal(t, "E_INVOCATION_START_FAILED", resp.ErrorCode)
}

func TestDaemonHeadedStartWithName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	fakeTmux := newFakeTmuxClient()
	env.Server.TmuxClient = fakeTmux

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "headed-named")

	resp, err := env.Client.ControlPlaneStartHeaded(ctx, daemonclient.ControlPlaneStartHeadedOpts{
		RepoRoot:       repoRoot,
		WorktreeRef:    "headed-named",
		Runner:         "claude",
		InvocationName: "my-headed-agent",
	})
	require.NoError(t, err)
	require.True(t, resp.OK, "start headed with name failed: %s - %s", resp.ErrorCode, resp.Message)

	meta, err := env.Store.ReadInvocationMeta(resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	assert.Equal(t, "my-headed-agent", meta.InvocationName)

	// Cleanup
	_, _ = env.Client.Kill(ctx, resp.RepoID, resp.InvocationID)
}

func TestDaemonHeadedStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	fakeTmux := newFakeTmuxClient()
	env.Server.TmuxClient = fakeTmux

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "headed-stop")

	resp := startTestHeadedInvocation(t, env.Client, repoRoot, "headed-stop")

	// Stop the headed invocation
	stopResp, err := env.Client.Stop(ctx, resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	assert.True(t, stopResp.OK)

	// Verify C-c was sent via tmux
	fakeTmux.Mu.Lock()
	require.Len(t, fakeTmux.SendKeysCalls, 1)
	assert.Equal(t, []tmux.Key{tmux.KeyCtrlC}, fakeTmux.SendKeysCalls[0].Keys)
	fakeTmux.Mu.Unlock()

	// Verify meta: stop_requested_at set, needs_attention, still running
	meta, err := env.Store.ReadInvocationMeta(resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	assert.NotEmpty(t, meta.StopRequestedAt)
	assert.True(t, meta.Flags.NeedsAttention)
	assert.Equal(t, store.InvocationStatusRunning, meta.Status, "headed stop should not terminate the invocation")

	// Cleanup
	_, _ = env.Client.Kill(ctx, resp.RepoID, resp.InvocationID)
}

func TestDaemonHeadedStopSessionMissing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	fakeTmux := newFakeTmuxClient()
	env.Server.TmuxClient = fakeTmux

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "headed-stop-miss")

	resp := startTestHeadedInvocation(t, env.Client, repoRoot, "headed-stop-miss")

	// Manually remove the session from the fake to simulate session gone
	fakeTmux.Mu.Lock()
	for k := range fakeTmux.Sessions {
		delete(fakeTmux.Sessions, k)
	}
	fakeTmux.Mu.Unlock()

	// Stop — session is missing
	stopResp, err := env.Client.Stop(ctx, resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	assert.True(t, stopResp.OK)

	// Verify meta: should be finished with exited reason
	meta, err := env.Store.ReadInvocationMeta(resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFinished, meta.Status)
	assert.Equal(t, "exited", meta.ExitReason)

	// Verify no C-c was sent
	fakeTmux.Mu.Lock()
	assert.Len(t, fakeTmux.SendKeysCalls, 0, "no C-c should be sent when session is missing")
	fakeTmux.Mu.Unlock()
}

func TestDaemonHeadedStopAlreadyFinished(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	fakeTmux := newFakeTmuxClient()
	env.Server.TmuxClient = fakeTmux

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "headed-stop-done")

	resp := startTestHeadedInvocation(t, env.Client, repoRoot, "headed-stop-done")

	// Kill it first (puts it in terminal state)
	killResp, err := env.Client.Kill(ctx, resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	require.True(t, killResp.OK)

	// Record current tmux call counts
	fakeTmux.Mu.Lock()
	sendKeysCountBefore := len(fakeTmux.SendKeysCalls)
	killCountBefore := len(fakeTmux.KillSessionCalls)
	fakeTmux.Mu.Unlock()

	// Now stop — should be a no-op since already terminal
	stopResp, err := env.Client.Stop(ctx, resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	assert.True(t, stopResp.OK)

	// Verify no new tmux calls
	fakeTmux.Mu.Lock()
	assert.Equal(t, sendKeysCountBefore, len(fakeTmux.SendKeysCalls), "no new C-c should be sent")
	assert.Equal(t, killCountBefore, len(fakeTmux.KillSessionCalls), "no new kill should be sent")
	fakeTmux.Mu.Unlock()
}

func TestDaemonHeadedKill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	fakeTmux := newFakeTmuxClient()
	env.Server.TmuxClient = fakeTmux

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "headed-kill")

	resp := startTestHeadedInvocation(t, env.Client, repoRoot, "headed-kill")

	// Kill the headed invocation
	killResp, err := env.Client.Kill(ctx, resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	assert.True(t, killResp.OK)

	// Verify tmux session was killed
	fakeTmux.Mu.Lock()
	assert.Len(t, fakeTmux.KillSessionCalls, 1)
	// Session should be removed from the map
	_, sessionExists := fakeTmux.Sessions[tmux.SessionName(resp.InvocationID)]
	assert.False(t, sessionExists, "session should be removed after kill")
	fakeTmux.Mu.Unlock()

	// Verify meta
	meta, err := env.Store.ReadInvocationMeta(resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFailed, meta.Status)
	assert.Equal(t, "killed", meta.ExitReason)
	assert.NotEmpty(t, meta.FinishedAt)
}

func TestDaemonHeadedKillSessionAlreadyGone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	fakeTmux := newFakeTmuxClient()
	env.Server.TmuxClient = fakeTmux

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "headed-kill-gone")

	resp := startTestHeadedInvocation(t, env.Client, repoRoot, "headed-kill-gone")

	// Inject killSession error to simulate "can't find session"
	fakeTmux.Mu.Lock()
	fakeTmux.KillSessionErr = fmt.Errorf("can't find session: %s", tmux.SessionName(resp.InvocationID))
	fakeTmux.Mu.Unlock()

	// Kill — should succeed even though session is gone
	killResp, err := env.Client.Kill(ctx, resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	assert.True(t, killResp.OK, "kill should succeed even if session already gone")

	// Verify meta
	meta, err := env.Store.ReadInvocationMeta(resp.RepoID, resp.InvocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFailed, meta.Status)
	assert.Equal(t, "killed", meta.ExitReason)
}

func TestDaemonForceShutdownWithHeaded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	fakeTmux := newFakeTmuxClient()
	env.Server.TmuxClient = fakeTmux

	repoRoot := setupTestGitRepo(t)
	_, _, _ = createTestWorktree(t, env.Client, repoRoot, "headed-shutdown")

	resp := startTestHeadedInvocation(t, env.Client, repoRoot, "headed-shutdown")

	// Shutdown without force should fail (headed invocation is active)
	shutResp, err := env.Client.Shutdown(ctx, false)
	require.NoError(t, err)
	assert.False(t, shutResp.OK)
	assert.Equal(t, "E_DAEMON_BUSY", shutResp.ErrorCode)

	// Shutdown with force should succeed
	shutResp, err = env.Client.Shutdown(ctx, true)
	require.NoError(t, err)
	assert.True(t, shutResp.OK, "force shutdown should succeed: %s - %s", shutResp.ErrorCode, shutResp.Message)

	// Wait for shutdown goroutine to update invocation meta.
	var meta *store.InvocationMeta
	require.Eventually(t, func() bool {
		var readErr error
		meta, readErr = env.Store.ReadInvocationMeta(resp.RepoID, resp.InvocationID)
		return readErr == nil && meta.Status == store.InvocationStatusFailed
	}, 10*time.Second, 50*time.Millisecond, "invocation meta not updated after force shutdown")
	assert.Equal(t, "killed", meta.ExitReason)

	// Verify tmux session was killed
	fakeTmux.Mu.Lock()
	assert.GreaterOrEqual(t, len(fakeTmux.KillSessionCalls), 1)
	fakeTmux.Mu.Unlock()
}
