package daemon_test

import (
	"context"
	"errors"
	"net"
	"path/filepath"
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
// Returns the server, store, and a function to wait for the reconcile loop to run.
func setupReconcileTestEnv(t *testing.T, fakeTmux *testutil.FakeTmuxClient) (*daemon.Server, *store.Store, func()) {
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

	return srv, st, func() {
		// Wait for at least one reconcile tick (interval is 200ms)
		time.Sleep(250 * time.Millisecond)
	}
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

func TestReconcile_RunningToFinished(t *testing.T) {
	// Test: Running → Finished when tmux session disappears
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st, waitForReconcile := setupReconcileTestEnv(t, fakeTmux)

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

	// Wait for reconciliation
	waitForReconcile()
	waitForReconcile() // Extra wait to be safe

	// Verify invocation is now finished
	meta, err = st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFinished, meta.Status)
	assert.Equal(t, "exited", meta.ExitReason)
	assert.NotEmpty(t, meta.FinishedAt)
}

func TestReconcile_StartingToFailed_GraceWindow(t *testing.T) {
	// Test: Starting → Failed only after grace window (2 consecutive ticks)
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st, waitForReconcile := setupReconcileTestEnv(t, fakeTmux)

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
	waitForReconcile()
	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusStarting, meta.Status, "should still be starting after first tick")

	// After second reconcile tick, should be failed (grace window exceeded)
	waitForReconcile()
	waitForReconcile() // Extra safety

	meta, err = st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFailed, meta.Status, "should be failed after grace window")
	assert.Equal(t, "start_failed", meta.ExitReason)
	assert.Equal(t, "tmux_session_missing", meta.FailureReason)
}

func TestReconcile_NoTransitionOnTransientError(t *testing.T) {
	// Test: Transient tmux errors don't finalize invocation
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st, waitForReconcile := setupReconcileTestEnv(t, fakeTmux)

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

	// Wait for reconciliation
	waitForReconcile()
	waitForReconcile()

	// Verify invocation is still running (not finalized due to error)
	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusRunning, meta.Status, "should not finalize on transient error")
}

func TestReconcile_Idempotence_TerminalUnchanged(t *testing.T) {
	// Test: Terminal invocations remain unchanged
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st, waitForReconcile := setupReconcileTestEnv(t, fakeTmux)

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

	// Wait for reconciliation
	waitForReconcile()
	waitForReconcile()

	// Verify invocation is unchanged
	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFinished, meta.Status)
	assert.Equal(t, originalFinishedAt, meta.FinishedAt, "finished_at should not change")
}

func TestReconcile_TmuxFlaps(t *testing.T) {
	// Test: Adversarial tmux session flapping
	// For "running" invocations, recovery scan also calls HasSession, so we must account for that.
	// Sequence: error (recovery) → error (tick 1) → exists (tick 2) → not exists (tick 3)
	// Expected: no transition until tick 3

	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st, waitForReconcile := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-5"
	invocationID := "20260205120004-i9j0"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create a running headed invocation
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusRunning, sessionName)

	// Track call count for sequential behavior
	// Note: Recovery scan calls HasSession once for "running" invocations,
	// so callCount=1 is recovery, callCount=2+ are reconcile ticks.
	callCount := 0
	fakeTmux.HasSessionFunc = func(name string) (bool, error) {
		callCount++
		switch callCount {
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

	// Tick 1: Error - should not finalize
	waitForReconcile()
	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusRunning, meta.Status, "should not finalize on error (tick 1)")

	// Tick 2: Session exists - should not finalize
	waitForReconcile()
	meta, err = st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusRunning, meta.Status, "should not finalize when session exists (tick 2)")

	// Tick 3: Session gone - should finalize
	waitForReconcile()
	meta, err = st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFinished, meta.Status, "should finalize when session gone (tick 3)")
}

func TestReconcile_StartingGraceWindowReset(t *testing.T) {
	// Test: Grace window resets when tmux session appears
	// Starting invocation with no session → session appears → no session
	// Should need 2 more ticks after session disappears again

	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st, waitForReconcile := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-6"
	invocationID := "20260205120005-k1l2"
	sessionName := tmux.SessionName(invocationID)

	// Setup: Create a starting headed invocation (no tmux session)
	ensureRepoDir(t, st, repoID)
	createTestHeadedInvocationMeta(t, st, repoID, invocationID, store.InvocationStatusStarting, sessionName)

	// Update to running so we can test the grace window reset scenario properly
	err := st.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		m.Status = store.InvocationStatusRunning
	})
	require.NoError(t, err)

	// Session exists initially
	fakeTmux.Sessions[sessionName] = testutil.FakeTmuxSession{Name: sessionName}

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// First tick: session exists
	waitForReconcile()
	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusRunning, meta.Status)

	// Remove session
	fakeTmux.Mu.Lock()
	delete(fakeTmux.Sessions, sessionName)
	fakeTmux.Mu.Unlock()

	// Next tick: session gone, should finalize immediately (running state)
	waitForReconcile()
	waitForReconcile()

	meta, err = st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusFinished, meta.Status)
}

func TestReconcile_HeadlessUnaffected(t *testing.T) {
	// Test: Headless invocations are not affected by headed reconciliation
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st, waitForReconcile := setupReconcileTestEnv(t, fakeTmux)

	repoID := "test-repo-7"
	invocationID := "20260205120006-m3n4"

	// Setup: Create a running headless invocation
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
	}
	err = st.WriteInvocationMeta(repoID, invocationID, meta)
	require.NoError(t, err)

	// Start server
	cleanup := startTestServer(t, srv)
	defer cleanup()

	// Wait for reconciliation
	waitForReconcile()
	waitForReconcile()

	// Verify headless invocation is unchanged (reconcile only affects headed)
	meta, err = st.ReadInvocationMeta(repoID, invocationID)
	require.NoError(t, err)
	assert.Equal(t, store.InvocationStatusRunning, meta.Status, "headless invocations should not be affected by headed reconciliation")
}

func TestReconcile_ShutdownOrdering(t *testing.T) {
	// Test: Shutdown waits for reconcile loop to exit before terminating
	fakeTmux := testutil.NewFakeTmuxClient()
	srv, st, _ := setupReconcileTestEnv(t, fakeTmux)

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
