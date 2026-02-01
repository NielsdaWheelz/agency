package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// handleStartHeadless handles POST /invocations/{id}/start_headless.
func (s *Server) handleStartHeadless(w http.ResponseWriter, r *http.Request, invocationID string) {
	// Parse request body
	var req StartHeadlessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "")
		return
	}

	// Override invocation ID from URL path
	req.InvocationID = invocationID

	// Validate required fields
	if req.RepoID == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id is required", "")
		return
	}
	if req.Runner == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "runner is required", "")
		return
	}
	if req.SandboxPath == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "sandbox_path is required", "")
		return
	}
	if req.Prompt == "" {
		s.writeError(w, http.StatusBadRequest, string(errors.EPromptRequired), "prompt is required for headless invocation", "")
		return
	}

	// Validation Gate 1: Check invocation meta exists
	meta, err := s.Store.ReadInvocationMeta(req.RepoID, req.InvocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
			s.writeError(w, http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "ensure invocation was created before calling start_headless")
			return
		}
		s.writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}

	// Validation Gate 2: Meta cross-validation
	if meta.Mode != store.RunnerModeHeadless {
		s.writeError(w, http.StatusBadRequest, string(errors.ESandboxValidationFailed), "invocation mode is not headless", "this endpoint is only for headless invocations")
		return
	}
	if meta.SandboxPath != req.SandboxPath {
		s.writeError(w, http.StatusBadRequest, string(errors.ESandboxValidationFailed), "sandbox_path in request does not match invocation meta", "verify sandbox was created correctly")
		return
	}

	// Check for idempotency / already running
	if meta.Status == store.InvocationStatusRunning && meta.PID != nil {
		pid := *meta.PID
		alive := s.PIDChecker(pid)

		if alive {
			// Check if this daemon instance is supervising it
			if meta.DaemonInstanceID == s.InstanceID {
				// Idempotent hit - we're already supervising this
				resp := StartHeadlessResponse{
					OK:               true,
					PID:              pid,
					PGID:             safeIntPtr(meta.PGID),
					DaemonInstanceID: s.InstanceID,
					AlreadyRunning:   true,
					Orphaned:         false,
					LogPaths: &LogPaths{
						Raw:    s.Store.SandboxRawLogPath(req.RepoID, req.InvocationID),
						Stderr: s.Store.SandboxStderrLogPath(req.RepoID, req.InvocationID),
					},
				}
				s.writeJSON(w, http.StatusOK, resp)
				return
			}

			// Process alive but different daemon instance (or no instance) - orphaned
			now := s.Clock().UTC().Format(time.RFC3339)
			_ = s.Store.UpdateInvocationMeta(req.RepoID, req.InvocationID, func(m *store.InvocationMeta) {
				m.Flags.Orphaned = true
				m.OrphanedAt = now
			})

			resp := StartHeadlessResponse{
				OK:               true,
				PID:              pid,
				PGID:             safeIntPtr(meta.PGID),
				DaemonInstanceID: s.InstanceID,
				AlreadyRunning:   true,
				Orphaned:         true,
				LogPaths: &LogPaths{
					Raw:    s.Store.SandboxRawLogPath(req.RepoID, req.InvocationID),
					Stderr: s.Store.SandboxStderrLogPath(req.RepoID, req.InvocationID),
				},
			}
			s.writeJSON(w, http.StatusOK, resp)
			return
		}

		// PID is dead - mark as orphaned/failed
		now := s.Clock().UTC().Format(time.RFC3339)
		_ = s.Store.UpdateInvocationMeta(req.RepoID, req.InvocationID, func(m *store.InvocationMeta) {
			m.Status = store.InvocationStatusFailed
			m.ExitReason = "unknown"
			m.FinishedAt = now
			m.PID = nil
			m.Flags.Orphaned = true
			m.Flags.NeedsAttention = true
			m.LifecycleOwner = ""
		})

		s.writeError(w, http.StatusConflict, string(errors.EInvocationOrphaned), "invocation was running but PID is dead", "process exited without daemon observing; start a new invocation")
		return
	}

	// Check for terminal status
	if meta.Status == store.InvocationStatusFinished || meta.Status == store.InvocationStatusFailed {
		s.writeError(w, http.StatusConflict, string(errors.EInvocationTerminal), "invocation already in terminal state: "+string(meta.Status), "start a new invocation instead")
		return
	}

	// Validation Gate 3: sandbox_path contains SANDBOX_MARKER
	if !invocation.HasSandboxMarker(req.SandboxPath) {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusBadRequest, string(errors.ESandboxValidationFailed), "sandbox does not contain SANDBOX_MARKER", "verify sandbox was created correctly via agent start")
		return
	}

	// Validation Gate 4: sandbox_path does NOT contain INTEGRATION_MARKER
	integrationMarkerPath := filepath.Join(req.SandboxPath, ".agency", "INTEGRATION_MARKER")
	if _, err := os.Stat(integrationMarkerPath); err == nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusBadRequest, string(errors.ESandboxValidationFailed), "sandbox contains INTEGRATION_MARKER - refusing to run in integration tree", "this is a bug - sandbox path resolved to integration tree")
		return
	}

	// Validation Gate 5: sandbox_path equals computed expected path
	expectedSandboxPath := s.Store.SandboxTreePath(req.RepoID, req.InvocationID)
	if req.SandboxPath != expectedSandboxPath {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusBadRequest, string(errors.ESandboxValidationFailed),
			fmt.Sprintf("sandbox_path does not match expected path: got %s, expected %s", req.SandboxPath, expectedSandboxPath),
			"verify sandbox was created correctly")
		return
	}

	// Validation Gate 6: runner is recognized
	if req.Runner != "claude" && req.Runner != "codex" {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusBadRequest, string(errors.ERunnerNotFound), "unrecognized runner: "+req.Runner, "valid runners: claude, codex")
		return
	}

	// Load user config for runner resolution
	userCfg, err := s.LoadUserConfig()
	if err != nil {
		// Non-fatal, use defaults
		userCfg = config.UserConfig{}
	}

	// Resolve runner command
	runnerCmd, err := config.ResolveRunnerCmd(s.Runner, s.FS, s.ConfigDir, userCfg, req.Runner)
	if err != nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusInternalServerError, string(errors.ERunnerNotFound), "failed to resolve runner command: "+err.Error(), "ensure runner is installed and configured")
		return
	}

	// Build command arguments
	args := buildRunnerArgs(req.Runner, req.Prompt, req.RunnerArgs)

	// Create logs directory
	logsDir := s.Store.SandboxLogsDir(req.RepoID, req.InvocationID)
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to create logs directory: "+err.Error(), "")
		return
	}

	// Write prompt file
	promptPath := s.Store.InvocationPromptPath(req.RepoID, req.InvocationID)
	promptHash := sha256.Sum256([]byte(req.Prompt))
	promptSHA := hex.EncodeToString(promptHash[:])

	if err := os.WriteFile(promptPath, []byte(req.Prompt), 0o600); err != nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to write prompt file: "+err.Error(), "")
		return
	}

	// Open log files
	rawLogPath := s.Store.SandboxRawLogPath(req.RepoID, req.InvocationID)
	stderrLogPath := s.Store.SandboxStderrLogPath(req.RepoID, req.InvocationID)

	rawFile, err := os.OpenFile(rawLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to open raw log file: "+err.Error(), "")
		return
	}

	stderrFile, err := os.OpenFile(stderrLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		_ = rawFile.Close()
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to open stderr log file: "+err.Error(), "")
		return
	}

	// Create the command
	cmd := osexec.Command(runnerCmd, args...)
	cmd.Dir = req.SandboxPath
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group
	}

	// Set up environment
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Set up pipes for stdout/stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = rawFile.Close()
		_ = stderrFile.Close()
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusInternalServerError, string(errors.ERunnerStartFailed), "failed to create stdout pipe: "+err.Error(), "")
		return
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = rawFile.Close()
		_ = stderrFile.Close()
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusInternalServerError, string(errors.ERunnerStartFailed), "failed to create stderr pipe: "+err.Error(), "")
		return
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		_ = rawFile.Close()
		_ = stderrFile.Close()
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		s.writeError(w, http.StatusInternalServerError, string(errors.ERunnerStartFailed), "failed to start runner: "+err.Error(), "")
		return
	}

	pid := cmd.Process.Pid
	pgid := pid // With Setpgid=true, the child becomes its own process group leader

	// Create supervised process record
	proc := &SupervisedProcess{
		InvocationID: req.InvocationID,
		RepoID:       req.RepoID,
		PID:          pid,
		PGID:         pgid,
		RawLogFile:   rawLogPath,
		StderrFile:   stderrLogPath,
		done:         make(chan struct{}),
	}

	// Register the process
	s.mu.Lock()
	s.processes[req.InvocationID] = proc
	s.mu.Unlock()

	// Update invocation meta
	now := s.Clock().UTC().Format(time.RFC3339)
	daemonPID := os.Getpid()
	err = s.Store.UpdateInvocationMeta(req.RepoID, req.InvocationID, func(m *store.InvocationMeta) {
		m.Status = store.InvocationStatusRunning
		m.PID = &pid
		m.PGID = &pgid
		m.DaemonPID = &daemonPID
		m.DaemonInstanceID = s.InstanceID
		m.ClaimedAt = now
		m.LifecycleOwner = "daemon"
		m.PromptPath = promptPath
		m.PromptSHA256 = promptSHA
	})
	if err != nil {
		// Best-effort: kill the process we just started
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = rawFile.Close()
		_ = stderrFile.Close()
		s.mu.Lock()
		delete(s.processes, req.InvocationID)
		s.mu.Unlock()
		s.writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to update invocation meta: "+err.Error(), "")
		return
	}

	// Start goroutines to stream output
	go s.streamOutput(proc, stdoutPipe, rawFile)
	go s.streamOutput(proc, stderrPipe, stderrFile)

	// Start goroutine to wait for process exit
	go s.waitForExit(proc, cmd, rawFile, stderrFile)

	// Start goroutine to periodically flush lastOutputAt
	go s.runOutputFlushLoop(proc)

	// Return success
	resp := StartHeadlessResponse{
		OK:               true,
		PID:              pid,
		PGID:             pgid,
		DaemonInstanceID: s.InstanceID,
		AlreadyRunning:   false,
		Orphaned:         false,
		LogPaths: &LogPaths{
			Raw:    rawLogPath,
			Stderr: stderrLogPath,
		},
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// handleStop handles POST /invocations/{id}/stop.
// Implements PR-05 stop semantics: SIGINT → wait 5s → SIGTERM → wait 2s → SIGKILL
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request, invocationID string) {
	// Read repo_id from query params
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	// Read invocation meta
	meta, err := s.Store.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.writeError(w, http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		s.writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}

	// Only for headless invocations
	if meta.Mode != store.RunnerModeHeadless {
		s.writeError(w, http.StatusBadRequest, string(errors.EInvocationInvalidMode), "stop via daemon is only for headless invocations", "use tmux send-keys for headed invocations")
		return
	}

	// Get PGID from meta (for orphaned invocations)
	pgid := 0
	if meta.PGID != nil {
		pgid = *meta.PGID
	} else if meta.PID != nil {
		pgid = *meta.PID // Fallback: assume PGID == PID
	}

	if pgid <= 0 {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "no PGID available to signal", "invocation may not have started properly")
		return
	}

	// Update invocation meta - mark stop requested
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		m.StopRequestedAt = now
		m.Flags.NeedsAttention = true
	})

	// Check if we're supervising this process
	s.mu.RLock()
	proc, supervised := s.processes[invocationID]
	s.mu.RUnlock()

	// Start stop escalation in background goroutine (PR-05)
	// SIGINT → wait 5s → SIGTERM → wait 2s → SIGKILL
	go s.stopEscalation(repoID, invocationID, pgid, supervised, proc)

	resp := StopResponse{OK: true}
	s.writeJSON(w, http.StatusOK, resp)
}

