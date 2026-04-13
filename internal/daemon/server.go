package daemon

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/daemon/worktreeevents"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// Server is the daemon HTTP server that supervises invocations (headless and headed).
type Server struct {
	// Store provides access to agency data.
	Store *store.Store

	// Runner is the interface for spawning processes (injectable for testing).
	Runner exec.CommandRunner

	// FS is the filesystem interface (injectable for testing).
	FS fs.FS

	// TmuxClient is the tmux client interface (injectable for testing). PR-10.
	TmuxClient tmux.Client

	// Clock returns the current time (injectable for testing).
	Clock func() time.Time

	// InvocationEvents appends invocation-scoped events with shared sequencing.
	InvocationEvents *invocationevents.Writer

	// WorktreeEvents appends worktree-scoped events with shared sequencing.
	WorktreeEvents *worktreeevents.Writer

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

	// mu protects the processes map.
	mu sync.RWMutex

	// processes maps invocation_id -> supervised process state (headless and headed).
	processes map[string]*SupervisedProcess

	// idempotencyMu protects the idempotency map.
	idempotencyMu sync.RWMutex

	// idempotency maps (repo_id, client_request_id) -> IdempotencyEntry.
	// Used to prevent duplicate headless invocations from retried requests.
	idempotency map[string]IdempotencyEntry

	// headedIdempotencyMu protects the headed idempotency map. PR-10.
	headedIdempotencyMu sync.RWMutex

	// headedIdempotency maps (repo_id, client_request_id) -> HeadedIdempotencyEntry. PR-10.
	// Used to prevent duplicate headed invocations from retried requests.
	headedIdempotency map[string]HeadedIdempotencyEntry

	// worktreeIdempotencyMu protects the worktree idempotency map.
	worktreeIdempotencyMu sync.RWMutex

	// worktreeIdempotency maps (repo_id, idempotency_key) -> WorktreeIdempotencyEntry.
	// Used to prevent duplicate worktree creation from retried requests.
	worktreeIdempotency map[string]WorktreeIdempotencyEntry

	// headedStartingFirstSeen tracks when a "starting" headed invocation was first
	// observed without a tmux session. Used for grace window before marking failed.
	// PR-11: Maps invocation_id -> count of ticks observed without tmux session.
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
	// PR-11: Used to ensure shutdown handlers wait for reconcile loop to exit.
	reconcileLoopDone chan struct{}
}

// NewServer creates a new daemon server with the given dependencies.
func NewServer(st *store.Store, runner exec.CommandRunner, fsys fs.FS, configDir string) *Server {
	repoLock := lock.NewRepoLock(st.DataDir)
	server := &Server{
		Store:                   st,
		Runner:                  runner,
		FS:                      fsys,
		TmuxClient:              tmux.NewExecClient(runner), // PR-10: create real tmux client
		ConfigDir:               configDir,
		Clock:                   time.Now,
		PIDChecker:              IsPIDAlive,
		InstanceID:              uuid.New().String(),
		processes:               make(map[string]*SupervisedProcess),
		idempotency:             make(map[string]IdempotencyEntry),
		headedIdempotency:       make(map[string]HeadedIdempotencyEntry), // PR-10
		worktreeIdempotency:     make(map[string]WorktreeIdempotencyEntry),
		headedStartingTickCount: make(map[string]int), // PR-11
		repoLock:                &repoLock,
		shutdownCh:              make(chan struct{}),
		reconcileLoopDone:       make(chan struct{}), // PR-11
	}
	server.InvocationEvents = invocationevents.NewWriter(func() time.Time {
		return server.Clock()
	})
	server.WorktreeEvents = worktreeevents.NewWriter(func() time.Time {
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

	// PR-11: Start headed reconciliation loop
	go s.runHeadedReconcileLoop()

	return s.server.Serve(listener)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	var err error
	s.shutdownOnce.Do(func() {
		// PR-11: Close shutdownCh to signal all loops to stop
		close(s.shutdownCh)

		// PR-11: Wait for reconciliation loop to exit before mutating state.
		// This ensures reconcile and shutdown handlers don't race on meta writes.
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

		if s.server != nil {
			err = s.server.Shutdown(ctx)
		}
	})
	return err
}

// registerRoutes sets up the HTTP routes.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/shutdown", s.handleShutdown)
	mux.HandleFunc("/invocations/", s.handleInvocations)
	mux.HandleFunc("/worktrees/", s.handleWorktrees)
	mux.HandleFunc("/worktrees", s.handleWorktrees)             // Without trailing slash for create
	mux.HandleFunc("/repos/", s.handleRepos)                    // PR-A: repo registry
	mux.HandleFunc("/repos", s.handleRepos)                     // PR-A: repo registry (no trailing slash)
	mux.HandleFunc("/spec/v2.1/s1/release/", s.handleS1Release) // PR-05: S1 release gates
}

func (s *Server) newHTTPHandler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return s.withRequestID(mux)
}

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := resolveOrGenerateRequestID(r.Header.Get("X-Request-ID"))
		setRequestIDHeader(w, requestID)
		next.ServeHTTP(w, r.WithContext(withRequestIDContext(r.Context(), requestID)))
	})
}

