package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// Server is the daemon HTTP server that supervises headless invocations.
type Server struct {
	// Store provides access to agency data.
	Store *store.Store

	// Runner is the interface for spawning processes (injectable for testing).
	Runner exec.CommandRunner

	// FS is the filesystem interface (injectable for testing).
	FS fs.FS

	// Clock returns the current time (injectable for testing).
	Clock func() time.Time

	// PIDChecker checks if a PID is alive (injectable for testing).
	PIDChecker func(int) bool

	// ConfigDir is the path to the config directory.
	ConfigDir string

	// InstanceID is the unique ID for this daemon instance, generated at startup.
	InstanceID string

	// startedAt is when the daemon started.
	startedAt time.Time

	// mu protects the processes map.
	mu sync.RWMutex

	// processes maps invocation_id -> supervised process state.
	processes map[string]*SupervisedProcess

	// idempotencyMu protects the idempotency map.
	idempotencyMu sync.RWMutex

	// idempotency maps (repo_id, client_request_id) -> IdempotencyEntry.
	// Used to prevent duplicate invocations from retried requests.
	idempotency map[string]IdempotencyEntry

	// worktreeIdempotencyMu protects the worktree idempotency map.
	worktreeIdempotencyMu sync.RWMutex

	// worktreeIdempotency maps (repo_id, idempotency_key) -> WorktreeIdempotencyEntry.
	// Used to prevent duplicate worktree creation from retried requests.
	worktreeIdempotency map[string]WorktreeIdempotencyEntry

	// repoLock is the repo lock instance for serializing git operations.
	repoLock *lock.RepoLock

	// server is the HTTP server.
	server *http.Server

	// shutdownCh signals graceful shutdown.
	shutdownCh chan struct{}

	// shutdownOnce ensures shutdown is only called once.
	shutdownOnce sync.Once
}

// NewServer creates a new daemon server with the given dependencies.
func NewServer(st *store.Store, runner exec.CommandRunner, fsys fs.FS, configDir string) *Server {
	repoLock := lock.NewRepoLock(st.DataDir)
	return &Server{
		Store:               st,
		Runner:              runner,
		FS:                  fsys,
		ConfigDir:           configDir,
		Clock:               time.Now,
		PIDChecker:          IsPIDAlive,
		InstanceID:          uuid.New().String(),
		processes:           make(map[string]*SupervisedProcess),
		idempotency:         make(map[string]IdempotencyEntry),
		worktreeIdempotency: make(map[string]WorktreeIdempotencyEntry),
		repoLock:            &repoLock,
		shutdownCh:          make(chan struct{}),
	}
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

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.server = &http.Server{
		Handler: mux,
	}

	// Run recovery scan before serving
	if err := s.runRecoveryScan(); err != nil {
		// Log but don't fail startup
		fmt.Fprintf(os.Stderr, "warning: recovery scan failed: %v\n", err)
	}

	return s.server.Serve(listener)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	var err error
	s.shutdownOnce.Do(func() {
		close(s.shutdownCh)

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
	mux.HandleFunc("/worktrees", s.handleWorktrees) // Without trailing slash for create
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

// terminateAllInvocations terminates all active headless invocations.
func (s *Server) terminateAllInvocations() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, proc := range s.processes {
		// Send SIGINT to process group
		_ = syscall.Kill(-proc.PGID, syscall.SIGINT)

		// Wait up to 5 seconds for graceful exit
		select {
		case <-proc.done:
			// Process exited gracefully
		case <-time.After(5 * time.Second):
			// Force kill
			_ = syscall.Kill(-proc.PGID, syscall.SIGKILL)
		}

		// Mark as failed/killed
		now := s.Clock().UTC().Format(time.RFC3339)
		_ = s.Store.UpdateInvocationMeta(proc.RepoID, id, func(meta *store.InvocationMeta) {
			meta.Status = store.InvocationStatusFailed
			meta.ExitReason = "killed"
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
	if remaining == "" {
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "endpoint required", "")
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

	// Find the slash separating id from action
	var invocationID, action string
	for i, c := range remaining {
		if c == '/' {
			invocationID = remaining[:i]
			action = remaining[i+1:]
			break
		}
	}
	if invocationID == "" {
		invocationID = remaining
	}

	if invocationID == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invocation id required", "")
		return
	}

	switch action {
	case "start_headless":
		// Legacy PR-04 endpoint: POST /invocations/{id}/start_headless
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleStartHeadless(w, r, invocationID)
	case "stop":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleStop(w, r, invocationID)
	case "kill":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleKill(w, r, invocationID)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown action: "+action, "")
	}
}

// writeJSON writes a JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes an error response.
func (s *Server) writeError(w http.ResponseWriter, status int, code, message, hint string) {
	resp := ErrorResponse{
		OK:        false,
		ErrorCode: code,
		Message:   message,
		Hint:      hint,
	}
	s.writeJSON(w, status, resp)
}

// flushLastOutputAt writes the last_output_at to meta.json.
func (s *Server) flushLastOutputAt(proc *SupervisedProcess) {
	lastOutput := time.Unix(0, proc.lastOutputAt)
	if lastOutput.IsZero() {
		return
	}

	_ = s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
		meta.LastOutputAt = lastOutput.UTC().Format(time.RFC3339)
	})
}

// runRecoveryScan checks for orphaned invocations on daemon startup.
func (s *Server) runRecoveryScan() error {
	// Scan all repos for running headless invocations
	repoIndexPath := s.Store.RepoIndexPath()
	data, err := s.FS.ReadFile(repoIndexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No repos yet
		}
		return err
	}

	var index store.RepoIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return err
	}

	for repoID := range index.Repos {
		if err := s.recoverRepoInvocations(repoID); err != nil {
			// Log but continue
			fmt.Fprintf(os.Stderr, "warning: recovery scan for repo %s failed: %v\n", repoID, err)
		}
	}

	return nil
}