// stopEscalation performs the stop signal escalation: SIGINT → SIGTERM → SIGKILL
func (s *Server) stopEscalation(repoID, invocationID string, pgid int, supervised bool, proc *SupervisedProcess) {
	// Step 1: Send SIGINT
	err := syscall.Kill(-pgid, syscall.SIGINT)
	if err == syscall.ESRCH {
		// Process already gone
		return
	}

	// Step 2: Wait up to 5 seconds for process to exit
	if supervised && proc != nil {
		select {
		case <-proc.done:
			// Process exited gracefully
			return
		case <-time.After(5 * time.Second):
			// Continue to SIGTERM
		}
	} else {
		// Not supervised - just wait and check PID
		time.Sleep(5 * time.Second)
		if !s.PIDChecker(pgid) {
			return // Process exited
		}
	}

	// Step 3: Send SIGTERM
	err = syscall.Kill(-pgid, syscall.SIGTERM)
	if err == syscall.ESRCH {
		return
	}

	// Step 4: Wait up to 2 seconds
	if supervised && proc != nil {
		select {
		case <-proc.done:
			return
		case <-time.After(2 * time.Second):
			// Continue to SIGKILL
		}
	} else {
		time.Sleep(2 * time.Second)
		if !s.PIDChecker(pgid) {
			return
		}
	}

	// Step 5: Send SIGKILL
	_ = syscall.Kill(-pgid, syscall.SIGKILL)

	// If not supervised, we need to mark the invocation as stopped
	if !supervised {
		now := s.Clock().UTC().Format(time.RFC3339)
		_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
			m.Status = store.InvocationStatusFailed
			m.ExitReason = "killed"
			m.FailureReason = "stopped"
			m.FinishedAt = now
			m.PID = nil
			m.LifecycleOwner = ""
		})
	}
}