// handleHealth handles GET /health.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
		return
	}

	uptime := int64(s.Clock().Sub(s.startedAt).Seconds())

	resp := HealthResponse{
		OK:               true,
		APIVersion:       APIVersion,
		BuildVersion:     version.FullVersion(),
		GitSHA:           version.Commit,
		PID:              os.Getpid(),
		DaemonInstanceID: s.InstanceID,
		UptimeSeconds:    uptime,
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleShutdown handles POST /shutdown.
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
		return
	}

	force := r.URL.Query().Get("force") == "true"

	// Check for active headless invocations
	s.mu.RLock()
	runningIDs := make([]string, 0)
	for id := range s.processes {
		runningIDs = append(runningIDs, id)
	}
	s.mu.RUnlock()

	if len(runningIDs) > 0 && !force {
		resp := ShutdownResponse{
			OK:                 false,
			ErrorCode:          string(errors.EDaemonBusy),
			Message:            fmt.Sprintf("%d active headless invocations; use --force to override", len(runningIDs)),
			Hint:               "use 'agency daemon stop --force' to terminate all invocations and stop the daemon",
			RunningInvocations: runningIDs,
		}
		s.writeJSON(w, http.StatusConflict, resp)
		return
	}

	if force && len(runningIDs) > 0 {
		// Terminate all active invocations
		s.terminateAllInvocations()
	}

	// Respond before shutting down
	resp := ShutdownResponse{OK: true}
	s.writeJSON(w, http.StatusOK, resp)

	// Trigger shutdown in a goroutine to allow the response to be sent
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	}()
}

// terminateAllInvocations terminates all active invocations (headless and headed).
func (s *Server) terminateAllInvocations() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, proc := range s.processes {
		// Set exit reason before sending signals so waitForExit* uses
		// the correct reason if the process exits from SIGINT.
		proc.exitReason.Store("killed")
		proc.failureReason.Store("killed")

		if proc.Mode == "headed" {
			// Headed path: kill tmux session, close done, write terminal meta
			if proc.TmuxSession != "" {
				_ = s.TmuxClient.KillSession(ctx, proc.TmuxSession)
			}
			proc.CloseDone()

			now := s.Clock().UTC().Format(time.RFC3339)
			_ = s.Store.UpdateInvocationMeta(proc.RepoID, id, func(meta *store.InvocationMeta) {
				meta.Status = store.InvocationStatusFailed
				meta.ExitReason = "killed"
				meta.FailureReason = "killed"
				meta.FinishedAt = now
				meta.LifecycleOwner = ""
			})

			delete(s.processes, id)
			continue
		}

		// Headless path: SIGINT → 5s → SIGKILL escalation
		if proc.PGID <= 0 {
			// Safety guard: skip signaling if PGID is invalid
			delete(s.processes, id)
			continue
		}

		// Send SIGINT to process group
		_ = syscall.Kill(-proc.PGID, syscall.SIGINT)

		// Wait up to 5 seconds for graceful exit
		select {
		case <-proc.done:
			// Process exited gracefully — waitForExit* will write terminal meta
			// using the exitReason we set above.
			continue
		case <-time.After(5 * time.Second):
			// Force kill
			_ = syscall.Kill(-proc.PGID, syscall.SIGKILL)
		}

		// Timed out waiting — write meta directly since waitForExit* is
		// blocked on s.mu.Lock() and we hold the lock.
		now := s.Clock().UTC().Format(time.RFC3339)
		_ = s.Store.UpdateInvocationMeta(proc.RepoID, id, func(meta *store.InvocationMeta) {
			meta.Status = store.InvocationStatusFailed
			meta.ExitReason = "killed"
			meta.FailureReason = "killed"
			meta.FinishedAt = now
			meta.PID = nil
			meta.LifecycleOwner = ""
		})

		delete(s.processes, id)
	}
}

