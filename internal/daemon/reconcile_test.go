package daemon_test

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// ---------------------------------------------------------------------------
// PR-11: Headed Reconciliation Unit Tests
// ---------------------------------------------------------------------------

// setupReconcileTestEnv creates a minimal daemon environment for reconciliation tests.
// Returns the server and store. Use waitForStatus / waitForStableStatus to poll.
func setupReconcileTestEnv(t *testing.T, fakeTmux *testutil.FakeTmuxClient) (*daemon.Server, *store.Store) {
	t.Helper()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	srv := daemon.NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	// Inject fake tmux client
	srv.TmuxClient = fakeTmux

	// Use a reconcile interval longer than the wait time to ensure
	// exactly one tick per waitForReconcile call (200ms interval, 150ms wait).
	testInterval := 200 * time.Millisecond
	srv.HeadedReconcileIntervalOverride = &testInterval

	// Fixed clock for predictable timestamps
	fixedTime := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	srv.Clock = func() time.Time { return fixedTime }

	return srv, st
}

// startTestServer starts the daemon server and returns a cleanup function.
// The server is given enough time to start and run the recovery scan,
// but not enough for the first reconcile tick (which happens at 200ms interval).
func startTestServer(t *testing.T, srv *daemon.Server) func() {
	t.Helper()

	// Create a listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")

	// Start server in background
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- srv.Serve(listener)
	}()

	// Wait for server startup and recovery scan to complete,
	// but not long enough for the first reconcile tick (200ms interval).
	time.Sleep(30 * time.Millisecond)

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-serveDone
	}
}

// createTestHeadedInvocationMeta creates a test headed invocation record.
// startedAtOffset controls how old the startedAt timestamp is relative to the fixed clock time.
// Use negative values for recent times (e.g., -10s means 10 seconds ago).
func createTestHeadedInvocationMeta(t *testing.T, st *store.Store, repoID, invocationID string, status store.InvocationStatus, tmuxSession string) {
	t.Helper()
	createTestHeadedInvocationMetaWithAge(t, st, repoID, invocationID, status, tmuxSession, -10*time.Second)
}

// createTestHeadedInvocationMetaWithAge creates a test headed invocation record with a specified age.
func createTestHeadedInvocationMetaWithAge(t *testing.T, st *store.Store, repoID, invocationID string, status store.InvocationStatus, tmuxSession string, ageOffset time.Duration) {
	t.Helper()

	// Ensure directories exist
	_, err := st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err, "ensure invocation dir")

	// Fixed clock time is 2026-02-05T12:00:00Z
	fixedTime := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	startedAt := fixedTime.Add(ageOffset)

	meta := &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          invocationID,
		IntegrationWorktreeID: "test-worktree",
		SandboxPath:           "/tmp/sandbox",
		SandboxBranch:         "agency/sandbox-" + invocationID,
		BaseCommit:            "abc123",
		Runner:                "claude",
		Mode:                  store.RunnerModeHeaded,
		TmuxSession:           tmuxSession,
		StartedAt:             startedAt.Format(time.RFC3339),
		Status:                status,
	}

	err = st.WriteInvocationMeta(repoID, invocationID, meta)
	require.NoError(t, err, "write invocation meta")
}

// ensureRepoDir creates the repo directory structure for tests.
func ensureRepoDir(t *testing.T, st *store.Store, repoID string) {
	t.Helper()
	repoDir := st.RepoDir(repoID)
	err := fs.NewRealFS().MkdirAll(repoDir, 0o700)
	require.NoError(t, err, "create repo dir")

	// Create repo_index.json so daemon can find the repo
	idx := store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			repoID: {
				RepoID:     repoID,
				Paths:      []string{"/tmp/repo"},
				LastSeenAt: "2026-02-05T12:00:00Z",
			},
		},
	}
	err = st.SaveRepoIndex(idx)
	require.NoError(t, err, "save repo index")
}