// recoverRepoInvocations checks invocations for a single repo (PR-05 enhanced).
func (s *Server) recoverRepoInvocations(repoID string) error {
	records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return err
	}

	now := s.Clock().UTC().Format(time.RFC3339)
	nowTime := s.Clock()

	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}

		// Only process headless invocations
		if r.Meta.Mode != store.RunnerModeHeadless {
			continue
		}

		// PR-05: Handle status=starting invocations that are too old (>60s)
		if r.Meta.Status == store.InvocationStatusStarting {
			startedAt, err := time.Parse(time.RFC3339, r.Meta.StartedAt)
			if err == nil {
				age := nowTime.Sub(startedAt)
				if age > 60*time.Second && r.Meta.PID == nil {
					_ = s.Store.UpdateInvocationMeta(repoID, r.InvocationID, func(meta *store.InvocationMeta) {
						meta.Status = store.InvocationStatusFailed
						meta.ExitReason = "start_failed"
						meta.FailureReason = "start_incomplete" // PR-05: set failure_reason
						meta.FinishedAt = now
						meta.Flags.NeedsAttention = true
						meta.LifecycleOwner = ""
					})
				}
			}
			continue
		}

		// Only check running invocations
		if r.Meta.Status != store.InvocationStatusRunning {
			continue
		}

		// Skip if no PID recorded
		if r.Meta.PID == nil {
			continue
		}

		pid := *r.Meta.PID
		alive := s.PIDChecker(pid)

		if !alive {
			// Case A: PID is NOT alive - mark as failed with failure_reason
			_ = s.Store.UpdateInvocationMeta(repoID, r.InvocationID, func(meta *store.InvocationMeta) {
				meta.Status = store.InvocationStatusFailed
				meta.ExitReason = "unknown"
				meta.FailureReason = "runner_exit_nonzero" // PR-05: set failure_reason (best guess)
				meta.FinishedAt = now
				meta.PID = nil
				meta.Flags.NeedsAttention = true
				meta.Flags.Orphaned = true
				meta.LifecycleOwner = ""
			})
		} else {
			// Case B: PID IS alive but daemon_instance_id doesn't match
			if r.Meta.DaemonInstanceID != s.InstanceID {
				_ = s.Store.UpdateInvocationMeta(repoID, r.InvocationID, func(meta *store.InvocationMeta) {
					meta.Flags.NeedsAttention = true
					meta.Flags.Orphaned = true
					meta.OrphanedAt = now
				})
			}
		}
	}

	return nil
}

// WritePidFile writes the daemon's PID to the pid file.
func WritePidFile(pidPath string) error {
	dir := filepath.Dir(pidPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

// ReadPidFile reads the PID from the pid file.
func ReadPidFile(pidPath string) (int, error) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(string(data[:len(data)-1])) // strip newline
	if err != nil {
		return 0, err
	}
	return pid, nil
}

// RemovePidFile removes the pid file.
func RemovePidFile(pidPath string) error {
	return os.Remove(pidPath)
}

// RemoveSocketFile removes the socket file.
func RemoveSocketFile(socketPath string) error {
	return os.Remove(socketPath)
}

// LoadUserConfig loads the user config.
func (s *Server) LoadUserConfig() (config.UserConfig, error) {
	cfg, _, err := config.LoadUserConfig(s.FS, s.ConfigDir)
	return cfg, err
}

// streamOutput copies data from reader to writer while updating lastOutputAt.
func (s *Server) streamOutput(proc *SupervisedProcess, reader io.Reader, file *os.File) {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			_, _ = file.Write(buf[:n])
			// Update last output timestamp (atomic store, not mutex)
			proc.lastOutputAt = s.Clock().UnixNano()
		}
		if err != nil {
			break
		}
	}
}