// handleInvocations handles requests to /invocations/...
func (s *Server) handleInvocations(w http.ResponseWriter, r *http.Request) {
	// Parse path: /invocations/{action_or_id}[/{action}]
	path := r.URL.Path
	if len(path) < len("/invocations/") {
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "not found", "")
		return
	}

	remaining := path[len("/invocations/"):]

	// PR-12: Handle GET /invocations (list invocations)
	if remaining == "" {
		if r.Method == http.MethodGet {
			s.handleListInvocations(w, r)
			return
		}
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
		return
	}

	// Check for control plane endpoint: POST /invocations/start_headless (no ID)
	if remaining == "start_headless" {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleControlPlaneStartHeadless(w, r)
		return
	}

	// PR-10: Check for control plane endpoint: POST /invocations/start_headed (no ID)
	if remaining == "start_headed" {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleControlPlaneStartHeaded(w, r)
		return
	}

	// Find the slash separating id from action
	var invocationRef, action string
	for i, c := range remaining {
		if c == '/' {
			invocationRef = remaining[:i]
			action = remaining[i+1:]
			break
		}
	}
	if invocationRef == "" {
		invocationRef = remaining
	}

	if invocationRef == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invocation ref required", "")
		return
	}

	// Extract the top-level action (first path segment after invocation ref).
	topAction, _, _ := strings.Cut(action, "/")

	switch topAction {
	case "":
		// PR-12: GET /invocations/{ref} - get single invocation
		if r.Method == http.MethodGet {
			s.handleGetInvocation(w, r, invocationRef)
			return
		}
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
	case "stop":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleStop(w, r, invocationRef)
	case "kill":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleKill(w, r, invocationRef)
	case "checkpoints":
		// PR-12: Handle GET /invocations/{ref}/checkpoints (list) and POST .../apply
		s.handleCheckpointsRoute(w, r, invocationRef, action)
	case "restart":
		// S3 PR-03: POST /invocations/{ref}/restart
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleRestartFromCheckpoint(w, r, invocationRef)
	case "land":
		// PR-09: Land sandbox changes to integration worktree
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleLand(w, r, invocationRef)
	case "discard":
		// PR-09: Discard sandbox without landing
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleDiscard(w, r, invocationRef)
	case "diff":
		// PR-12: GET /invocations/{ref}/diff
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleGetInvocationDiff(w, r, invocationRef)
	case "logs":
		// PR-12: GET /invocations/{ref}/logs
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleGetInvocationLogs(w, r, invocationRef)
	case "timeline":
		// S3 PR-01: GET /invocations/{ref}/timeline
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleGetInvocationTimeline(w, r, invocationRef)
	case "review":
		// S5 PR-01: GET /invocations/{ref}/review
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleGetInvocationReview(w, r, invocationRef)
	case "chat":
		// S3 PR-02: POST /invocations/{ref}/chat
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleControlPlaneFollowUpPrompt(w, r, invocationRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown action: "+action, "")
	}
}