// waitForStatus polls the store until the invocation reaches the expected status.
// Uses require.Eventually with tight polling interval.
func waitForStatus(t *testing.T, st *store.Store, repoID, invocationID string, expected store.InvocationStatus) *store.InvocationMeta {
	t.Helper()
	var result *store.InvocationMeta
	require.Eventually(t, func() bool {
		meta, err := st.ReadInvocationMeta(repoID, invocationID)
		if err != nil {
			return false
		}
		if meta.Status == expected {
			result = meta
			return true
		}
		return false
	}, 3*time.Second, 50*time.Millisecond, "invocation %s did not reach status %s", invocationID, expected)
	return result
}

// waitForStableStatus polls the store and confirms the invocation STAYS at the expected status
// after at least N reconcile ticks. Used for negative tests (asserting no transition).
func waitForStableStatus(t *testing.T, st *store.Store, repoID, invocationID string, expected store.InvocationStatus, duration time.Duration) {
	t.Helper()
	// Wait for enough ticks to have fired
	time.Sleep(duration)
	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, expected, meta.Status, "status should remain %s", expected)
}

func TestReconcile_RunningToFinished(t *testing.T) {
	t.Parallel()
	// Test: Running → Finished when tmux session disappears
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-1"
	invocationID := "20260205120000-a1b2"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create a running headed invocation
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusRunning, sessionName)

	// Initially, tmux session exists
	fakeTmux.Sessions[sessionName] = testutil.FakeTmuxSession{Name: sessionName}

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Verify invocation is still running
	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusRunning, meta.Status)

	// Now remove the tmux session (simulating session exit)
	fakeTmux.Mu.Lock()
	delete(fakeTmux.Sessions, sessionName)
	fakeTmux.Mu.Unlock()

	// Wait for reconciliation to transition to finished
	meta = waitForStatus(t, st, repoID, invocationID, store.InvocationStatusFinished)
	assert.Equal(t, "exited", meta.ExitReason)
	assert.Equal(t, "2026-02-05T12:00:00Z", meta.FinishedAt)
	assert.Empty(t, meta.LifecycleOwner)
}

func TestReconcile_StartingToFailed_GraceWindow(t *testing.T) {
	t.Parallel()
	// Test: Starting → Failed only after grace window (2 consecutive ticks)
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-2"
	invocationID := "20260205120001-c3d4"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create a starting headed invocation (no tmux session exists)
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusStarting, sessionName)

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// After first reconcile tick, should still be starting (grace window)
	// Use waitForStableStatus to confirm it stays starting through one tick
	waitForStableStatus(t, st, repoID, invocationID, store.InvocationStatusStarting, 250*time.Millisecond)

	// After second reconcile tick, should be failed (grace window exceeded)
	meta := waitForStatus(t, st, repoID, invocationID, store.InvocationStatusFailed)
	assert.Equal(t, "start_failed", meta.ExitReason)
	assert.Equal(t, "tmux_session_missing", meta.FailureReason)
	assert.Equal(t, "2026-02-05T12:00:00Z", meta.FinishedAt)
	assert.Empty(t, meta.LifecycleOwner)
}

func TestReconcile_NoTransitionOnTransientError(t *testing.T) {
	t.Parallel()
	// Test: Transient tmux errors don't finalize invocation
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-3"
	invocationID := "20260205120002-e5f6"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create a running headed invocation
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusRunning, sessionName)

	// HasSession returns transient error (not "no session")
	fakeTmux.HasSessionErr = errors.New("connection refused")

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Verify invocation stays running through multiple ticks
	waitForStableStatus(t, st, repoID, invocationID, store.InvocationStatusRunning, 500*time.Millisecond)
}

func TestReconcile_Idempotence_TerminalUnchanged(t *testing.T) {
	t.Parallel()
	// Test: Terminal invocations remain unchanged
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-4"
	invocationID := "20260205120003-g7h8"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create a finished headed invocation
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusFinished, sessionName)

	// Set finished_at to verify it doesn't change
	originalFinishedAt := "2026-02-05T11:59:30Z"
	err := st.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		m.FinishedAt = originalFinishedAt
		m.ExitReason = "exited"
	})
	require.NoError(t, err)

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Verify invocation stays unchanged through multiple ticks
	waitForStableStatus(t, st, repoID, invocationID, store.InvocationStatusFinished, 500*time.Millisecond)

	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, originalFinishedAt, meta.FinishedAt, "finished_at should not change")
}