// idempotencyKey generates a key for the idempotency map.
func idempotencyKey(repoID, clientRequestID string) string {
	return repoID + ":" + clientRequestID
}

// checkIdempotency checks if a request is a duplicate based on client_request_id.
// Returns (invocation_id, true) if this is a duplicate request.
// Returns ("", false) if this is a new request.
func (s *Server) checkIdempotency(repoID, clientRequestID string) (string, bool) {
	if clientRequestID == "" {
		return "", false
	}

	s.idempotencyMu.RLock()
	defer s.idempotencyMu.RUnlock()

	key := idempotencyKey(repoID, clientRequestID)
	entry, exists := s.idempotency[key]
	if !exists {
		return "", false
	}

	// Check if entry is expired
	now := s.Clock().Unix()
	if now-entry.CreatedAt > IdempotencyTTL {
		return "", false
	}

	return entry.InvocationID, true
}

// recordIdempotency records a successful request for idempotency.
func (s *Server) recordIdempotency(repoID, clientRequestID, invocationID string) {
	if clientRequestID == "" {
		return
	}

	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()

	key := idempotencyKey(repoID, clientRequestID)
	s.idempotency[key] = IdempotencyEntry{
		InvocationID: invocationID,
		CreatedAt:    s.Clock().Unix(),
	}

	// Opportunistically clean up expired entries (every 100 requests or so)
	if len(s.idempotency) > 100 {
		s.cleanupExpiredIdempotency()
	}
}

// cleanupExpiredIdempotency removes expired entries from the idempotency map.
// Must be called with idempotencyMu held.
func (s *Server) cleanupExpiredIdempotency() {
	now := s.Clock().Unix()
	for key, entry := range s.idempotency {
		if now-entry.CreatedAt > IdempotencyTTL {
			delete(s.idempotency, key)
		}
	}
}

// worktreeIdempotencyKey generates a key for the worktree idempotency map.
func worktreeIdempotencyKey(repoID, idempotencyKey string) string {
	return repoID + ":worktree:" + idempotencyKey
}

// checkWorktreeIdempotency checks if a worktree create request is a duplicate.
// Returns the entry and true if this is a duplicate request.
func (s *Server) checkWorktreeIdempotency(repoID, idempotencyKey string) (WorktreeIdempotencyEntry, bool) {
	if idempotencyKey == "" {
		return WorktreeIdempotencyEntry{}, false
	}

	s.worktreeIdempotencyMu.RLock()
	defer s.worktreeIdempotencyMu.RUnlock()

	key := worktreeIdempotencyKey(repoID, idempotencyKey)
	entry, exists := s.worktreeIdempotency[key]
	if !exists {
		return WorktreeIdempotencyEntry{}, false
	}

	// Check if entry is expired
	now := s.Clock().Unix()
	if now-entry.CreatedAt > IdempotencyTTL {
		return WorktreeIdempotencyEntry{}, false
	}

	return entry, true
}

// recordWorktreeIdempotency records a successful worktree create request.
func (s *Server) recordWorktreeIdempotency(repoID, idempotencyKey, worktreeID, treePath, branch string) {
	if idempotencyKey == "" {
		return
	}

	s.worktreeIdempotencyMu.Lock()
	defer s.worktreeIdempotencyMu.Unlock()

	key := worktreeIdempotencyKey(repoID, idempotencyKey)
	s.worktreeIdempotency[key] = WorktreeIdempotencyEntry{
		WorktreeID: worktreeID,
		TreePath:   treePath,
		Branch:     branch,
		CreatedAt:  s.Clock().Unix(),
	}

	// Opportunistically clean up expired entries
	if len(s.worktreeIdempotency) > 100 {
		s.cleanupExpiredWorktreeIdempotency()
	}
}

// cleanupExpiredWorktreeIdempotency removes expired entries.
// Must be called with worktreeIdempotencyMu held.
func (s *Server) cleanupExpiredWorktreeIdempotency() {
	now := s.Clock().Unix()
	for key, entry := range s.worktreeIdempotency {
		if now-entry.CreatedAt > IdempotencyTTL {
			delete(s.worktreeIdempotency, key)
		}
	}
}

// handleWorktrees handles requests to /worktrees/...
func (s *Server) handleWorktrees(w http.ResponseWriter, r *http.Request) {
	// Parse path: /worktrees[/create] or /worktrees/{id}/rm
	path := r.URL.Path

	// Handle POST /worktrees/create (or /worktrees with trailing /)
	if path == "/worktrees" || path == "/worktrees/" {
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "endpoint required", "use /worktrees/create or /worktrees/{id}/rm")
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

	// Handle POST /worktrees/{id}/rm
	// Find the slash separating id from action
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
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "worktree id required", "")
		return
	}

	switch action {
	case "rm":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleWorktreeRm(w, r, worktreeRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown action: "+action, "supported actions: rm")
	}
}
