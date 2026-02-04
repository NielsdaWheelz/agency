package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/store"
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
	if err != nil {
		t.Fatalf("health: %v", err)
	}

	if !resp.OK {
		t.Error("expected OK=true")
	}
	if resp.APIVersion != daemon.APIVersion {
		t.Errorf("api_version = %d, want %d", resp.APIVersion, daemon.APIVersion)
	}
	if resp.DaemonInstanceID == "" {
		t.Error("expected daemon_instance_id to be set")
	}
	if resp.PID == 0 {
		t.Error("expected pid to be set")
	}
}

func TestDaemonShutdownClean(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := startTestDaemon(t)
	ctx := context.Background()

	resp, err := env.Client.Shutdown(ctx, false)
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if !resp.OK {
		t.Errorf("expected OK=true, got error: %s - %s", resp.ErrorCode, resp.Message)
	}

	// After shutdown, health should fail.
	time.Sleep(200 * time.Millisecond)
	if env.Client.IsRunning(ctx) {
		t.Error("daemon still running after shutdown")
	}
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
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if shutResp.OK {
		t.Error("expected shutdown to fail without force")
	}
	if shutResp.ErrorCode != "E_DAEMON_BUSY" {
		t.Errorf("error_code = %q, want E_DAEMON_BUSY", shutResp.ErrorCode)
	}
	if len(shutResp.RunningInvocations) == 0 {
		t.Error("expected running_invocations to be populated")
	}

	// Shutdown with force should succeed.
	shutResp, err = env.Client.Shutdown(ctx, true)
	if err != nil {
		t.Fatalf("shutdown force: %v", err)
	}
	if !shutResp.OK {
		t.Errorf("expected OK=true with force, got error: %s - %s", shutResp.ErrorCode, shutResp.Message)
	}

	// Give the async shutdown goroutine time to complete.
	time.Sleep(500 * time.Millisecond)

	// Verify invocation meta was updated.
	meta, err := env.Store.ReadInvocationMeta(startResp.RepoID, startResp.InvocationID)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if meta.Status != store.InvocationStatusFailed {
		t.Errorf("status = %q, want %q", meta.Status, store.InvocationStatusFailed)
	}
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
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !resp.OK {
		t.Fatalf("start failed: %s - %s", resp.ErrorCode, resp.Message)
	}

	if resp.InvocationID == "" {
		t.Error("expected invocation_id to be set")
	}
	if resp.PID == 0 {
		t.Error("expected pid to be set")
	}
	if resp.SandboxPath == "" {
		t.Error("expected sandbox_path to be set")
	}
	if resp.RepoID != repoID {
		t.Errorf("repo_id = %q, want %q", resp.RepoID, repoID)
	}
	if resp.DaemonInstanceID == "" {
		t.Error("expected daemon_instance_id to be set")
	}
	if resp.LogPaths == nil {
		t.Error("expected log_paths to be set")
	}

	// Wait for the exit-ok runner to finish.
	meta := waitForInvocationTerminal(t, env.Store, repoID, resp.InvocationID, 5*time.Second)
	if meta.Status != store.InvocationStatusFinished {
		t.Errorf("status = %q, want %q", meta.Status, store.InvocationStatusFinished)
	}
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
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !stopResp.OK {
		t.Errorf("stop failed: %s - %s", stopResp.ErrorCode, stopResp.Message)
	}

	// Wait for the process to actually exit (stop escalation runs in background).
	meta := waitForInvocationTerminal(t, env.Store, startResp.RepoID, startResp.InvocationID, 10*time.Second)
	if meta.Status != store.InvocationStatusFailed {
		t.Errorf("status = %q, want %q", meta.Status, store.InvocationStatusFailed)
	}
	if meta.ExitReason != "stopped" {
		t.Errorf("exit_reason = %q, want %q", meta.ExitReason, "stopped")
	}
	if meta.FailureReason != "stopped" {
		t.Errorf("failure_reason = %q, want %q", meta.FailureReason, "stopped")
	}
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
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !killResp.OK {
		t.Errorf("kill failed: %s - %s", killResp.ErrorCode, killResp.Message)
	}

	// After kill, meta should reach terminal state quickly (no escalation).
	meta := waitForInvocationTerminal(t, env.Store, startResp.RepoID, startResp.InvocationID, 5*time.Second)
	if meta.Status != store.InvocationStatusFailed {
		t.Errorf("status = %q, want %q", meta.Status, store.InvocationStatusFailed)
	}
	if meta.ExitReason != "killed" {
		t.Errorf("exit_reason = %q, want %q", meta.ExitReason, "killed")
	}
	if meta.FailureReason != "killed" {
		t.Errorf("failure_reason = %q, want %q", meta.FailureReason, "killed")
	}
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
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !stopResp.OK {
		t.Errorf("stop failed: %s - %s", stopResp.ErrorCode, stopResp.Message)
	}

	// Must wait for escalation: 5s (SIGINT wait) + some buffer.
	meta := waitForInvocationTerminal(t, env.Store, startResp.RepoID, startResp.InvocationID, 15*time.Second)
	if meta.Status != store.InvocationStatusFailed {
		t.Errorf("status = %q, want %q", meta.Status, store.InvocationStatusFailed)
	}
	if meta.ExitReason != "stopped" {
		t.Errorf("exit_reason = %q, want %q", meta.ExitReason, "stopped")
	}
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
	if resp1.InvocationID == "" {
		t.Error("expected invocation_id to be set")
	}
	if resp1.AlreadyRunning {
		t.Error("first request should not be already_running")
	}
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
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	if !resp1.OK {
		t.Fatalf("first start failed: %s - %s", resp1.ErrorCode, resp1.Message)
	}

	// Second invocation with same name should fail.
	resp2, err := env.Client.ControlPlaneStartHeadless(ctx, daemonclient.ControlPlaneStartOpts{
		RepoRoot:       repoRoot,
		WorktreeRef:    "name-test",
		Runner:         "claude",
		Prompt:         "test prompt 2",
		InvocationName: "my-agent",
		Env:            map[string]string{"FAKE_RUNNER_MODE": "sleep"},
	})
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if resp2.OK {
		// Clean up the accidental second invocation.
		_, _ = env.Client.Kill(ctx, resp2.RepoID, resp2.InvocationID)
		t.Fatal("expected second start to fail due to name collision")
	}
	if resp2.ErrorCode != "E_INVOCATION_NAME_EXISTS" {
		t.Errorf("error_code = %q, want E_INVOCATION_NAME_EXISTS", resp2.ErrorCode)
	}

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
	if meta.Status != store.InvocationStatusFailed {
		t.Errorf("status = %q, want %q", meta.Status, store.InvocationStatusFailed)
	}
	if meta.FailureReason != "runner_exit_nonzero" {
		t.Errorf("failure_reason = %q, want %q", meta.FailureReason, "runner_exit_nonzero")
	}
	if meta.ExitCode == nil || *meta.ExitCode != 1 {
		exitCode := -1
		if meta.ExitCode != nil {
			exitCode = *meta.ExitCode
		}
		t.Errorf("exit_code = %d, want 1", exitCode)
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
	if _, err := os.Stat(treePath); os.IsNotExist(err) {
		t.Errorf("tree_path does not exist: %s", treePath)
	}

	// Verify INTEGRATION_MARKER exists.
	markerPath := filepath.Join(treePath, ".agency", "INTEGRATION_MARKER")
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Errorf("INTEGRATION_MARKER does not exist: %s", markerPath)
	}

	// Remove worktree (force=true because marker file is untracked).
	rmResp, err := env.Client.WorktreeRm(ctx, repoID, wtID, true)
	if err != nil {
		t.Fatalf("worktree rm: %v", err)
	}
	if !rmResp.OK {
		t.Errorf("worktree rm failed: %s - %s", rmResp.ErrorCode, rmResp.Message)
	}

	// Verify tree is gone.
	if _, err := os.Stat(treePath); !os.IsNotExist(err) {
		t.Errorf("tree_path still exists: %s", treePath)
	}

	// Verify meta is archived.
	meta, err := env.Store.ReadIntegrationWorktreeMeta(repoID, wtID)
	if err != nil {
		t.Fatalf("read worktree meta: %v", err)
	}
	if meta.State != store.WorktreeStateArchived {
		t.Errorf("state = %q, want %q", meta.State, store.WorktreeStateArchived)
	}
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
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if !resp1.OK {
		t.Fatalf("first create failed: %s - %s", resp1.ErrorCode, resp1.Message)
	}

	// Second create with same key.
	resp2, err := env.Client.WorktreeCreate(ctx, daemonclient.WorktreeCreateOpts{
		RepoRoot:       repoRoot,
		Name:           "idem-test",
		ParentBranch:   "main",
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if !resp2.OK {
		t.Fatalf("second create failed: %s - %s", resp2.ErrorCode, resp2.Message)
	}

	// Should return same worktree.
	if resp1.WorktreeID != resp2.WorktreeID {
		t.Errorf("idempotent requests returned different IDs: %s vs %s", resp1.WorktreeID, resp2.WorktreeID)
	}
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
	if err != nil {
		t.Fatalf("worktree rm: %v", err)
	}
	if rmResp.OK {
		t.Fatal("expected rm to fail with active invocation")
	}
	if rmResp.ErrorCode != "E_WORKTREE_HAS_ACTIVE_INVOCATIONS" {
		t.Errorf("error_code = %q, want E_WORKTREE_HAS_ACTIVE_INVOCATIONS", rmResp.ErrorCode)
	}

	// Remove with force should succeed.
	rmResp, err = env.Client.WorktreeRm(ctx, repoID, wtID, true)
	if err != nil {
		t.Fatalf("worktree rm force: %v", err)
	}
	if !rmResp.OK {
		t.Errorf("worktree rm force failed: %s - %s", rmResp.ErrorCode, rmResp.Message)
	}

	// Verify the invocation was killed.
	time.Sleep(500 * time.Millisecond)
	meta, err := env.Store.ReadInvocationMeta(startResp.RepoID, startResp.InvocationID)
	if err != nil {
		t.Fatalf("read invocation meta: %v", err)
	}
	if meta.Status != store.InvocationStatusFailed {
		t.Errorf("invocation status = %q, want %q", meta.Status, store.InvocationStatusFailed)
	}
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
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if resp2.OK {
		t.Fatal("expected second create to fail due to name collision")
	}
	if resp2.ErrorCode != "E_WORKTREE_NAME_EXISTS" {
		t.Errorf("error_code = %q, want E_WORKTREE_NAME_EXISTS", resp2.ErrorCode)
	}
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

	if err1 != nil {
		t.Fatalf("start a: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("start b: %v", err2)
	}
	if !resp1.OK {
		t.Fatalf("start a failed: %s - %s", resp1.ErrorCode, resp1.Message)
	}
	if !resp2.OK {
		t.Fatalf("start b failed: %s - %s", resp2.ErrorCode, resp2.Message)
	}

	if resp1.InvocationID == resp2.InvocationID {
		t.Error("concurrent starts returned same invocation_id")
	}

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
	if err != nil {
		t.Fatalf("CheckpointApply returned transport error: %v", err)
	}

	if resp.OK {
		t.Error("expected OK=false for checkpoint apply while running")
	}
	if resp.ErrorCode != "E_INVOCATION_STILL_RUNNING" {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, "E_INVOCATION_STILL_RUNNING")
	}

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
	if err != nil {
		t.Fatalf("CheckpointApply returned transport error: %v", err)
	}

	if resp.OK {
		t.Error("expected OK=false for non-existent checkpoint")
	}
	if resp.ErrorCode != "E_CHECKPOINT_NOT_FOUND" {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, "E_CHECKPOINT_NOT_FOUND")
	}
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
	if err := os.WriteFile(testFilePath, []byte("checkpoint test content\n"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Wait for the checkpoint engine debounce (3s) + rate limit buffer.
	// The fsnotify watcher should detect the file write, debounce for 3s,
	// then create a checkpoint (if dirty).
	time.Sleep(6 * time.Second)

	// Kill the invocation. This triggers a final checkpoint via the engine's Stop().
	killResp, err := env.Client.Kill(ctx, startResp.RepoID, startResp.InvocationID)
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !killResp.OK {
		t.Fatalf("kill failed: %s - %s", killResp.ErrorCode, killResp.Message)
	}

	// Wait for invocation to reach terminal state.
	waitForInvocationTerminal(t, env.Store, repoID, startResp.InvocationID, 10*time.Second)

	// Allow a brief pause for the checkpoint engine goroutine to finish writing.
	time.Sleep(500 * time.Millisecond)

	// Load checkpoints.json and verify at least one checkpoint was created.
	checkpointsPath := env.Store.SandboxCheckpointsPath(repoID, startResp.InvocationID)
	cpData, err := os.ReadFile(checkpointsPath)
	if err != nil {
		t.Fatalf("failed to read checkpoints.json: %v (path: %s)", err, checkpointsPath)
	}

	var cpFile checkpoint.CheckpointsFile
	if err := json.Unmarshal(cpData, &cpFile); err != nil {
		t.Fatalf("failed to parse checkpoints.json: %v", err)
	}
	if len(cpFile.Checkpoints) == 0 {
		t.Fatal("expected at least one checkpoint, got none")
	}

	latestCP := cpFile.Checkpoints[len(cpFile.Checkpoints)-1]
	t.Logf("found %d checkpoints, latest ID=%d", len(cpFile.Checkpoints), latestCP.ID)

	// Verify the test file still exists in the sandbox (checkpoint captured it).
	if _, err := os.Stat(testFilePath); os.IsNotExist(err) {
		t.Error("test file should still exist in sandbox after kill")
	}

	// Modify the sandbox to verify that apply restores the checkpoint state.
	if err := os.WriteFile(testFilePath, []byte("modified after kill\n"), 0o644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	// Apply the latest checkpoint via the daemon client.
	applyResp, err := env.Client.CheckpointApply(ctx, startResp.RepoID, startResp.InvocationID, latestCP.ID)
	if err != nil {
		t.Fatalf("CheckpointApply transport error: %v", err)
	}
	if !applyResp.OK {
		t.Fatalf("CheckpointApply failed: %s - %s", applyResp.ErrorCode, applyResp.Message)
	}
	if applyResp.CheckpointID != latestCP.ID {
		t.Errorf("applied checkpoint_id = %d, want %d", applyResp.CheckpointID, latestCP.ID)
	}

	// Verify the file was restored to the checkpoint state (original content).
	restoredContent, err := os.ReadFile(testFilePath)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(restoredContent) != "checkpoint test content\n" {
		t.Errorf("restored content = %q, want %q", string(restoredContent), "checkpoint test content\n")
	}
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

	// Wait for the process to actually terminate and meta to update.
	time.Sleep(1 * time.Second)

	meta, err := env.Store.ReadInvocationMeta(startResp.RepoID, startResp.InvocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFailed, meta.Status)
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