func TestReconcile_TmuxFlaps(t *testing.T) {
	t.Parallel()
	// Test: Adversarial tmux session flapping
	// For "running" invocations, recovery scan also calls HasSession, so we must account for that.
	// Sequence: error (recovery) → error (tick 1) → exists (tick 2) → not exists (tick 3)
	// Expected: no transition until tick 3

	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-5"
	invocationID := "20260205120004-i9j0"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create a running headed invocation
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusRunning, sessionName)

	// Track call count for sequential behavior
	// Note: Recovery scan calls HasSession once for "running" invocations,
	// so callCount=1 is recovery, callCount=2+ are reconcile ticks.
	var callCount int64
	fakeTmux.HasSessionFunc = func(name string) (bool, error) {
		c := atomic.AddInt64(&callCount, 1)
		switch c {
		case 1:
			// Recovery scan: transient error
			return false, errors.New("connection refused")
		case 2:
			// Tick 1: transient error
			return false, errors.New("connection refused")
		case 3:
			// Tick 2: session exists
			return true, nil
		default:
			// Tick 3+: session does not exist
			return false, nil
		}
	}

	// Start server (recovery scan runs, uses call 1)
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Should stay running through errors and session-exists ticks
	waitForStableStatus(t, st, repoID, invocationID, store.InvocationStatusRunning, 500*time.Millisecond)

	// Eventually finalize when session is gone
	meta := waitForStatus(t, st, repoID, invocationID, store.InvocationStatusFinished)
	assert.Equal(t, "exited", meta.ExitReason)
	assert.Equal(t, "2026-02-05T12:00:00Z", meta.FinishedAt)
	assert.Empty(t, meta.LifecycleOwner)
}

func TestReconcile_StartingGraceWindowReset(t *testing.T) {
	t.Parallel()
	// Test: Grace window resets when tmux session appears then disappears
	// Sequence:
	//   Call 1 (recovery): no session, but age <= 30s so left for reconcile loop
	//   Call 2 (tick 1): no session → grace count = 1
	//   Call 3 (tick 2): session exists → grace count reset to 0
	//   Call 4 (tick 3): no session → grace count = 1
	//   Call 5 (tick 4): no session → grace count = 2 → transition to failed

	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-6"
	invocationID := "20260205120005-k1l2"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create a starting headed invocation (no tmux session)
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusStarting, sessionName)

	var callCount int64
	fakeTmux.HasSessionFunc = func(name string) (bool, error) {
		c := atomic.AddInt64(&callCount, 1)
		switch c {
		case 1:
			// Recovery scan: no session (age <= 30s, left for loop)
			return false, nil
		case 2:
			// Tick 1: no session → grace count = 1
			return false, nil
		case 3:
			// Tick 2: session exists → grace count reset
			return true, nil
		case 4:
			// Tick 3: no session → grace count = 1
			return false, nil
		default:
			// Tick 4+: no session → grace count = 2 → failed
			return false, nil
		}
	}

	// Start server (recovery call fires = call 1)
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Ticks 1-2 fire: no session then session appears (grace reset).
	// Should still be starting after these ticks.
	waitForStableStatus(t, st, repoID, invocationID, store.InvocationStatusStarting, 500*time.Millisecond)

	// Ticks 3-4: no session twice → grace window exceeded → failed
	meta := waitForStatus(t, st, repoID, invocationID, store.InvocationStatusFailed)
	assert.Equal(t, "start_failed", meta.ExitReason)
	assert.Equal(t, "tmux_session_missing", meta.FailureReason)
	assert.Equal(t, "2026-02-05T12:00:00Z", meta.FinishedAt)
	assert.Empty(t, meta.LifecycleOwner)
}

