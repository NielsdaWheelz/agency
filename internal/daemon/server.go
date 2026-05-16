package daemon

import (
	"context"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// Server is the daemon HTTP server that supervises invocations (headless and headed).
type Server struct {
	// Store provides access to agency data.
	Store *store.Store

	// Runner is the interface for spawning processes (injectable for testing).
	Runner exec.CommandRunner

	// FS is the filesystem interface (injectable for testing).
	FS fs.FS

	// TmuxClient is the tmux client interface (injectable for testing).
	TmuxClient tmux.Client

	// Clock returns the current time (injectable for testing).
	Clock func() time.Time

	// InvocationEvents appends invocation-scoped events with shared sequencing.
	InvocationEvents *eventlog.Writer

	// WorktreeEvents appends worktree-scoped events with shared sequencing.
	WorktreeEvents *eventlog.Writer

	// TaskEvents appends task-scoped events with shared sequencing.
	TaskEvents *eventlog.Writer

	// RepoEvents appends repo-scoped events with shared sequencing.
	RepoEvents eventlog.Appender

	// PIDChecker checks if a PID is alive (injectable for testing).
	PIDChecker func(int) bool

	// CheckpointDebounceOverride, if set, overrides the default checkpoint debounce interval.
	// Used in tests to avoid long waits.
	CheckpointDebounceOverride *time.Duration

	// HeadedReconcileIntervalOverride, if set, overrides the default headed reconcile interval.
	// Used in tests to speed up reconciliation.
	HeadedReconcileIntervalOverride *time.Duration

	// ConfigDir is the path to the config directory.
	ConfigDir string

	// InstanceID is the unique ID for this daemon instance, generated at startup.
	InstanceID string

	// startedAt is when the daemon started.
	startedAt time.Time

	// mu protects the processes and activeMerges maps.
	mu sync.RWMutex

	// processes maps invocation_id -> supervised process state (headless and headed).
	processes map[string]*SupervisedProcess

	// activeMerges maps "<repo_id>/<worktree_id>" -> accepted worktree merge attempt.
	activeMerges map[string]*WorktreeMergeProcess

	// idempotencyMu protects the idempotency map.
	idempotencyMu sync.RWMutex

	// idempotency maps (repo_id, client_request_id) -> IdempotencyEntry.
	// Used to prevent duplicate headless invocations from retried requests.
	idempotency map[string]IdempotencyEntry

	// headedIdempotencyMu protects the headed idempotency map.
	headedIdempotencyMu sync.RWMutex

	// headedIdempotency maps (repo_id, client_request_id) -> HeadedIdempotencyEntry.
	// Used to prevent duplicate headed invocations from retried requests.
	headedIdempotency map[string]HeadedIdempotencyEntry

	// headedHookMu serializes headed hook imports so transcript offsets and parser
	// state advance in the same order as writes to raw.jsonl and stream.jsonl.
	headedHookMu sync.Mutex

	// worktreeIdempotencyMu protects the worktree idempotency map.
	worktreeIdempotencyMu sync.RWMutex

	// worktreeIdempotency maps (repo_id, idempotency_key) -> WorktreeIdempotencyEntry.
	// Used to prevent duplicate worktree creation from retried requests.
	worktreeIdempotency map[string]WorktreeIdempotencyEntry

	// headedStartingFirstSeen tracks when a "starting" headed invocation was first
	// observed without a tmux session. Used for grace window before marking failed.
	// Maps invocation_id -> count of ticks observed without tmux session.
	headedStartingTickCount   map[string]int
	headedStartingTickCountMu sync.Mutex

	// repoLock is the repo lock instance for serializing git operations.
	repoLock *lock.RepoLock

	// server is the HTTP server.
	server *http.Server

	// shutdownCh signals graceful shutdown.
	shutdownCh chan struct{}

	// shutdownOnce ensures shutdown is only called once.
	shutdownOnce sync.Once

	// reconcileLoopStarted reports whether Serve started the reconcile loop.
	// Shutdown only waits on reconcileLoopDone when this is true, so a server
	// shut down without ever being served does not block on a loop that was
	// never started. Atomic because Serve and Shutdown can run concurrently.
	reconcileLoopStarted atomic.Bool

	// reconcileLoopDone is closed when the reconciliation loop exits.
	// Shutdown waits for the reconcile loop to exit before mutating state.
	reconcileLoopDone chan struct{}

	// supervisionWg tracks every long-lived per-invocation supervision
	// goroutine (exit-waiter, output-flush loop, checkpoint loop and its
	// stopper). Shutdown signals all processes to stop, then Wait()s on this
	// so no supervision goroutine is still writing into the data dir once
	// Shutdown returns. It also covers invocations whose runner already
	// exited and were removed from the processes map but whose checkpoint
	// loop is still performing its final write.
	supervisionWg sync.WaitGroup
}

// NewServer creates a new daemon server with the given dependencies.
func NewServer(st *store.Store, runner exec.CommandRunner, fsys fs.FS, configDir string) *Server {
	repoLock := lock.NewRepoLock(st.DataDir)
	server := &Server{
		Store:                   st,
		Runner:                  runner,
		FS:                      fsys,
		TmuxClient:              tmux.NewExecClient(runner),
		ConfigDir:               configDir,
		Clock:                   time.Now,
		PIDChecker:              IsPIDAlive,
		InstanceID:              uuid.New().String(),
		processes:               make(map[string]*SupervisedProcess),
		activeMerges:            make(map[string]*WorktreeMergeProcess),
		idempotency:             make(map[string]IdempotencyEntry),
		headedIdempotency:       make(map[string]HeadedIdempotencyEntry),
		worktreeIdempotency:     make(map[string]WorktreeIdempotencyEntry),
		headedStartingTickCount: make(map[string]int),
		repoLock:                &repoLock,
		shutdownCh:              make(chan struct{}),
		reconcileLoopDone:       make(chan struct{}),
	}
	server.InvocationEvents = eventlog.NewWriter("invocation_id", func() time.Time {
		return server.Clock()
	})
	server.WorktreeEvents = eventlog.NewWriter("worktree_id", func() time.Time {
		return server.Clock()
	})
	server.TaskEvents = eventlog.NewWriter("task_id", func() time.Time {
		return server.Clock()
	})
	server.RepoEvents = eventlog.NewWriter("repo_id", func() time.Time {
		return server.Clock()
	})
	return server
}

// IsPIDAlive checks if a process with the given PID is alive.
func IsPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// Send signal 0 to check if process exists
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// Serve starts the HTTP server on the given listener.
// This blocks until the server shuts down.
func (s *Server) Serve(listener net.Listener) error {
	s.startedAt = s.Clock()

	s.server = &http.Server{
		Handler: s.newHTTPHandler(),
	}

	// Run recovery scan before serving
	if err := s.runRecoveryScan(); err != nil {
		return err
	}

	s.reconcileLoopStarted.Store(true)
	go s.runHeadedReconcileLoop()

	return s.server.Serve(listener)
}

// Shutdown gracefully shuts down the server.
//
// Shutdown is synchronous: it terminates every supervised invocation, reaps
// the runner child processes, and waits for all supervision goroutines
// (exit-waiter, output-flush loop, checkpoint loop) to return before it
// returns. No daemon-owned goroutine keeps writing into the data dir once
// Shutdown has returned.
func (s *Server) Shutdown(ctx context.Context) error {
	var err error
	s.shutdownOnce.Do(func() {
		close(s.shutdownCh)

		// Wait for reconciliation loop to exit before mutating state so
		// reconcile and shutdown handlers do not race on meta writes, and
		// so it cannot register a new supervised process after we drain.
		// Skip when Serve never started the loop (handler-only tests).
		if s.reconcileLoopStarted.Load() {
			select {
			case <-s.reconcileLoopDone:
				// Reconciliation loop has exited
			case <-ctx.Done():
				// Shutdown context timed out - proceed anyway
			}
		}

		// Stop the HTTP server first so no in-flight request can register a
		// new supervised invocation while drainSupervisedProcesses waits on
		// the supervision WaitGroup. http.Server.Shutdown blocks until all
		// active handlers return.
		if s.server != nil {
			err = s.server.Shutdown(ctx)
		}

		// Terminate every supervised invocation and wait for its goroutines
		// and child process to fully drain.
		s.drainSupervisedProcesses(ctx)
		s.cancelActiveWorktreeMerges(ctx)
	})
	return err
}

// drainSupervisedProcesses terminates every supervised invocation and blocks
// until all per-invocation supervision goroutines and runner child processes
// have fully stopped. After it returns, no supervision goroutine is still
// writing files.
func (s *Server) drainSupervisedProcesses(ctx context.Context) {
	s.mu.RLock()
	procs := make([]*SupervisedProcess, 0, len(s.processes))
	for _, proc := range s.processes {
		procs = append(procs, proc)
	}
	s.mu.RUnlock()

	// Signal every still-tracked invocation to stop: kill the runner and
	// close its done channel so the supervision goroutines unwind.
	for _, proc := range procs {
		s.terminateSupervisedProcess(ctx, proc)
	}

	// Wait for every supervision goroutine to return. supervisionWg is
	// server-scoped, so this also covers invocations whose runner already
	// exited and were removed from the processes map but whose checkpoint
	// loop is still performing its final checkpoint write. Those goroutines
	// are already unwinding (their done channel is closed), so no further
	// signalling is needed here.
	s.supervisionWg.Wait()

	// Persist the final output offsets and drop the drained processes.
	for _, proc := range procs {
		s.flushLastOutputAt(proc)
		s.clearInvocationProcess(proc.InvocationID)
	}
}

// terminateSupervisedProcess stops a single supervised invocation. For
// headless invocations it kills the runner process group (escalating
// SIGINT -> SIGKILL); the headless exit-waiter then writes the terminal
// "killed" meta on its own. For headed invocations it kills the tmux
// session and writes the terminal meta directly, since headed invocations
// have no exit-waiter goroutine. It then closes the process done channel so
// the output-flush and checkpoint loops unwind even if the runner is
// wedged. It does not wait for goroutines here; drainSupervisedProcesses
// does that after every process is signalled.
func (s *Server) terminateSupervisedProcess(ctx context.Context, proc *SupervisedProcess) {
	proc.exitReason.Store(store.ExitReasonKilled)
	proc.failureReason.Store("killed")

	if proc.Mode == "headed" {
		if proc.TmuxSession != "" {
			_ = s.TmuxClient.KillSession(ctx, proc.TmuxSession)
		}
		s.failInvocationKilled(proc.RepoID, proc.InvocationID)
		proc.CloseDone()
		return
	}

	if proc.PGID > 0 {
		if err := syscall.Kill(-proc.PGID, syscall.SIGINT); err != syscall.ESRCH {
			select {
			case <-proc.done:
			case <-time.After(2 * time.Second):
				_ = syscall.Kill(-proc.PGID, syscall.SIGKILL)
			case <-ctx.Done():
				_ = syscall.Kill(-proc.PGID, syscall.SIGKILL)
			}
		}
	}
	// Close done unconditionally so the checkpoint and output-flush loops
	// terminate even when the runner exited on its own (waitForExit closes
	// done too, but CloseDone is idempotent).
	proc.CloseDone()
}