// handleCheckpointsRoute routes checkpoint requests (PR-12 addition).
func (s *Server) handleCheckpointsRoute(w http.ResponseWriter, r *http.Request, invocationRef, action string) {
	// Parse sub-action: checkpoints or checkpoints/apply
	subAction := ""
	if idx := strings.Index(action, "/"); idx != -1 {
		subAction = action[idx+1:]
	}

	switch subAction {
	case "":
		// GET /invocations/{ref}/checkpoints - list checkpoints
		if r.Method == http.MethodGet {
			s.handleGetInvocationCheckpoints(w, r, invocationRef)
			return
		}
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
	case "apply":
		// POST /invocations/{ref}/checkpoints/apply - apply checkpoint
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleCheckpoints(w, r, invocationRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown checkpoints action: "+subAction, "")
	}
}

// writeJSON writes a JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func setRequestIDHeader(w http.ResponseWriter, requestID string) {
	normalized := normalizeRequestID(requestID)
	if normalized == "" {
		return
	}
	w.Header().Set("X-Request-ID", normalized)
}

func requestIDForResponse(w http.ResponseWriter) string {
	if w == nil {
		return newRequestID()
	}
	if requestID := normalizeRequestID(w.Header().Get("X-Request-ID")); requestID != "" {
		return requestID
	}
	requestID := newRequestID()
	setRequestIDHeader(w, requestID)
	return requestID
}

// writeError writes an error response.
func (s *Server) writeError(w http.ResponseWriter, status int, code, message, hint string) {
	s.writeErrorWithRequestID(w, status, requestIDForResponse(w), code, message, hint)
}

func (s *Server) writeErrorWithRequestID(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	requestID = resolveOrGenerateRequestID(requestID)
	setRequestIDHeader(w, requestID)
	resp := ErrorResponse{
		OK:        false,
		RequestID: requestID,
		ErrorCode: code,
		Message:   message,
		Hint:      hint,
	}
	s.writeJSON(w, status, resp)
}

// flushLastOutputAt writes the last_output_at to meta.json.
func (s *Server) flushLastOutputAt(proc *SupervisedProcess) {
	lastOutput := time.Unix(0, s.latestOutputAtUnixNano(proc))
	if lastOutput.IsZero() {
		return
	}

	_ = s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
		meta.LastOutputAt = lastOutput.UTC().Format(time.RFC3339)
	})
}

func (s *Server) latestOutputAtUnixNano(proc *SupervisedProcess) int64 {
	if proc == nil {
		return 0
	}
	lastOutput := proc.lastOutputAt.Load()
	if proc.Parser != nil {
		if parserLastOutput := proc.Parser.GetLastOutputAt(); parserLastOutput > lastOutput {
			lastOutput = parserLastOutput
		}
	}
	return lastOutput
}

// LoadUserConfig loads the user config.
func (s *Server) LoadUserConfig() (config.UserConfig, error) {
	cfg, _, err := config.LoadUserConfig(s.FS, s.ConfigDir)
	return cfg, err
}

// streamOutput copies data from reader to writer while updating lastOutputAt.
func (s *Server) streamOutput(proc *SupervisedProcess, reader io.Reader, file *os.File) {
	defer proc.streamWg.Done()
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			_, _ = file.Write(buf[:n])
			proc.lastOutputAt.Store(s.Clock().UnixNano())
		}
		if err != nil {
			break
		}
	}
}

// streamAndParseOutput streams stdout while parsing for normalized events (PR-07).
// Writes verbatim to rawFile, normalized events to streamFile.
func (s *Server) streamAndParseOutput(proc *SupervisedProcess, reader io.Reader, rawFile, streamFile *os.File) {
	defer proc.streamWg.Done()
	if proc.Parser == nil {
		// No parser - fall back to simple streaming
		s.streamOutput(proc, reader, rawFile)
		return
	}

	// Use the parser to handle streaming and parsing
	if err := proc.Parser.StreamAndParse(reader, rawFile, streamFile); err != nil {
		if !stderrors.Is(err, stream.ErrRawLogWriteFailed) && !stderrors.Is(err, stream.ErrNormalizedStreamWriteFailed) {
			// Reader-side errors can occur during normal shutdown races (e.g. pipe close).
			// Only persistence failures should trigger lifecycle failure semantics.
			return
		}

		// Stream persistence is contract-critical; surface failure deterministically
		// and terminate the supervised process to prevent silent data loss.
		proc.exitReason.Store("stream_write_failed")
		proc.failureReason.Store("stream_write_failed")
			_ = s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
				meta.Flags.NeedsAttention = true
				meta.FailureReason = "stream_write_failed"
			})
			s.recordInvocationWarning(proc.RepoID, proc.InvocationID, "stream_write_failed", err.Error(), nil)

			if proc.PGID > 0 {
				_ = syscall.Kill(-proc.PGID, syscall.SIGKILL)
			}
	}
}