func TestReconcile_HeadlessNoPID_Unaffected(t *testing.T) {
	t.Parallel()
	// Test: Headless invocations without PIDs are unaffected by reconciliation.
	// No PID means no liveness check is possible; leave for recovery scan.
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-7"
	invocationID := "20260205120006-m3n4"

	// Setup: Create a running headless invocation with no PID
	ensureRepoDir(t, st, repoID)
	_, err := st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)

	meta := &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          invocationID,
		IntegrationWorktreeID: "test-worktree",
		SandboxPath:           "/tmp/sandbox",
		SandboxBranch:         "agency/sandbox-" + invocationID,
		BaseCommit:            "abc123",
		Runner:                "claude",
		Mode:                  store.RunnerModeHeadless, // Headless!
		StartedAt:             time.Date(2026, 2, 5, 11, 59, 0, 0, time.UTC).Format(time.RFC3339),
		Status:                store.InvocationStatusRunning,
		// PID is nil — no liveness check possible
	}
	err = st.WriteInvocationMeta(repoID, invocationID, meta)
	require.NoError(t, err)

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Verify headless invocation stays running through multiple ticks
	waitForStableStatus(t, st, repoID, invocationID, store.InvocationStatusRunning, 500*time.Millisecond)
}

func TestReconcile_HeadlessDeadPID(t *testing.T) {
	t.Parallel()
	// Test: Headless invocation with dead PID transitions to failed.
	// This is the primary defense-in-depth fix for stale "running" status
	// when waitForExitWithFailureReason() fails to update meta.
	//
	// The PID is alive during recovery scan but dies afterwards.
	// The reconcile loop must detect the dead PID and mark as failed.
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	// PIDChecker: alive during recovery scan (call 1), dead on reconcile ticks
	var pidCheckCount int64
	srv.PIDChecker = func(pid int) bool {
		c := atomic.AddInt64(&pidCheckCount, 1)
		return c <= 1 // alive on first check (recovery), dead after
	}

	repoID := "test-repo-hl-dead"
	invocationID := "20260205120006-dead"

	ensureRepoDir(t, st, repoID)
	_, err := st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)

	pid := 99999
	meta := &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          invocationID,
		IntegrationWorktreeID: "test-worktree",
		SandboxPath:           "/tmp/sandbox",
		SandboxBranch:         "agency/sandbox-" + invocationID,
		BaseCommit:            "abc123",
		Runner:                "claude",
		Mode:                  store.RunnerModeHeadless,
		StartedAt:             time.Date(2026, 2, 5, 11, 59, 0, 0, time.UTC).Format(time.RFC3339),
		Status:                store.InvocationStatusRunning,
		PID:                   &pid,
	}
	err = st.WriteInvocationMeta(repoID, invocationID, meta)
	require.NoError(t, err)

	// Start server (recovery scan sees PID as alive, leaves it running)
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Reconcile loop should detect dead PID and transition to failed
	result := waitForStatus(t, st, repoID, invocationID, store.InvocationStatusFailed)
	assert.Equal(t, "unknown", result.ExitReason)
	assert.Equal(t, "runner_pid_dead", result.FailureReason)
	assert.Equal(t, "2026-02-05T12:00:00Z", result.FinishedAt)
	assert.True(t, result.Flags.NeedsAttention)
	assert.Nil(t, result.PID)
	assert.Empty(t, result.LifecycleOwner)
}

func TestReconcile_HeadlessAlivePID(t *testing.T) {
	t.Parallel()
	// Test: Headless invocation with alive PID stays running.
	// The process is alive — don't touch it.
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	// Override PIDChecker to report all PIDs as alive
	srv.PIDChecker = func(pid int) bool { return true }

	repoID := "test-repo-hl-alive"
	invocationID := "20260205120006-aliv"

	ensureRepoDir(t, st, repoID)
	_, err := st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)

	pid := 12345
	meta := &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          invocationID,
		IntegrationWorktreeID: "test-worktree",
		SandboxPath:           "/tmp/sandbox",
		SandboxBranch:         "agency/sandbox-" + invocationID,
		BaseCommit:            "abc123",
		Runner:                "claude",
		Mode:                  store.RunnerModeHeadless,
		StartedAt:             time.Date(2026, 2, 5, 11, 59, 0, 0, time.UTC).Format(time.RFC3339),
		Status:                store.InvocationStatusRunning,
		PID:                   &pid,
	}
	err = st.WriteInvocationMeta(repoID, invocationID, meta)
	require.NoError(t, err)

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Verify headless invocation stays running (PID is alive)
	waitForStableStatus(t, st, repoID, invocationID, store.InvocationStatusRunning, 500*time.Millisecond)
}