// handleKill handles POST /invocations/{id}/kill.
func (s *Server) handleKill(w http.ResponseWriter, r *http.Request, invocationID string) {
	// Read repo_id from query params
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	// Read invocation meta
	meta, err := s.Store.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.writeError(w, http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		s.writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}

	// Only for headless invocations
	if meta.Mode != store.RunnerModeHeadless {
		s.writeError(w, http.StatusBadRequest, string(errors.EInvocationInvalidMode), "kill via daemon is only for headless invocations", "use tmux kill-session for headed invocations")
		return
	}

	// Get PGID from meta
	pgid := 0
	if meta.PGID != nil {
		pgid = *meta.PGID
	} else if meta.PID != nil {
		pgid = *meta.PID // Fallback
	}

	// Send SIGKILL if we have a PGID
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}

	// Update invocation meta - always mark as killed regardless of current state
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		m.Status = store.InvocationStatusFailed
		m.ExitReason = "killed"
		m.FailureReason = "killed" // PR-05: set failure_reason
		m.FinishedAt = now
		m.PID = nil
		m.LifecycleOwner = ""
	})

	// Remove from supervised processes if present
	s.mu.Lock()
	delete(s.processes, invocationID)
	s.mu.Unlock()

	resp := KillResponse{OK: true}
	s.writeJSON(w, http.StatusOK, resp)
}

// waitForExit waits for the process to exit and updates meta.
func (s *Server) waitForExit(proc *SupervisedProcess, cmd *osexec.Cmd, rawFile, stderrFile *os.File) {
	defer func() { _ = rawFile.Close() }()
	defer func() { _ = stderrFile.Close() }()
	defer close(proc.done)

	// Wait for process to exit
	err := cmd.Wait()

	// Determine exit code and status
	exitCode := 0
	exitReason := "exited"
	status := store.InvocationStatusFinished

	if err != nil {
		if exitErr, ok := err.(*osexec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		status = store.InvocationStatusFailed
	}

	// Update invocation meta
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
		meta.Status = status
		meta.ExitReason = exitReason
		meta.ExitCode = &exitCode
		meta.FinishedAt = now
		meta.PID = nil
		meta.LifecycleOwner = ""
	})

	// Remove from supervised processes
	s.mu.Lock()
	delete(s.processes, proc.InvocationID)
	s.mu.Unlock()
}