// runSemanticStatusFlushLoop periodically flushes semantic status to meta.json (PR-07).
// Only flushes when semantic status has changed, not on every tick.
func (s *Server) runSemanticStatusFlushLoop(proc *SupervisedProcess) {
	if proc.Parser == nil {
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastStatus *runnerstatus.Status
	var lastUpdatedAt time.Time

	for {
		select {
		case <-proc.done:
			// Final flush handled by waitForExit*
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			currentStatus := proc.Parser.GetSemanticStatus()
			currentUpdatedAt := proc.Parser.GetSemanticStatusUpdatedAt()

			// Only flush if status actually changed
			statusChanged := false
			if currentStatus == nil && lastStatus != nil {
				statusChanged = true
			} else if currentStatus != nil && lastStatus == nil {
				statusChanged = true
			} else if currentStatus != nil && lastStatus != nil && *currentStatus != *lastStatus {
				statusChanged = true
			} else if !currentUpdatedAt.IsZero() && currentUpdatedAt.After(lastUpdatedAt) {
				// Same status but newer timestamp - still update
				statusChanged = true
			}

			if statusChanged {
				s.flushSemanticStatus(proc, currentStatus, currentUpdatedAt)
				lastStatus = currentStatus
				lastUpdatedAt = currentUpdatedAt
			}
		}
	}
}

// flushSemanticStatus writes the semantic status to meta.json.
func (s *Server) flushSemanticStatus(proc *SupervisedProcess, status *runnerstatus.Status, updatedAt time.Time) {
	_ = s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
		meta.SemanticStatus = status
		if status != nil && !updatedAt.IsZero() {
			meta.SemanticStatusUpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		} else {
			meta.SemanticStatusUpdatedAt = ""
		}
	})
}

// handleWorktrees handles requests to /worktrees/...
func (s *Server) handleWorktrees(w http.ResponseWriter, r *http.Request) {
	// Parse path: /worktrees[/create] or /worktrees/{ref}[/rm]
	path := r.URL.Path

	// PR-12: Handle GET /worktrees (list worktrees) or /worktrees/
	if path == "/worktrees" || path == "/worktrees/" {
		if r.Method == http.MethodGet {
			s.handleListWorktrees(w, r)
			return
		}
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
		return
	}

	remaining := strings.TrimPrefix(path, "/worktrees/")

	// Handle POST /worktrees/create
	if remaining == "create" {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleWorktreeCreate(w, r)
		return
	}

	// Handle /worktrees/{ref} or /worktrees/{ref}/rm
	// Find the slash separating ref from action
	var worktreeRef, action string
	for i, c := range remaining {
		if c == '/' {
			worktreeRef = remaining[:i]
			action = remaining[i+1:]
			break
		}
	}
	if worktreeRef == "" {
		worktreeRef = remaining
	}

	if worktreeRef == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "worktree ref required", "")
		return
	}

	topAction, _, _ := strings.Cut(action, "/")
	switch topAction {
	case "":
		// PR-12: GET /worktrees/{ref} - get single worktree
		if r.Method == http.MethodGet {
			s.handleGetWorktree(w, r, worktreeRef)
			return
		}
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
	case "rm":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleWorktreeRm(w, r, worktreeRef)
	case "pr":
		s.handleWorktreePRRoute(w, r, worktreeRef, action)
	case "merge":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleWorktreePRMerge(w, r, worktreeRef)
	case "update":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleWorktreeUpdate(w, r, worktreeRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown action: "+action, "supported actions: rm, pr, merge, update")
	}
}

func (s *Server) handleWorktreePRRoute(w http.ResponseWriter, r *http.Request, worktreeRef, action string) {
	subAction := ""
	if idx := strings.Index(action, "/"); idx != -1 {
		subAction = action[idx+1:]
	}

	switch subAction {
	case "sync":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleWorktreePRSync(w, r, worktreeRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown pr action: "+subAction, "")
	}
}