func TestReconcile_ShutdownOrdering(t *testing.T) {
	t.Parallel()
	// Test: Shutdown waits for reconcile loop to exit before terminating
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-8"
	invocationID := "20260205120007-o5p6"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create a running headed invocation
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusRunning, sessionName)

	// Session exists
	fakeTmux.Sessions[sessionName] = testutil.FakeTmuxSession{Name: sessionName}

	// Start server
	cleanup := startTestServer(t, srv)

	// Shutdown should complete without deadlock
	shutdownDone := make(chan struct{})
	go func() {
		cleanup()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		// Success - shutdown completed
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timed out - possible deadlock in reconcile loop ordering")
	}
}

// ---------------------------------------------------------------------------
// New tests: Landing status, idempotence, recovery, multi-invocation, fallback
// ---------------------------------------------------------------------------

func TestReconcile_LandingStatusPreserved(t *testing.T) {
	t.Parallel()
	// Test: LandingStatus is never modified through a transition
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-landing"
	invocationID := "20260205120010-land"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create a running headed invocation with LandingStatus = "pending"
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusRunning, sessionName)
	err := st.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		m.LandingStatus = store.LandingStatusPending
	})
	require.NoError(t, err)

	// Initially, tmux session exists
	fakeTmux.Sessions[sessionName] = testutil.FakeTmuxSession{Name: sessionName}

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Remove the tmux session to trigger transition
	fakeTmux.Mu.Lock()
	delete(fakeTmux.Sessions, sessionName)
	fakeTmux.Mu.Unlock()

	// Wait for transition to finished
	meta := waitForStatus(t, st, repoID, invocationID, store.InvocationStatusFinished)
	assert.Equal(t, store.LandingStatusPending, meta.LandingStatus, "landing_status should be preserved")
	assert.Equal(t, "exited", meta.ExitReason)
}

func TestReconcile_Idempotence_FailedUnchanged(t *testing.T) {
	t.Parallel()
	// Test: Failed terminal state is a no-op for reconciliation
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-failed-idem"
	invocationID := "20260205120011-fail"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create a failed headed invocation with specific fields
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusFailed, sessionName)

	originalFinishedAt := "2026-02-05T11:58:00Z"
	err := st.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		m.FinishedAt = originalFinishedAt
		m.ExitReason = "start_failed"
		m.FailureReason = "tmux_session_missing"
	})
	require.NoError(t, err)

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Verify invocation stays unchanged through multiple ticks
	waitForStableStatus(t, st, repoID, invocationID, store.InvocationStatusFailed, 500*time.Millisecond)

	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFailed, meta.Status)
	assert.Equal(t, originalFinishedAt, meta.FinishedAt, "finished_at should not change")
	assert.Equal(t, "start_failed", meta.ExitReason, "exit_reason should not change")
	assert.Equal(t, "tmux_session_missing", meta.FailureReason, "failure_reason should not change")
}

// ---------------------------------------------------------------------------
// Recovery scan tests
// ---------------------------------------------------------------------------

func TestRecovery_RunningNoSession(t *testing.T) {
	t.Parallel()
	// Test: Recovery scan: running + no tmux session → finished
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-rec-run"
	invocationID := "20260205120020-rec1"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create running headed invocation, age 60s (well past any grace)
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMetaWithAge(t, st, repoID, invocationID, store.InvocationStatusRunning, sessionName, -60*time.Second)

	// No tmux session in fake (default empty map)

	// Start server (recovery runs synchronously before HTTP listener)
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Recovery should have transitioned to finished
	meta := waitForStatus(t, st, repoID, invocationID, store.InvocationStatusFinished)
	assert.Equal(t, "exited", meta.ExitReason)
	assert.Equal(t, "2026-02-05T12:00:00Z", meta.FinishedAt)
	assert.Empty(t, meta.LifecycleOwner)
}