// runOutputFlushLoop periodically flushes lastOutputAt to meta.json.
func (s *Server) runOutputFlushLoop(proc *SupervisedProcess) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastFlushed int64

	for {
		select {
		case <-proc.done:
			// Final flush
			if proc.lastOutputAt > lastFlushed {
				s.flushLastOutputAt(proc)
			}
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			// Only flush if there's new output
			if proc.lastOutputAt > lastFlushed {
				s.flushLastOutputAt(proc)
				lastFlushed = proc.lastOutputAt
			}
		}
	}
}

// markInvocationFailed marks an invocation as failed with the given exit reason.
func (s *Server) markInvocationFailed(repoID, invocationID, exitReason string) {
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFailed
		meta.ExitReason = exitReason
		meta.FinishedAt = now
		meta.Flags.NeedsAttention = true
	})
}

// buildRunnerArgs builds the command arguments for a runner.
func buildRunnerArgs(runner, prompt string, extraArgs []string) []string {
	var args []string

	switch runner {
	case "claude":
		// claude -p --output-format stream-json --verbose "<prompt>"
		args = append(args, "-p", "--output-format", "stream-json", "--verbose")
		args = append(args, extraArgs...)
		args = append(args, prompt)
	case "codex":
		// codex exec --json "<prompt>"
		// Note: -C <dir> is handled via cmd.Dir
		args = append(args, "exec", "--json")
		args = append(args, extraArgs...)
		args = append(args, prompt)
	default:
		// Unknown runner, just pass prompt as first arg
		args = append(args, extraArgs...)
		args = append(args, prompt)
	}

	return args
}

