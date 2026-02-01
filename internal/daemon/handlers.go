package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	osexec "os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/store"
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

	// Send SIGINT to process group
	err = syscall.Kill(-pgid, syscall.SIGINT)
	if err != nil && err != syscall.ESRCH {
		// ESRCH means process doesn't exist, which is fine
		s.writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to send SIGINT: "+err.Error(), "")
		return
	}

	// Update invocation meta
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		m.StopRequestedAt = now
		m.Flags.NeedsAttention = true
	})

	resp := StopResponse{OK: true}
	s.writeJSON(w, http.StatusOK, resp)
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