func TestRecovery_StartingOldNoSession(t *testing.T) {
	t.Parallel()
	// Test: Recovery scan: starting + age > 30s + no tmux → failed
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-rec-old"
	invocationID := "20260205120021-rec2"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create starting headed invocation, age 60s (> 30s threshold)
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMetaWithAge(t, st, repoID, invocationID, store.InvocationStatusStarting, sessionName, -60*time.Second)

	// No tmux session

	// Start server (recovery runs)
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Recovery should have transitioned to failed
	meta := waitForStatus(t, st, repoID, invocationID, store.InvocationStatusFailed)
	assert.Equal(t, "start_failed", meta.ExitReason)
	assert.Equal(t, "tmux_session_missing", meta.FailureReason)
	assert.Equal(t, "2026-02-05T12:00:00Z", meta.FinishedAt)
	assert.Empty(t, meta.LifecycleOwner)
}

func TestRecovery_StartingYoungNoSession(t *testing.T) {
	t.Parallel()
	// Test: Recovery scan: starting + age < 30s → left for reconcile loop → eventually failed
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-rec-young"
	invocationID := "20260205120022-rec3"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create starting headed invocation, age 5s (< 30s threshold)
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMetaWithAge(t, st, repoID, invocationID, store.InvocationStatusStarting, sessionName, -5*time.Second)

	// No tmux session

	// Start server (recovery leaves it for reconcile loop)
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Should still be starting immediately after recovery
	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusStarting, meta.Status, "recovery should leave young starting invocations for reconcile loop")

	// Wait for reconcile ticks to apply grace window (2 ticks)
	meta = waitForStatus(t, st, repoID, invocationID, store.InvocationStatusFailed)
	assert.Equal(t, "start_failed", meta.ExitReason)
	assert.Equal(t, "tmux_session_missing", meta.FailureReason)
}

func TestRecovery_RunningWithSession(t *testing.T) {
	t.Parallel()
	// Test: Recovery scan: running + tmux session alive → no change
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-rec-alive"
	invocationID := "20260205120023-rec4"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create running headed invocation
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMetaWithAge(t, st, repoID, invocationID, store.InvocationStatusRunning, sessionName, -60*time.Second)

	// Add tmux session to fake
	fakeTmux.Sessions[sessionName] = testutil.FakeTmuxSession{Name: sessionName}

	// Start server (recovery runs)
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Should still be running (session is alive)
	waitForStableStatus(t, st, repoID, invocationID, store.InvocationStatusRunning, 500*time.Millisecond)
}

func TestRecovery_TmuxError(t *testing.T) {
	t.Parallel()
	// Test: Recovery scan: tmux transient error → no finalization
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-rec-err"
	invocationID := "20260205120024-rec5"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create running headed invocation
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMetaWithAge(t, st, repoID, invocationID, store.InvocationStatusRunning, sessionName, -60*time.Second)

	// Set transient error for HasSession
	fakeTmux.HasSessionErr = errors.New("connection refused")

	// Start server (recovery runs)
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Should still be running (transient error, no finalization)
	waitForStableStatus(t, st, repoID, invocationID, store.InvocationStatusRunning, 500*time.Millisecond)
}

// ---------------------------------------------------------------------------
// Multi-invocation and edge case tests
// ---------------------------------------------------------------------------