// safeIntPtr returns the int value of a pointer or 0 if nil.
func safeIntPtr(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// handleControlPlaneStartHeadless handles POST /invocations/start_headless (PR-05 control plane).
// This is the new endpoint where daemon creates everything: invocation, sandbox, and starts runner.
func (s *Server) handleControlPlaneStartHeadless(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req ControlPlaneStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeControlPlaneError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "", "")
		return
	}

	// 1. Validate required fields
	if req.ClientRequestID == "" {
		s.writeControlPlaneError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "client_request_id is required", "provide a UUID for idempotency", "")
		return
	}
	if req.RepoRoot == "" {
		s.writeControlPlaneError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "repo_root is required", "", req.ClientRequestID)
		return
	}
	if req.WorktreeRef == "" {
		s.writeControlPlaneError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "worktree_ref is required", "", req.ClientRequestID)
		return
	}
	if req.Runner == "" {
		s.writeControlPlaneError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "runner is required", "", req.ClientRequestID)
		return
	}
	if req.Prompt == "" {
		s.writeControlPlaneError(w, http.StatusBadRequest, string(errors.EPromptRequired), "prompt is required for headless invocation", "", req.ClientRequestID)
		return
	}

	// 2. Validate prompt size (max 256KB)
	if len(req.Prompt) > MaxPromptSize {
		s.writeControlPlaneError(w, http.StatusBadRequest, string(errors.EPromptTooLarge),
			fmt.Sprintf("prompt exceeds maximum size of %d bytes (got %d)", MaxPromptSize, len(req.Prompt)),
			"reduce prompt size or split into smaller chunks", req.ClientRequestID)
		return
	}

	// 3. Validate runner
	if req.Runner != "claude" && req.Runner != "codex" {
		s.writeControlPlaneError(w, http.StatusBadRequest, string(errors.ERunnerNotFound),
			"unrecognized runner: "+req.Runner, "valid runners: claude, codex", req.ClientRequestID)
		return
	}

	// 4. Validate reserved flags in runner args
	if err := validateRunnerArgs(req.Runner, req.RunnerArgs); err != nil {
		s.writeControlPlaneError(w, http.StatusBadRequest, string(errors.ERunnerArgConflict),
			err.Error(), "remove reserved flags from runner_args", req.ClientRequestID)
		return
	}

	// 5. Validate invocation name if provided
	if req.InvocationName != "" {
		if err := core.ValidateName(req.InvocationName); err != nil {
			s.writeControlPlaneError(w, http.StatusBadRequest, string(errors.EInvalidName),
				"invalid invocation name: "+err.Error(),
				"names must be 2-40 chars, lowercase alphanumeric + hyphens", req.ClientRequestID)
			return
		}
	}

	// 6. Resolve repo_root to canonical absolute path
	repoRoot, err := filepath.Abs(req.RepoRoot)
	if err != nil {
		s.writeControlPlaneError(w, http.StatusBadRequest, "E_INVALID_REQUEST",
			"failed to resolve repo_root: "+err.Error(), "", req.ClientRequestID)
		return
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		s.writeControlPlaneError(w, http.StatusBadRequest, "E_INVALID_REQUEST",
			"failed to resolve repo_root symlinks: "+err.Error(), "", req.ClientRequestID)
		return
	}

	// 7. Recursion guard: reject if repo_root is inside agency-managed worktree
	if isInsideAgencyManagedWorktree(repoRoot, s.Store.DataDir) {
		s.writeControlPlaneError(w, http.StatusBadRequest, string(errors.EUnsafeRepoRoot),
			"repo_root is inside an agency-managed worktree",
			"use the original repository, not a sandbox or integration worktree", req.ClientRequestID)
		return
	}

	// 8. Derive git root via git rev-parse --show-toplevel
	gitRoot, err := git.GetRepoRoot(ctx, s.Runner, repoRoot)
	if err != nil {
		s.writeControlPlaneError(w, http.StatusBadRequest, string(errors.ENoRepo),
			"repo_root is not inside a git repository: "+err.Error(), "", req.ClientRequestID)
		return
	}
	repoRoot = gitRoot.Path // Use the actual git root

	// 9. Derive repo identity
	originInfo := git.GetOriginInfo(ctx, s.Runner, repoRoot)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot, originInfo.URL)

	// 10. Check idempotency before doing any work
	if existingID, isDuplicate := s.checkIdempotency(repoIdentity.RepoID, req.ClientRequestID); isDuplicate {
		// Return the existing invocation
		meta, err := s.Store.ReadInvocationMeta(repoIdentity.RepoID, existingID)
		if err == nil {
			s.writeControlPlaneSuccess(w, existingID, meta, repoIdentity.RepoID, req.ClientRequestID, true)
			return
		}
		// If we can't read meta, fall through to create new (idempotency entry may be stale)
	}

	// 11. Self-register repo if needed
	if err := s.ensureRepoRegistered(repoIdentity, repoRoot); err != nil {
		s.writeControlPlaneError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to register repo: "+err.Error(), "", req.ClientRequestID)
		return
	}

	// 12. Resolve integration worktree
	wtSvc := integrationworktree.NewService(s.Store, s.Runner, s.FS, s.Clock)
	wtRecord, err := wtSvc.Resolve(repoIdentity.RepoID, req.WorktreeRef, false)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeControlPlaneError(w, http.StatusNotFound, string(code),
			err.Error(), "run 'agency worktree ls' to see available worktrees", req.ClientRequestID)
		return
	}

	if wtRecord.Broken || wtRecord.Meta == nil {
		s.writeControlPlaneError(w, http.StatusBadRequest, string(errors.EWorktreeBroken),
			"integration worktree exists but meta.json is unreadable",
			"inspect or recreate the worktree", req.ClientRequestID)
		return
	}

	if wtRecord.Meta.State != store.WorktreeStatePresent {
		s.writeControlPlaneError(w, http.StatusBadRequest, string(errors.EWorktreeNotFound),
			"integration worktree is archived", "use a present (non-archived) integration worktree", req.ClientRequestID)
		return
	}

	// 13. Check invocation name uniqueness if name provided
	if req.InvocationName != "" {
		if err := s.checkInvocationNameUniqueness(repoIdentity.RepoID, req.InvocationName); err != nil {
			s.writeControlPlaneError(w, http.StatusConflict, string(errors.EInvocationNameExists),
				err.Error(), "use a different name or wait for the existing invocation to complete", req.ClientRequestID)
			return
		}
	}

	// 14. Create invocation and sandbox atomically
	result, err := s.createInvocationAndSandbox(ctx, repoIdentity.RepoID, repoRoot, wtRecord, req)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeControlPlaneError(w, http.StatusInternalServerError, string(code),
			err.Error(), "", req.ClientRequestID)
		return
	}

	// 15. Start the runner process
	pid, pgid, err := s.startRunner(ctx, repoIdentity.RepoID, result, req)
	if err != nil {
		// Cleanup on failure
		s.cleanupFailedInvocation(ctx, repoIdentity.RepoID, result, repoRoot, "spawn_failed")
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerStartFailed
		}
		s.writeControlPlaneError(w, http.StatusInternalServerError, string(code),
			err.Error(), "", req.ClientRequestID)
		return
	}

	// 16. Record idempotency entry
	s.recordIdempotency(repoIdentity.RepoID, req.ClientRequestID, result.InvocationID)

	// 17. Update invocation meta with running state
	now := s.Clock().UTC().Format(time.RFC3339)
	daemonPID := os.Getpid()
	promptHash := sha256.Sum256([]byte(req.Prompt))
	promptSHA := hex.EncodeToString(promptHash[:])

	_ = s.Store.UpdateInvocationMeta(repoIdentity.RepoID, result.InvocationID, func(m *store.InvocationMeta) {
		m.Status = store.InvocationStatusRunning
		m.PID = &pid
		m.PGID = &pgid
		m.DaemonPID = &daemonPID
		m.DaemonInstanceID = s.InstanceID
		m.ClaimedAt = now
		m.LifecycleOwner = "daemon"
		m.PromptPath = s.Store.InvocationPromptPath(repoIdentity.RepoID, result.InvocationID)
		m.PromptSHA256 = promptSHA
	})

	// 18. Read final meta and return success
	meta, _ := s.Store.ReadInvocationMeta(repoIdentity.RepoID, result.InvocationID)
	s.writeControlPlaneSuccess(w, result.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, false)
}

// writeControlPlaneError writes an error response for control plane endpoints.
func (s *Server) writeControlPlaneError(w http.ResponseWriter, status int, code, message, hint, clientRequestID string) {
	resp := ControlPlaneStartResponse{
		OK:              false,
		ErrorCode:       code,
		Message:         message,
		Hint:            hint,
		APIVersion:      APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: clientRequestID,
	}
	s.writeJSON(w, status, resp)
}

