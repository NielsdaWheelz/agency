package daemon

import (
	"context"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/daemon/repoevents"
	"github.com/NielsdaWheelz/agency/internal/daemon/taskevents"
	"github.com/NielsdaWheelz/agency/internal/daemon/worktreeevents"
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
	InvocationEvents *invocationevents.Writer

	// WorktreeEvents appends worktree-scoped events with shared sequencing.
	WorktreeEvents *worktreeevents.Writer

	// TaskEvents appends task-scoped events with shared sequencing.
	TaskEvents *taskevents.Writer

	// RepoEvents appends repo-scoped events with shared sequencing.
	RepoEvents repoevents.Appender

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

	// reconcileLoopDone is closed when the reconciliation loop exits.
	// Shutdown waits for the reconcile loop to exit before mutating state.
	reconcileLoopDone chan struct{}
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
	server.InvocationEvents = invocationevents.NewWriter(func() time.Time {
		return server.Clock()
	})
	server.WorktreeEvents = worktreeevents.NewWriter(func() time.Time {
		return server.Clock()
	})
	server.TaskEvents = taskevents.NewWriter(func() time.Time {
		return server.Clock()
	})
	server.RepoEvents = repoevents.NewWriter(func() time.Time {
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

	go s.runHeadedReconcileLoop()

	return s.server.Serve(listener)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	var err error
	s.shutdownOnce.Do(func() {
		close(s.shutdownCh)

		// Wait for reconciliation loop to exit before mutating state so
		// reconcile and shutdown handlers do not race on meta writes.
		select {
		case <-s.reconcileLoopDone:
			// Reconciliation loop has exited
		case <-ctx.Done():
			// Shutdown context timed out - proceed anyway
		}

		// Flush all pending meta writes
		s.mu.RLock()
		for _, proc := range s.processes {
			s.flushLastOutputAt(proc)
		}
		s.mu.RUnlock()
		s.cancelActiveWorktreeMerges(ctx)

		if s.server != nil {
			err = s.server.Shutdown(ctx)
		}
	})
	return err
}