func TestReconcile_MultipleInvocationsSameRepo(t *testing.T) {
	t.Parallel()
	// Test: Multiple invocations in same repo with different statuses
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-multi"
	ensureRepoDir(t, st, repoID)

	// inv-1: running headed, tmux session present → should stay running
	inv1 := "20260205120030-mul1"
	session1 := tmux.SessionName(inv1)
	createTestHeadedInvocationMeta(t, st, repoID, inv1, store.InvocationStatusRunning, session1)
	fakeTmux.Sessions[session1] = testutil.FakeTmuxSession{Name: session1}

	// inv-2: running headed, tmux session absent → should become finished
	inv2 := "20260205120031-mul2"
	session2 := tmux.SessionName(inv2)
	createTestHeadedInvocationMeta(t, st, repoID, inv2, store.InvocationStatusRunning, session2)
	// No session added for inv2

	// inv-3: finished headed → should remain finished (idempotent)
	inv3 := "20260205120032-mul3"
	session3 := tmux.SessionName(inv3)
	createTestHeadedInvocationMeta(t, st, repoID, inv3, store.InvocationStatusFinished, session3)
	err := st.UpdateInvocationMeta(repoID, inv3, func(m *store.InvocationMeta) {
		m.FinishedAt = "2026-02-05T11:50:00Z"
		m.ExitReason = "exited"
	})
	require.NoError(t, err)

	// inv-4: headless running with alive PID → should be unaffected
	inv4 := "20260205120033-mul4"
	_, err = st.EnsureInvocationDir(repoID, inv4)
	require.NoError(t, err)
	inv4PID := 55555
	err = st.WriteInvocationMeta(repoID, inv4, &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          inv4,
		IntegrationWorktreeID: "test-worktree",
		SandboxPath:           "/tmp/sandbox",
		SandboxBranch:         "agency/sandbox-" + inv4,
		BaseCommit:            "abc123",
		Runner:                "claude",
		Mode:                  store.RunnerModeHeadless,
		StartedAt:             "2026-02-05T11:59:50Z",
		Status:                store.InvocationStatusRunning,
		PID:                   &inv4PID,
	})
	require.NoError(t, err)

	// Override PIDChecker: inv4's PID (55555) is alive
	srv.PIDChecker = func(pid int) bool { return pid == inv4PID }

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// inv-2 should transition to finished
	meta2 := waitForStatus(t, st, repoID, inv2, store.InvocationStatusFinished)
	assert.Equal(t, "exited", meta2.ExitReason)

	// inv-1: still running
	meta1, err := st.ReadInvocationMeta(repoID, inv1)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusRunning, meta1.Status, "inv-1 should still be running")

	// inv-3: still finished, unchanged
	meta3, err := st.ReadInvocationMeta(repoID, inv3)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFinished, meta3.Status, "inv-3 should remain finished")
	assert.Equal(t, "2026-02-05T11:50:00Z", meta3.FinishedAt, "inv-3 finished_at should be unchanged")

	// inv-4: still running (headless with alive PID)
	meta4, err := st.ReadInvocationMeta(repoID, inv4)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusRunning, meta4.Status, "inv-4 headless with alive PID should still be running")
}

func TestReconcile_EmptyTmuxSessionFallback(t *testing.T) {
	t.Parallel()
	// Test: Empty TmuxSession field falls back to tmux.SessionName(invocationID)
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-fallback"
	invocationID := "20260205120040-fall"

	// Setup: Create running headed invocation with TmuxSession = "" (empty)
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusRunning, "") // empty TmuxSession

	// Add session under the fallback name
	fallbackName := tmux.SessionName(invocationID)
	fakeTmux.Sessions[fallbackName] = testutil.FakeTmuxSession{Name: fallbackName}

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Verify still running (fallback name correctly used to find session)
	waitForStableStatus(t, st, repoID, invocationID, store.InvocationStatusRunning, 500*time.Millisecond)

	// Remove the fallback-named session
	fakeTmux.Mu.Lock()
	delete(fakeTmux.Sessions, fallbackName)
	fakeTmux.Mu.Unlock()

	// Should transition to finished
	meta := waitForStatus(t, st, repoID, invocationID, store.InvocationStatusFinished)
	assert.Equal(t, "exited", meta.ExitReason)
	assert.Equal(t, "2026-02-05T12:00:00Z", meta.FinishedAt)
	assert.Empty(t, meta.LifecycleOwner)
}