// writeControlPlaneSuccess writes a success response for control plane endpoints.
func (s *Server) writeControlPlaneSuccess(w http.ResponseWriter, invocationID string, meta *store.InvocationMeta, repoID, clientRequestID string, alreadyRunning bool) {
	resp := ControlPlaneStartResponse{
		OK:                    true,
		InvocationID:          invocationID,
		SandboxPath:           meta.SandboxPath,
		RepoID:                repoID,
		IntegrationWorktreeID: meta.IntegrationWorktreeID,
		AlreadyRunning:        alreadyRunning,
		APIVersion:            APIVersion,
		BuildVersion:          version.FullVersion(),
		ClientRequestID:       clientRequestID,
		DaemonInstanceID:      s.InstanceID,
	}

	if meta.PID != nil {
		resp.PID = *meta.PID
	}
	if meta.PGID != nil {
		resp.PGID = *meta.PGID
	}

	resp.LogPaths = &LogPaths{
		Raw:    s.Store.SandboxRawLogPath(repoID, invocationID),
		Stderr: s.Store.SandboxStderrLogPath(repoID, invocationID),
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// validateRunnerArgs checks for reserved flags in runner args.
func validateRunnerArgs(runner string, args []string) error {
	reservedClaude := []string{
		"--output-format", "-p", "--print", "--verbose",
	}
	reservedCodex := []string{
		"exec", "--json", "-C", "--cd",
	}

	var reserved []string
	switch runner {
	case "claude":
		reserved = reservedClaude
	case "codex":
		reserved = reservedCodex
	default:
		return nil
	}

	for _, arg := range args {
		// Check exact match
		for _, r := range reserved {
			if arg == r {
				return fmt.Errorf("reserved flag '%s' cannot be passed via runner_args", arg)
			}
			// Check prefix form (e.g., --output-format=json)
			if strings.HasPrefix(arg, r+"=") {
				return fmt.Errorf("reserved flag '%s' cannot be passed via runner_args", r)
			}
		}
	}

	return nil
}

// isInsideAgencyManagedWorktree checks if a path is inside an agency-managed worktree.
func isInsideAgencyManagedWorktree(path, dataDir string) bool {
	// Clean paths for comparison
	cleanPath := filepath.Clean(path)
	cleanDataDir := filepath.Clean(dataDir)

	// Check if path is under ${DATA_DIR}/repos/*/worktrees/*/tree
	// or ${DATA_DIR}/repos/*/sandboxes/*/tree
	reposDir := filepath.Join(cleanDataDir, "repos")
	if !strings.HasPrefix(cleanPath, reposDir) {
		return false
	}

	// Check for worktrees or sandboxes pattern
	rel, err := filepath.Rel(reposDir, cleanPath)
	if err != nil {
		return false
	}

	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 4 {
		return false
	}

	// Pattern: <repo_id>/<worktrees|sandboxes>/<id>/tree[/...]
	// or: <repo_id>/integration_worktrees/<id>/tree[/...]
	if parts[1] == "integration_worktrees" || parts[1] == "sandboxes" {
		if len(parts) >= 4 && parts[3] == "tree" {
			return true
		}
	}

	return false
}

// ensureRepoRegistered ensures the repo is registered in repo_index.json.
func (s *Server) ensureRepoRegistered(repoIdentity identity.RepoIdentity, repoRoot string) error {
	idx, err := s.Store.LoadRepoIndex()
	if err != nil {
		return err
	}

	idx = s.Store.UpsertRepoIndexEntry(idx, repoIdentity.RepoKey, repoIdentity.RepoID, repoRoot)

	return s.Store.SaveRepoIndex(idx)
}

// checkInvocationNameUniqueness checks if an invocation name is already used by an active invocation.
func (s *Server) checkInvocationNameUniqueness(repoID, name string) error {
	records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return fmt.Errorf("failed to scan invocations: %w", err)
	}

	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}

		// Skip terminal invocations
		if r.Meta.Status == store.InvocationStatusFinished || r.Meta.Status == store.InvocationStatusFailed {
			continue
		}
		if r.Meta.LandingStatus == store.LandingStatusLanded || r.Meta.LandingStatus == store.LandingStatusDiscarded {
			continue
		}

		if r.Meta.InvocationName == name {
			return fmt.Errorf("invocation name '%s' is already used by active invocation %s", name, r.InvocationID)
		}
	}

	return nil
}

// invocationCreateResult holds the result of createInvocationAndSandbox.
type invocationCreateResult struct {
	InvocationID          string
	SandboxPath           string
	SandboxBranch         string
	BaseCommit            string
	IntegrationWorktreeID string // PR-06: track worktree for rm guard
}

// createInvocationAndSandbox creates invocation directory, sandbox directory, and git worktree atomically.
func (s *Server) createInvocationAndSandbox(ctx context.Context, repoID, repoRoot string, wtRecord *store.IntegrationWorktreeRecord, req ControlPlaneStartRequest) (*invocationCreateResult, error) {
	// 1. Generate invocation_id
	invocationID, err := core.NewRunID(s.Clock())
	if err != nil {
		return nil, fmt.Errorf("failed to generate invocation_id: %w", err)
	}

	// 2. Compute paths
	sandboxTreePath := s.Store.SandboxTreePath(repoID, invocationID)
	sandboxBranch := "agency/sandbox-" + invocationID
	integrationTreePath := wtRecord.Meta.TreePath
	integrationBranch := wtRecord.Meta.Branch

	// 3. Validate sandbox path safety
	if err := validateSandboxPath(sandboxTreePath, integrationTreePath); err != nil {
		return nil, err
	}

	// 4. Create invocation directory (exclusive)
	_, err = s.Store.EnsureInvocationDir(repoID, invocationID)
	if err != nil {
		return nil, err
	}

	// Track cleanup state
	invocationDirCreated := true
	sandboxDirCreated := false
	gitWorktreeCreated := false
	branchCreated := false

	// Cleanup function
	cleanup := func() {
		if gitWorktreeCreated {
			args := []string{"-C", repoRoot, "worktree", "remove", "--force", sandboxTreePath}
			_, _ = s.Runner.Run(ctx, "git", args, exec.RunOpts{})
		}
		if branchCreated {
			args := []string{"-C", repoRoot, "branch", "-D", sandboxBranch}
			_, _ = s.Runner.Run(ctx, "git", args, exec.RunOpts{})
		}
		if sandboxDirCreated {
			_ = s.Store.RemoveSandboxDir(repoID, invocationID)
		}
		if invocationDirCreated {
			_ = s.Store.RemoveInvocationDir(repoID, invocationID)
		}
	}

	// 5. Create sandbox directory
	_, err = s.Store.EnsureSandboxDir(repoID, invocationID)
	if err != nil {
		cleanup()
		return nil, err
	}
	sandboxDirCreated = true

	// 6. Capture base_commit
	baseCommitResult, err := s.Runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"rev-parse", integrationBranch,
	}, exec.RunOpts{})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to get base commit: %w", err)
	}
	if baseCommitResult.ExitCode != 0 {
		cleanup()
		return nil, fmt.Errorf("failed to get base commit: %s", strings.TrimSpace(baseCommitResult.Stderr))
	}
	baseCommit := strings.TrimSpace(baseCommitResult.Stdout)

	// 7. Create sandbox git worktree + branch
	args := []string{
		"-C", repoRoot,
		"worktree", "add",
		"-b", sandboxBranch,
		sandboxTreePath,
		integrationBranch,
	}

	result, err := s.Runner.Run(ctx, "git", args, exec.RunOpts{})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to execute git worktree add: %w", err)
	}
	if result.ExitCode != 0 {
		cleanup()
		return nil, fmt.Errorf("git worktree add failed: %s", strings.TrimSpace(result.Stderr))
	}

	gitWorktreeCreated = true
	branchCreated = true

	// 8. Write SANDBOX_MARKER
	agencyDir := filepath.Join(sandboxTreePath, ".agency")
	if err := s.FS.MkdirAll(agencyDir, 0o755); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to create .agency directory in sandbox: %w", err)
	}

	markerPath := filepath.Join(agencyDir, invocation.SandboxMarkerFileName)
	markerContent := "# This directory is a sandbox worktree.\n# Runners may execute here.\n"
	if err := s.FS.WriteFile(markerPath, []byte(markerContent), 0o644); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to write SANDBOX_MARKER: %w", err)
	}

	// 9. Verify sandbox does NOT have integration marker
	if integrationworktree.HasIntegrationMarker(sandboxTreePath) {
		cleanup()
		return nil, fmt.Errorf("CRITICAL: sandbox tree contains INTEGRATION_MARKER")
	}

	// 10. Write invocation meta.json with status=starting
	meta := store.NewInvocationMeta(
		invocationID,
		req.InvocationName,
		wtRecord.WorktreeID,
		sandboxTreePath,
		sandboxBranch,
		baseCommit,
		req.Runner,
		store.RunnerModeHeadless,
		s.Clock(),
	)

	if err := s.Store.WriteInvocationMeta(repoID, invocationID, meta); err != nil {
		cleanup()
		return nil, err
	}

	// 11. Create logs directory
	logsDir := s.Store.SandboxLogsDir(repoID, invocationID)
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	// 12. Write prompt file
	promptPath := s.Store.InvocationPromptPath(repoID, invocationID)
	if err := os.WriteFile(promptPath, []byte(req.Prompt), 0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to write prompt file: %w", err)
	}

	return &invocationCreateResult{
		InvocationID:          invocationID,
		SandboxPath:           sandboxTreePath,
		SandboxBranch:         sandboxBranch,
		BaseCommit:            baseCommit,
		IntegrationWorktreeID: wtRecord.WorktreeID,
	}, nil
}

// validateSandboxPath ensures the sandbox path does not resolve to the integration tree.
func validateSandboxPath(sandboxPath, integrationPath string) error {
	sandboxClean := filepath.Clean(sandboxPath)
	integrationClean := filepath.Clean(integrationPath)

	if sandboxClean == integrationClean {
		return fmt.Errorf("sandbox path equals integration tree path")
	}

	if strings.HasPrefix(integrationClean, sandboxClean+string(filepath.Separator)) {
		return fmt.Errorf("sandbox path is a parent of integration tree path")
	}

	if strings.HasPrefix(sandboxClean, integrationClean+string(filepath.Separator)) {
		return fmt.Errorf("sandbox path is a child of integration tree path")
	}

	if integrationworktree.HasIntegrationMarker(sandboxPath) {
		return fmt.Errorf("sandbox path already contains INTEGRATION_MARKER")
	}

	return nil
}

// startRunner starts the runner process and returns PID and PGID.
func (s *Server) startRunner(ctx context.Context, repoID string, result *invocationCreateResult, req ControlPlaneStartRequest) (int, int, error) {
	// Load user config for runner resolution
	userCfg, err := s.LoadUserConfig()
	if err != nil {
		userCfg = config.UserConfig{}
	}

	// Resolve runner command
	runnerCmd, err := config.ResolveRunnerCmd(s.Runner, s.FS, s.ConfigDir, userCfg, req.Runner)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to resolve runner command: %w", err)
	}

	// Build command arguments
	args := buildRunnerArgsWithSandbox(req.Runner, req.Prompt, result.SandboxPath, req.RunnerArgs)

	// Open log files
	rawLogPath := s.Store.SandboxRawLogPath(repoID, result.InvocationID)
	stderrLogPath := s.Store.SandboxStderrLogPath(repoID, result.InvocationID)

	rawFile, err := os.OpenFile(rawLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open raw log file: %w", err)
	}

	stderrFile, err := os.OpenFile(stderrLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		_ = rawFile.Close()
		return 0, 0, fmt.Errorf("failed to open stderr log file: %w", err)
	}

	// Create the command
	cmd := osexec.Command(runnerCmd, args...)
	cmd.Dir = result.SandboxPath
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Set up environment
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Set up pipes for stdout/stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = rawFile.Close()
		_ = stderrFile.Close()
		return 0, 0, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = rawFile.Close()
		_ = stderrFile.Close()
		return 0, 0, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		_ = rawFile.Close()
		_ = stderrFile.Close()
		return 0, 0, fmt.Errorf("failed to start runner: %w", err)
	}

	pid := cmd.Process.Pid
	pgid := pid

	// Create supervised process record
	proc := &SupervisedProcess{
		InvocationID:          result.InvocationID,
		RepoID:                repoID,
		IntegrationWorktreeID: result.IntegrationWorktreeID, // PR-06: track for worktree rm guard
		PID:                   pid,
		PGID:                  pgid,
		RawLogFile:            rawLogPath,
		StderrFile:            stderrLogPath,
		done:                  make(chan struct{}),
	}

	// Register the process
	s.mu.Lock()
	s.processes[result.InvocationID] = proc
	s.mu.Unlock()

	// Start goroutines to stream output
	go s.streamOutput(proc, stdoutPipe, rawFile)
	go s.streamOutput(proc, stderrPipe, stderrFile)

	// Start goroutine to wait for process exit
	go s.waitForExitWithFailureReason(proc, cmd, rawFile, stderrFile)

	// Start goroutine to periodically flush lastOutputAt
	go s.runOutputFlushLoop(proc)

	return pid, pgid, nil
}

// buildRunnerArgsWithSandbox builds the command arguments for a runner, including sandbox path for codex.
func buildRunnerArgsWithSandbox(runner, prompt, sandboxPath string, extraArgs []string) []string {
	var args []string

	switch runner {
	case "claude":
		// claude -p --output-format stream-json --verbose "<prompt>"
		args = append(args, "-p", "--output-format", "stream-json", "--verbose")
		args = append(args, extraArgs...)
		args = append(args, prompt)
	case "codex":
		// codex -C <sandbox_path> exec --json "<prompt>"
		// PR-05 fix: include -C flag for codex
		args = append(args, "-C", sandboxPath, "exec", "--json")
		args = append(args, extraArgs...)
		args = append(args, prompt)
	default:
		args = append(args, extraArgs...)
		args = append(args, prompt)
	}

	return args
}

// cleanupFailedInvocation cleans up after a failed invocation start.
func (s *Server) cleanupFailedInvocation(ctx context.Context, repoID string, result *invocationCreateResult, repoRoot, failureReason string) {
	// Mark invocation as failed
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, result.InvocationID, func(m *store.InvocationMeta) {
		m.Status = store.InvocationStatusFailed
		m.ExitReason = "start_failed"
		m.FailureReason = failureReason
		m.FinishedAt = now
		m.Flags.NeedsAttention = true
	})

	// Best-effort cleanup of sandbox worktree
	args := []string{"-C", repoRoot, "worktree", "remove", "--force", result.SandboxPath}
	_, _ = s.Runner.Run(ctx, "git", args, exec.RunOpts{})

	// Best-effort cleanup of sandbox branch
	args = []string{"-C", repoRoot, "branch", "-D", result.SandboxBranch}
	_, _ = s.Runner.Run(ctx, "git", args, exec.RunOpts{})

	// Keep invocation directory for debugging/inspection
}

// waitForExitWithFailureReason waits for the process to exit and updates meta with failure_reason.
func (s *Server) waitForExitWithFailureReason(proc *SupervisedProcess, cmd *osexec.Cmd, rawFile, stderrFile *os.File) {
	defer func() { _ = rawFile.Close() }()
	defer func() { _ = stderrFile.Close() }()
	defer close(proc.done)

	// Wait for process to exit
	err := cmd.Wait()

	// Determine exit code and status
	exitCode := 0
	exitReason := "exited"
	failureReason := ""
	status := store.InvocationStatusFinished

	if err != nil {
		if exitErr, ok := err.(*osexec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		status = store.InvocationStatusFailed
		failureReason = "runner_exit_nonzero"
	}

	// Update invocation meta
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
		meta.Status = status
		meta.ExitReason = exitReason
		if failureReason != "" {
			meta.FailureReason = failureReason
		}
		meta.ExitCode = &exitCode
		meta.FinishedAt = now
		meta.PID = nil
		meta.LifecycleOwner = ""
	})

	// Remove from supervised processes
	s.mu.Lock()
	delete(s.processes, proc.InvocationID)
	s.mu.Unlock()
}
