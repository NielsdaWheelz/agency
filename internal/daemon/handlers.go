package daemon

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/relay"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// handleStartHeadless handles POST /invocations/{id}/start_headless.
func (s *Server) handleStartHeadless(w http.ResponseWriter, r *http.Request, invocationID string) {
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	writeErr := func(status int, code, message, hint string) {
		s.writeErrorWithRequestID(w, status, requestID, code, message, hint)
	}

	// Parse request body
	var req StartHeadlessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "")
		return
	}

	// Override invocation ID from URL path
	req.InvocationID = invocationID

	// Validate required fields
	if req.RepoID == "" {
		writeErr(http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id is required", "")
		return
	}
	if req.Runner == "" {
		writeErr(http.StatusBadRequest, "E_INVALID_REQUEST", "runner is required", "")
		return
	}
	if req.SandboxPath == "" {
		writeErr(http.StatusBadRequest, "E_INVALID_REQUEST", "sandbox_path is required", "")
		return
	}
	if req.Prompt == "" {
		writeErr(http.StatusBadRequest, string(errors.EPromptRequired), "prompt is required for headless invocation", "")
		return
	}

	// Validation Gate 1: Check invocation meta exists
	meta, err := s.Store.ReadInvocationMeta(req.RepoID, req.InvocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
			writeErr(http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "ensure invocation was created before calling start_headless")
			return
		}
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}

	// Validation Gate 2: Meta cross-validation
	if meta.Mode != store.RunnerModeHeadless {
		writeErr(http.StatusBadRequest, string(errors.ESandboxValidationFailed), "invocation mode is not headless", "this endpoint is only for headless invocations")
		return
	}
	if meta.SandboxPath != req.SandboxPath {
		writeErr(http.StatusBadRequest, string(errors.ESandboxValidationFailed), "sandbox_path in request does not match invocation meta", "verify sandbox was created correctly")
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
					RequestID:        requestID,
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
				RequestID:        requestID,
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

		writeErr(http.StatusConflict, string(errors.EInvocationOrphaned), "invocation was running but PID is dead", "process exited without daemon observing; start a new invocation")
		return
	}

	// Check for terminal status
	if meta.Status == store.InvocationStatusFinished || meta.Status == store.InvocationStatusFailed {
		writeErr(http.StatusConflict, string(errors.EInvocationTerminal), "invocation already in terminal state: "+string(meta.Status), "start a new invocation instead")
		return
	}

	// Validation Gate 3: sandbox_path contains SANDBOX_MARKER
	if !invocation.HasSandboxMarker(req.SandboxPath) {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		writeErr(http.StatusBadRequest, string(errors.ESandboxValidationFailed), "sandbox does not contain SANDBOX_MARKER", "verify sandbox was created correctly via agent start")
		return
	}

	// Validation Gate 4: sandbox_path does NOT contain INTEGRATION_MARKER
	integrationMarkerPath := filepath.Join(req.SandboxPath, ".agency", "INTEGRATION_MARKER")
	if _, err := os.Stat(integrationMarkerPath); err == nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		writeErr(http.StatusBadRequest, string(errors.ESandboxValidationFailed), "sandbox contains INTEGRATION_MARKER - refusing to run in integration tree", "this is a bug - sandbox path resolved to integration tree")
		return
	}

	// Validation Gate 5: sandbox_path equals computed expected path
	expectedSandboxPath := s.Store.SandboxTreePath(req.RepoID, req.InvocationID)
	if req.SandboxPath != expectedSandboxPath {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		writeErr(http.StatusBadRequest, string(errors.ESandboxValidationFailed),
			fmt.Sprintf("sandbox_path does not match expected path: got %s, expected %s", req.SandboxPath, expectedSandboxPath),
			"verify sandbox was created correctly")
		return
	}

	// Validation Gate 6: runner is recognized and canonicalized.
	canonicalRunner, err := runners.Canonicalize(req.Runner)
	if err != nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		writeErr(http.StatusBadRequest, string(errors.ERunnerNotFound), err.Error(), "valid runners: "+strings.Join(runners.CanonicalIDs(), ", "))
		return
	}
	req.Runner = canonicalRunner

	// Validation Gate 7: headless reserved runner args are rejected.
	if err := validateHeadlessRunnerArgs(req.Runner, req.RunnerArgs); err != nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerArgConflict
		}
		hint := "remove reserved flags from runner_args"
		if code == errors.ERunnerNotFound {
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		}
		writeErr(http.StatusBadRequest, string(code), err.Error(), hint)
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
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerNotConfigured
		}
		writeErr(
			http.StatusBadRequest,
			string(code),
			"failed to resolve runner command: "+err.Error(),
			"set explicit config.runners.<runner-id> mapping to an installed executable",
		)
		return
	}

	// Build command arguments
	args, err := buildRunnerArgsWithSandbox(req.Runner, req.Prompt, req.SandboxPath, req.RunnerArgs)
	if err != nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerNotFound
		}
		writeErr(http.StatusBadRequest, string(code), err.Error(), "valid runners: "+strings.Join(runners.CanonicalIDs(), ", "))
		return
	}

	// Create logs directory
	logsDir := s.Store.SandboxLogsDir(req.RepoID, req.InvocationID)
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to create logs directory: "+err.Error(), "")
		return
	}

	// Write prompt file
	promptPath := s.Store.InvocationPromptPath(req.RepoID, req.InvocationID)
	promptHash := sha256.Sum256([]byte(req.Prompt))
	promptSHA := hex.EncodeToString(promptHash[:])

	if err := os.WriteFile(promptPath, []byte(req.Prompt), 0o600); err != nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to write prompt file: "+err.Error(), "")
		return
	}

	// Open log files
	rawLogPath := s.Store.SandboxRawLogPath(req.RepoID, req.InvocationID)
	stderrLogPath := s.Store.SandboxStderrLogPath(req.RepoID, req.InvocationID)
	streamLogPath := s.Store.SandboxStreamLogPath(req.RepoID, req.InvocationID)

	rawFile, err := os.OpenFile(rawLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to open raw log file: "+err.Error(), "")
		return
	}

	stderrFile, err := os.OpenFile(stderrLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		_ = rawFile.Close()
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to open stderr log file: "+err.Error(), "")
		return
	}

	// PR-07: Open stream.jsonl for normalized events
	streamFile, err := os.OpenFile(streamLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		_ = rawFile.Close()
		_ = stderrFile.Close()
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to open stream log file: "+err.Error(), "")
		return
	}

	// Set up deterministic, automation-safe environment for headless runner starts.
	envOverlay := nonInteractiveRunnerEnv()
	for k, v := range req.Env {
		envOverlay[k] = v
	}

	// Create stdin pipe + chat relay for runners that support stdin streaming input.
	stdinReader, chatRelay := createChatRelay(req.Runner)

	// Start runner with live stdout/stderr pipes.
	startedProc, err := exec.StartProcess(context.Background(), runnerCmd, args, exec.StartOpts{
		Dir:        req.SandboxPath,
		Env:        envOverlay,
		Stdin:      stdinReader,
		StdoutPipe: true,
		StderrPipe: true,
		Setpgid:    true,
	})
	// Close the parent's copy of the stdin read end — the child has inherited the FD.
	if stdinReader != nil {
		_ = stdinReader.Close()
	}
	if err != nil {
		if chatRelay != nil {
			_ = chatRelay.Close()
		}
		_ = rawFile.Close()
		_ = stderrFile.Close()
		_ = streamFile.Close()
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		writeErr(http.StatusInternalServerError, string(errors.ERunnerStartFailed), "failed to start runner: "+err.Error(), "")
		return
	}
	if err := sendInitialPromptIfRequired(req.Runner, req.Prompt, chatRelay); err != nil {
		if chatRelay != nil {
			_ = chatRelay.Close()
		}
		if startedProc.PGID > 0 {
			_ = syscall.Kill(-startedProc.PGID, syscall.SIGKILL)
		}
		_ = rawFile.Close()
		_ = stderrFile.Close()
		_ = streamFile.Close()
		s.markInvocationFailed(req.RepoID, req.InvocationID, "start_failed")
		writeErr(http.StatusInternalServerError, string(errors.ERunnerStartFailed), "failed to deliver initial prompt: "+err.Error(), "")
		return
	}

	stdoutPipe := startedProc.StdoutPipe
	stderrPipe := startedProc.StderrPipe
	pid := startedProc.PID
	pgid := startedProc.PGID

	// PR-07/S4 PR-02: keep stream sequence monotonic across append boundaries
	// even for legacy /start_headless path.
	initialSeq := loadMaxStreamSeq(streamLogPath)
	parser := stream.NewParser(req.InvocationID, req.Runner, s.Clock)
	parser.SetInitialSeq(initialSeq)

	// Create supervised process record
	proc := &SupervisedProcess{
		InvocationID:          req.InvocationID,
		RepoID:                req.RepoID,
		IntegrationWorktreeID: meta.IntegrationWorktreeID,
		PID:                   pid,
		PGID:                  pgid,
		SandboxPath:           req.SandboxPath,
		RawLogFile:            rawLogPath,
		StderrFile:            stderrLogPath,
		StreamLogFile:         streamLogPath,
		Runner:                req.Runner,
		RepoRoot:              req.SandboxPath, // Legacy path lacks repo_root; sandbox root is sufficient fallback.
		RunnerArgs:            append([]string(nil), req.RunnerArgs...),
		Env:                   copyStringMap(req.Env),
		Parser:                parser,
		Relay:                 chatRelay,
		done:                  make(chan struct{}),
	}
	parser.SetSessionStartNotify(func(n stream.SessionStartNotification) {
		proc.SetResumeSessionID(n.SessionID)
	})

	// Register the process
	s.mu.Lock()
	s.processes[req.InvocationID] = proc
	s.mu.Unlock()

	// Update invocation meta
	now := s.Clock().UTC().Format(time.RFC3339)
	daemonPID := os.Getpid()
	envKeys := sortedEnvKeys(req.Env)
	runnerArgs := append([]string(nil), req.RunnerArgs...)
	err = s.Store.UpdateInvocationMeta(req.RepoID, req.InvocationID, func(m *store.InvocationMeta) {
		m.Status = store.InvocationStatusRunning
		m.Runner = req.Runner
		m.PID = &pid
		m.PGID = &pgid
		m.DaemonPID = &daemonPID
		m.DaemonInstanceID = s.InstanceID
		m.ClaimedAt = now
		m.LifecycleOwner = "daemon"
		m.PromptPath = promptPath
		m.PromptSHA256 = promptSHA
		m.RunnerArgs = runnerArgs
		m.CustomEnvKeys = envKeys
	})
	if err != nil {
		// Best-effort: kill the process we just started
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = rawFile.Close()
		_ = stderrFile.Close()
		_ = streamFile.Close()
		s.mu.Lock()
		delete(s.processes, req.InvocationID)
		s.mu.Unlock()
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to update invocation meta: "+err.Error(), "")
		return
	}

	// PR-07: Start goroutine to stream and parse stdout
	proc.streamWg.Add(2)
	go s.streamAndParseOutput(proc, stdoutPipe, rawFile, streamFile)
	// Stderr is still streamed without parsing
	go s.streamOutput(proc, stderrPipe, stderrFile)

	// Start goroutine to wait for process exit
	go s.waitForExitWithFailureReason(proc, startedProc, rawFile, stderrFile, streamFile)

	// Start goroutine to periodically flush lastOutputAt and semantic status
	go s.runOutputFlushLoop(proc)
	go s.runSemanticStatusFlushLoop(proc)

	// Return success
	resp := StartHeadlessResponse{
		OK:               true,
		RequestID:        requestID,
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
// PR-05 headless: SIGINT → wait 5s → SIGTERM → wait 2s → SIGKILL
// PR-10 headed: tmux send-keys C-c (does not mark as finished; reconcile handles that)
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request, invocationID string) {
	ctx := r.Context()
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	writeStopError := func(status int, code, message, hint string) {
		s.writeErrorWithRequestID(w, status, requestID, code, message, hint)
	}

	// Read repo_id from query params
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		writeStopError(http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	// Read invocation meta
	meta, err := s.Store.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			writeStopError(http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		writeStopError(http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}

	// Early return if invocation is already in a terminal state
	if meta.Status == store.InvocationStatusFinished || meta.Status == store.InvocationStatusFailed {
		resp := StopResponse{
			OK:              true,
			InvocationID:    invocationID,
			RequestID:       requestID,
			APIVersion:      APIVersion,
			BuildVersion:    version.FullVersion(),
			ClientRequestID: "",
		}
		s.writeJSON(w, http.StatusOK, resp)
		return
	}

	// Update invocation meta - mark stop requested (idempotent)
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		if m.StopRequestedAt == "" {
			m.StopRequestedAt = now
		}
		m.Flags.NeedsAttention = true
	})

	// PR-10: Handle headed invocations via tmux
	if meta.Mode == store.RunnerModeHeaded {
		sessionName := meta.TmuxSession
		if sessionName == "" {
			sessionName = tmux.SessionName(invocationID)
		}

		// Check if session exists
		exists, err := s.TmuxClient.HasSession(ctx, sessionName)
		if err != nil {
			// Non-fatal, log but continue
			fmt.Fprintf(os.Stderr, "warning: could not check tmux session: %v\n", err)
		}

		if !exists {
			// Session already gone - mark as finished if still running
			if meta.Status == store.InvocationStatusRunning {
				finishedAt := s.Clock().UTC().Format(time.RFC3339)
				_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
					m.Status = store.InvocationStatusFinished
					m.ExitReason = "exited"
					m.FinishedAt = finishedAt
				})
			}
			resp := StopResponse{
				OK:              true,
				InvocationID:    invocationID,
				RequestID:       requestID,
				APIVersion:      APIVersion,
				BuildVersion:    version.FullVersion(),
				ClientRequestID: "",
			}
			s.writeJSON(w, http.StatusOK, resp)
			return
		}

		// Send C-c via tmux (does not mark finished per PR-10 spec)
		err = s.TmuxClient.SendKeys(ctx, sessionName, []tmux.Key{tmux.KeyCtrlC})
		if err != nil {
			// Log but don't fail - session may be in a state that doesn't accept input
			fmt.Fprintf(os.Stderr, "warning: could not send C-c to tmux session: %v\n", err)
		}

		resp := StopResponse{
			OK:              true,
			InvocationID:    invocationID,
			RequestID:       requestID,
			APIVersion:      APIVersion,
			BuildVersion:    version.FullVersion(),
			ClientRequestID: "",
		}
		s.writeJSON(w, http.StatusOK, resp)
		return
	}

	// Headless mode: signal escalation
	pgid := 0
	if meta.PGID != nil {
		pgid = *meta.PGID
	} else if meta.PID != nil {
		pgid = *meta.PID // Fallback: assume PGID == PID
	}

	if pgid <= 0 {
		writeStopError(http.StatusBadRequest, "E_INVALID_REQUEST", "no PGID available to signal", "invocation may not have started properly")
		return
	}

	// Check if we're supervising this process
	s.mu.RLock()
	proc, supervised := s.processes[invocationID]
	s.mu.RUnlock()

	// Start stop escalation in background goroutine (PR-05)
	// SIGINT → wait 5s → SIGTERM → wait 2s → SIGKILL
	go s.stopEscalation(repoID, invocationID, pgid, supervised, proc)

	resp := StopResponse{
		OK:              true,
		InvocationID:    invocationID,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: "",
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// stopEscalation performs the stop signal escalation: SIGINT → SIGTERM → SIGKILL
func (s *Server) stopEscalation(repoID, invocationID string, pgid int, supervised bool, proc *SupervisedProcess) {
	// Set exit reason on supervised process BEFORE sending signals,
	// so waitForExit* uses "stopped" instead of "exited" after cmd.Wait() returns.
	if supervised && proc != nil {
		proc.exitReason.Store("stopped")
		proc.failureReason.Store("stopped")
	}

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
// PR-10: Now supports both headless (SIGKILL) and headed (tmux kill-session) invocations.
func (s *Server) handleKill(w http.ResponseWriter, r *http.Request, invocationID string) {
	ctx := r.Context()
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	writeKillError := func(status int, code, message, hint string) {
		s.writeErrorWithRequestID(w, status, requestID, code, message, hint)
	}

	// Read repo_id from query params
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		writeKillError(http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	// Read invocation meta
	meta, err := s.Store.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			writeKillError(http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		writeKillError(http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}

	// PR-10: Handle headed invocations via tmux kill-session
	if meta.Mode == store.RunnerModeHeaded {
		sessionName := meta.TmuxSession
		if sessionName == "" {
			sessionName = tmux.SessionName(invocationID)
		}

		// Kill the tmux session
		err = s.TmuxClient.KillSession(ctx, sessionName)
		if err != nil && !tmux.IsNoSessionErr(err) {
			// Log but don't fail - session may already be gone
			fmt.Fprintf(os.Stderr, "warning: could not kill tmux session: %v\n", err)
		}

		// Mark invocation as failed with exit_reason = "killed"
		now := s.Clock().UTC().Format(time.RFC3339)
		_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
			m.Status = store.InvocationStatusFailed
			m.ExitReason = "killed"
			m.FinishedAt = now
		})

		// Remove from supervised processes if present
		s.mu.Lock()
		if proc, ok := s.processes[invocationID]; ok {
			proc.CloseDone()
			delete(s.processes, invocationID)
		}
		s.mu.Unlock()

		resp := KillResponse{
			OK:              true,
			InvocationID:    invocationID,
			RequestID:       requestID,
			APIVersion:      APIVersion,
			BuildVersion:    version.FullVersion(),
			ClientRequestID: "",
		}
		s.writeJSON(w, http.StatusOK, resp)
		return
	}

	// Headless mode: SIGKILL
	pgid := 0
	if meta.PGID != nil {
		pgid = *meta.PGID
	} else if meta.PID != nil {
		pgid = *meta.PID // Fallback
	}

	// Set exit reason on supervised process BEFORE sending signal,
	// so waitForExit* uses the correct reason after cmd.Wait() returns.
	s.mu.RLock()
	proc, supervised := s.processes[invocationID]
	s.mu.RUnlock()

	if supervised && proc != nil {
		proc.exitReason.Store("killed")
		proc.failureReason.Store("killed")
	}

	// Send SIGKILL if we have a PGID
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}

	// For supervised processes, waitForExit* will write the terminal meta
	// using the exitReason/failureReason we set above. For unsupervised
	// processes (orphaned), we must write meta directly.
	if !supervised {
		now := s.Clock().UTC().Format(time.RFC3339)
		_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
			m.Status = store.InvocationStatusFailed
			m.ExitReason = "killed"
			m.FailureReason = "killed"
			m.FinishedAt = now
			m.PID = nil
			m.LifecycleOwner = ""
		})
	}

	resp := KillResponse{
		OK:              true,
		InvocationID:    invocationID,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: "",
	}
	s.writeJSON(w, http.StatusOK, resp)
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
			if current := s.latestOutputAtUnixNano(proc); current > lastFlushed {
				s.flushLastOutputAt(proc)
			}
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			// Only flush if there's new output
			if current := s.latestOutputAtUnixNano(proc); current > lastFlushed {
				s.flushLastOutputAt(proc)
				lastFlushed = current
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

// safeIntPtr returns the int value of a pointer or 0 if nil.
func safeIntPtr(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// nonInteractiveRunnerEnv returns deterministic defaults for automation-safe
// non-interactive runner launches.
func nonInteractiveRunnerEnv() map[string]string {
	return map[string]string{
		"CI":                  "1",
		"GIT_TERMINAL_PROMPT": "0",
		"GH_PROMPT_DISABLED":  "1",
	}
}

// mergeEnvDeterministic merges base and overlay env maps with deterministic
// ordering, no duplicate keys, and "last overlay wins" semantics.
func mergeEnvDeterministic(baseEnv []string, overlays ...map[string]string) []string {
	merged := make(map[string]string, len(baseEnv))

	for _, entry := range baseEnv {
		key, val, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		merged[key] = val
	}
	for _, overlay := range overlays {
		for k, v := range overlay {
			merged[k] = v
		}
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]string, 0, len(keys))
	for _, k := range keys {
		result = append(result, k+"="+merged[k])
	}
	return result
}

// sortedEnvKeys returns unique env keys in sorted order.
func sortedEnvKeys(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// loadMaxStreamSeq returns the highest seq observed in an existing stream log.
// Used to keep stream sequence ordering monotonic across restart boundaries.
func loadMaxStreamSeq(path string) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	var maxSeq uint64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxTimelineLineBytes)
	for scanner.Scan() {
		var event struct {
			Seq uint64 `json:"seq"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Seq > maxSeq {
			maxSeq = event.Seq
		}
	}
	return maxSeq
}

// handleControlPlaneStartHeadless handles POST /invocations/start_headless (PR-05 control plane).
// This is the new endpoint where daemon creates everything: invocation, sandbox, and starts runner.
func (s *Server) handleControlPlaneStartHeadless(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	writeControlPlaneErr := func(status int, code, message, hint, clientRequestID string) {
		s.writeControlPlaneError(w, status, requestID, code, message, hint, clientRequestID)
	}

	// Parse request body
	var req ControlPlaneStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeControlPlaneErr(http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "", "")
		return
	}

	// 1. Validate required fields
	if req.ClientRequestID == "" {
		writeControlPlaneErr(http.StatusBadRequest, "E_INVALID_REQUEST", "client_request_id is required", "provide a UUID for idempotency", "")
		return
	}
	if req.RepoRoot == "" {
		writeControlPlaneErr(http.StatusBadRequest, "E_INVALID_REQUEST", "repo_root is required", "", req.ClientRequestID)
		return
	}
	if req.WorktreeRef == "" {
		writeControlPlaneErr(http.StatusBadRequest, "E_INVALID_REQUEST", "worktree_ref is required", "", req.ClientRequestID)
		return
	}
	if req.Runner == "" {
		writeControlPlaneErr(http.StatusBadRequest, "E_INVALID_REQUEST", "runner is required", "", req.ClientRequestID)
		return
	}
	if req.Prompt == "" {
		writeControlPlaneErr(http.StatusBadRequest, string(errors.EPromptRequired), "prompt is required for headless invocation", "", req.ClientRequestID)
		return
	}

	// 2. Validate prompt size (max 256KB)
	if len(req.Prompt) > MaxPromptSize {
		writeControlPlaneErr(http.StatusBadRequest, string(errors.EPromptTooLarge),
			fmt.Sprintf("prompt exceeds maximum size of %d bytes (got %d)", MaxPromptSize, len(req.Prompt)),
			"reduce prompt size or split into smaller chunks", req.ClientRequestID)
		return
	}

	// 3. Validate + canonicalize runner.
	canonicalRunner, err := runners.Canonicalize(req.Runner)
	if err != nil {
		writeControlPlaneErr(http.StatusBadRequest, string(errors.ERunnerNotFound),
			err.Error(), "valid runners: "+strings.Join(runners.CanonicalIDs(), ", "), req.ClientRequestID)
		return
	}
	req.Runner = canonicalRunner

	// 4. Validate reserved flags in runner args (headless includes permission flags)
	if err := validateHeadlessRunnerArgs(req.Runner, req.RunnerArgs); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerArgConflict
		}
		hint := "remove reserved flags from runner_args"
		if code == errors.ERunnerNotFound {
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		}
		writeControlPlaneErr(http.StatusBadRequest, string(code),
			err.Error(), hint, req.ClientRequestID)
		return
	}

	// 5. Validate invocation name if provided
	if req.InvocationName != "" {
		if err := core.ValidateName(req.InvocationName); err != nil {
			writeControlPlaneErr(http.StatusBadRequest, string(errors.EInvalidName),
				"invalid invocation name: "+err.Error(),
				"names must be 2-40 chars, lowercase alphanumeric + hyphens", req.ClientRequestID)
			return
		}
	}

	// 6. Resolve repo_root to canonical absolute path
	repoRoot, err := filepath.Abs(req.RepoRoot)
	if err != nil {
		writeControlPlaneErr(http.StatusBadRequest, "E_INVALID_REQUEST",
			"failed to resolve repo_root: "+err.Error(), "", req.ClientRequestID)
		return
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		writeControlPlaneErr(http.StatusBadRequest, "E_INVALID_REQUEST",
			"failed to resolve repo_root symlinks: "+err.Error(), "", req.ClientRequestID)
		return
	}

	// 7. Recursion guard: reject if repo_root is inside agency-managed worktree
	if isInsideAgencyManagedWorktree(repoRoot, s.Store.DataDir) {
		writeControlPlaneErr(http.StatusBadRequest, string(errors.EUnsafeRepoRoot),
			"repo_root is inside an agency-managed worktree",
			"use the original repository, not a sandbox or integration worktree", req.ClientRequestID)
		return
	}

	// 8. Derive git root via git rev-parse --show-toplevel
	gitRoot, err := git.GetRepoRoot(ctx, s.Runner, repoRoot)
	if err != nil {
		writeControlPlaneErr(http.StatusBadRequest, string(errors.ENoRepo),
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
			s.writeControlPlaneSuccess(w, existingID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
			return
		}
		// If we can't read meta, fall through to create new (idempotency entry may be stale)
	}

	// 11. Self-register repo if needed
	if err := s.ensureRepoRegistered(repoIdentity, repoRoot); err != nil {
		writeControlPlaneErr(http.StatusInternalServerError, "E_INTERNAL",
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
		writeControlPlaneErr(http.StatusNotFound, string(code),
			err.Error(), "run 'agency worktree ls' to see available worktrees", req.ClientRequestID)
		return
	}

	if wtRecord.Broken || wtRecord.Meta == nil {
		writeControlPlaneErr(http.StatusBadRequest, string(errors.EWorktreeBroken),
			"integration worktree exists but meta.json is unreadable",
			"inspect or recreate the worktree", req.ClientRequestID)
		return
	}

	if wtRecord.Meta.State != store.WorktreeStatePresent {
		writeControlPlaneErr(http.StatusBadRequest, string(errors.EWorktreeNotFound),
			"integration worktree is archived", "use a present (non-archived) integration worktree", req.ClientRequestID)
		return
	}

	// 13. Check invocation name uniqueness if name provided
	if req.InvocationName != "" {
		if err := s.checkInvocationNameUniqueness(repoIdentity.RepoID, req.InvocationName); err != nil {
			writeControlPlaneErr(http.StatusConflict, string(errors.EInvocationNameExists),
				err.Error(), "use a different name or wait for the existing invocation to complete", req.ClientRequestID)
			return
		}
	}

	// 14. Create invocation and sandbox via invocation.Service.Create (unified path)
	invSvc := invocation.NewService(s.Store, s.Runner, s.FS, s.Clock)
	createResult, err := invSvc.Create(ctx, invocation.CreateOpts{
		IntegrationWorktreeID:   wtRecord.WorktreeID,
		IntegrationWorktreeMeta: wtRecord.Meta,
		RepoRoot:                repoRoot,
		RepoID:                  repoIdentity.RepoID,
		Runner:                  req.Runner,
		Mode:                    store.RunnerModeHeadless,
		InvocationName:          req.InvocationName,
		NoIncludeUntracked:      req.NoIncludeUntracked,
	})
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		writeControlPlaneErr(http.StatusInternalServerError, string(code),
			err.Error(), "", req.ClientRequestID)
		return
	}

	// 14a. Create logs directory (headless-specific, not handled by invocation.Service.Create)
	logsDir := s.Store.SandboxLogsDir(repoIdentity.RepoID, createResult.InvocationID)
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		s.cleanupFailedInvocation(ctx, repoIdentity.RepoID, createResult, repoRoot, "start_failed")
		writeControlPlaneErr(http.StatusInternalServerError, "E_INTERNAL",
			"failed to create logs directory: "+err.Error(), "", req.ClientRequestID)
		return
	}

	// 14b. Write prompt file (headless-specific)
	promptPath := s.Store.InvocationPromptPath(repoIdentity.RepoID, createResult.InvocationID)
	if err := os.WriteFile(promptPath, []byte(req.Prompt), 0o600); err != nil {
		s.cleanupFailedInvocation(ctx, repoIdentity.RepoID, createResult, repoRoot, "start_failed")
		writeControlPlaneErr(http.StatusInternalServerError, "E_INTERNAL",
			"failed to write prompt file: "+err.Error(), "", req.ClientRequestID)
		return
	}

	// 15. Start the runner process
	pid, pgid, err := s.startRunner(ctx, repoIdentity.RepoID, createResult, repoRoot, wtRecord.WorktreeID, req)
	if err != nil {
		// Cleanup on failure
		s.cleanupFailedInvocation(ctx, repoIdentity.RepoID, createResult, repoRoot, "spawn_failed")
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerStartFailed
		}
		writeControlPlaneErr(http.StatusInternalServerError, string(code),
			err.Error(), "", req.ClientRequestID)
		return
	}

	// 16. Record idempotency entry
	s.recordIdempotency(repoIdentity.RepoID, req.ClientRequestID, createResult.InvocationID)

	// 17. Update invocation meta with running state
	now := s.Clock().UTC().Format(time.RFC3339)
	daemonPID := os.Getpid()
	promptHash := sha256.Sum256([]byte(req.Prompt))
	promptSHA := hex.EncodeToString(promptHash[:])
	envKeys := sortedEnvKeys(req.Env)
	runnerArgs := append([]string(nil), req.RunnerArgs...)

	_ = s.Store.UpdateInvocationMeta(repoIdentity.RepoID, createResult.InvocationID, func(m *store.InvocationMeta) {
		m.Status = store.InvocationStatusRunning
		m.Runner = req.Runner
		m.PID = &pid
		m.PGID = &pgid
		m.DaemonPID = &daemonPID
		m.DaemonInstanceID = s.InstanceID
		m.ClaimedAt = now
		m.LifecycleOwner = "daemon"
		m.PromptPath = s.Store.InvocationPromptPath(repoIdentity.RepoID, createResult.InvocationID)
		m.PromptSHA256 = promptSHA
		m.RunnerArgs = runnerArgs
		m.CustomEnvKeys = envKeys
	})

	// 18. Read final meta and return success
	meta, _ := s.Store.ReadInvocationMeta(repoIdentity.RepoID, createResult.InvocationID)
	s.writeControlPlaneSuccess(w, createResult.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, false)
}

// writeControlPlaneError writes an error response for control plane endpoints.
func (s *Server) writeControlPlaneError(w http.ResponseWriter, status int, requestID, code, message, hint, clientRequestID string) {
	resp := ControlPlaneStartResponse{
		OK:              false,
		ErrorCode:       code,
		Message:         message,
		Hint:            hint,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: clientRequestID,
	}
	s.writeJSON(w, status, resp)
}

// writeControlPlaneSuccess writes a success response for control plane endpoints.
func (s *Server) writeControlPlaneSuccess(w http.ResponseWriter, invocationID string, meta *store.InvocationMeta, repoID, clientRequestID, requestID string, alreadyRunning bool) {
	resp := ControlPlaneStartResponse{
		OK:                    true,
		InvocationID:          invocationID,
		SandboxPath:           meta.SandboxPath,
		RepoID:                repoID,
		IntegrationWorktreeID: meta.IntegrationWorktreeID,
		AlreadyRunning:        alreadyRunning,
		RequestID:             requestID,
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

// validateRunnerArgs checks for universal reserved flags in runner args.
func validateRunnerArgs(runner string, args []string) error {
	return runners.ValidateArgs(runner, args)
}

// validateHeadlessRunnerArgs checks for all reserved flags including
// headless-only permission/approval flags that Agency controls.
func validateHeadlessRunnerArgs(runner string, args []string) error {
	return runners.ValidateHeadlessArgs(runner, args)
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

// startRunner starts the runner process and returns PID and PGID.
func (s *Server) startRunner(ctx context.Context, repoID string, result *invocation.CreateResult, repoRoot, integrationWorktreeID string, req ControlPlaneStartRequest) (int, int, error) {
	args, err := buildRunnerArgsWithSandbox(req.Runner, req.Prompt, result.SandboxPath, req.RunnerArgs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to build runner args: %w", err)
	}
	return s.startRunnerWithArgs(ctx, repoID, result, repoRoot, integrationWorktreeID, req, args, "")
}

func (s *Server) startRunnerResumeTurn(ctx context.Context, proc *SupervisedProcess, prompt string) (int, int, error) {
	req := ControlPlaneStartRequest{
		Runner:             proc.Runner,
		Prompt:             prompt,
		RunnerArgs:         append([]string(nil), proc.RunnerArgs...),
		Env:                copyStringMap(proc.Env),
		NoIncludeUntracked: proc.NoIncludeUntracked,
	}
	resumeSessionID := proc.GetResumeSessionID()
	args, err := runners.BuildResumeArgs(proc.Runner, prompt, resumeSessionID, req.RunnerArgs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to build runner resume args: %w", err)
	}
	return s.startRunnerWithArgs(
		ctx,
		proc.RepoID,
		&invocation.CreateResult{
			InvocationID: proc.InvocationID,
			SandboxPath:  proc.SandboxPath,
		},
		proc.RepoRoot,
		proc.IntegrationWorktreeID,
		req,
		args,
		resumeSessionID,
	)
}

func (s *Server) startRunnerWithArgs(ctx context.Context, repoID string, result *invocation.CreateResult, repoRoot, integrationWorktreeID string, req ControlPlaneStartRequest, args []string, resumeSessionID string) (int, int, error) {
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

	// Open log files
	rawLogPath := s.Store.SandboxRawLogPath(repoID, result.InvocationID)
	stderrLogPath := s.Store.SandboxStderrLogPath(repoID, result.InvocationID)
	streamLogPath := s.Store.SandboxStreamLogPath(repoID, result.InvocationID)

	rawFile, err := os.OpenFile(rawLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open raw log file: %w", err)
	}

	stderrFile, err := os.OpenFile(stderrLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		_ = rawFile.Close()
		return 0, 0, fmt.Errorf("failed to open stderr log file: %w", err)
	}

	// PR-07: Open stream.jsonl for normalized events
	streamFile, err := os.OpenFile(streamLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		_ = rawFile.Close()
		_ = stderrFile.Close()
		return 0, 0, fmt.Errorf("failed to open stream log file: %w", err)
	}

	// Set up deterministic environment overlays.
	envOverlay := nonInteractiveRunnerEnv()
	for k, v := range req.Env {
		envOverlay[k] = v
	}

	// Create stdin pipe + chat relay for runners that support stdin streaming input.
	stdinReader, chatRelay := createChatRelay(req.Runner)

	startedProc, err := exec.StartProcess(context.Background(), runnerCmd, args, exec.StartOpts{
		Dir:        result.SandboxPath,
		Env:        envOverlay,
		Stdin:      stdinReader,
		StdoutPipe: true,
		StderrPipe: true,
		Setpgid:    true,
	})
	// Close the parent's copy of the stdin read end — the child has inherited the FD.
	if stdinReader != nil {
		_ = stdinReader.Close()
	}
	if err != nil {
		if chatRelay != nil {
			_ = chatRelay.Close()
		}
		_ = rawFile.Close()
		_ = stderrFile.Close()
		_ = streamFile.Close()
		return 0, 0, fmt.Errorf("failed to start runner: %w", err)
	}
	if err := sendInitialPromptIfRequired(req.Runner, req.Prompt, chatRelay); err != nil {
		if chatRelay != nil {
			_ = chatRelay.Close()
		}
		if startedProc.PGID > 0 {
			_ = syscall.Kill(-startedProc.PGID, syscall.SIGKILL)
		}
		_ = rawFile.Close()
		_ = stderrFile.Close()
		_ = streamFile.Close()
		return 0, 0, fmt.Errorf("failed to deliver initial prompt: %w", err)
	}

	stdoutPipe := startedProc.StdoutPipe
	stderrPipe := startedProc.StderrPipe
	pid := startedProc.PID
	pgid := startedProc.PGID

	// PR-07/S3 PR-03: keep stream sequence monotonic across restart boundaries.
	initialSeq := loadMaxStreamSeq(streamLogPath)
	parser := stream.NewParser(result.InvocationID, req.Runner, s.Clock)
	parser.SetInitialSeq(initialSeq)

	// PR-08: Create checkpoint engine
	checkpointsDir := s.Store.SandboxDir(repoID, result.InvocationID)
	eventsPath := s.Store.InvocationEventsPath(repoID, result.InvocationID)

	cpConfig := checkpoint.DefaultConfig()
	cpConfig.IncludeUntracked = !req.NoIncludeUntracked
	if s.CheckpointDebounceOverride != nil {
		cpConfig.DebounceInterval = *s.CheckpointDebounceOverride
		cpConfig.DriftInterval = *s.CheckpointDebounceOverride
	}

	cpEngine := checkpoint.NewEngineWithWriter(
		result.InvocationID,
		repoID,
		result.SandboxPath,
		repoRoot,
		checkpointsDir,
		eventsPath,
		cpConfig,
		s.Runner,
		s.FS,
		s.Clock,
		s.InvocationEvents,
	)

	// Wire semantic trigger channel: parser notifies checkpoint engine on mutating tool completions.
	triggerCh := make(chan checkpoint.TriggerEvent, 32)
	cpEngine.SetTriggerChannel(triggerCh)
	parser.SetCheckpointNotify(func(n stream.CheckpointNotification) {
		trigger := checkpoint.TriggerEvent{
			Kind:      checkpoint.TriggerToolEnd,
			ToolName:  n.ToolName,
			ToolNames: n.ToolNames,
			Seq:       n.Seq,
		}
		select {
		case triggerCh <- trigger:
			return
		default:
		}

		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
		select {
		case triggerCh <- trigger:
		case <-timer.C:
			// Do not silently lose semantic checkpoint intent; emit a warning so
			// operators can diagnose backpressure and tune queue sizing.
			fmt.Fprintf(
				os.Stderr,
				"warning: checkpoint trigger queue full; dropped semantic trigger invocation=%s seq=%d tool=%s\n",
				result.InvocationID,
				n.Seq,
				n.ToolName,
			)
		}
	})

	// Create supervised process record
	proc := &SupervisedProcess{
		InvocationID:          result.InvocationID,
		RepoID:                repoID,
		IntegrationWorktreeID: integrationWorktreeID, // PR-06: track for worktree rm guard
		PID:                   pid,
		PGID:                  pgid,
		SandboxPath:           result.SandboxPath,
		RawLogFile:            rawLogPath,
		StderrFile:            stderrLogPath,
		StreamLogFile:         streamLogPath,
		Runner:                req.Runner,
		RepoRoot:              repoRoot, // PR-08: for checkpoint engine
		RunnerArgs:            append([]string(nil), req.RunnerArgs...),
		Env:                   copyStringMap(req.Env),
		NoIncludeUntracked:    req.NoIncludeUntracked,
		Parser:                parser,
		CheckpointEngine:      cpEngine,
		Relay:                 chatRelay,
		done:                  make(chan struct{}),
	}
	proc.SetResumeSessionID(resumeSessionID)

	// Track any session_start identifier emitted by the runner so future
	// resume turns can target the exact thread instead of relying on --last.
	parser.SetSessionStartNotify(func(n stream.SessionStartNotification) {
		proc.SetResumeSessionID(n.SessionID)
	})

	// Register the process
	s.mu.Lock()
	s.processes[result.InvocationID] = proc
	s.mu.Unlock()

	// PR-07: Start goroutine to stream and parse stdout
	proc.streamWg.Add(2)
	go s.streamAndParseOutput(proc, stdoutPipe, rawFile, streamFile)
	// Stderr is still streamed without parsing
	go s.streamOutput(proc, stderrPipe, stderrFile)

	// Start goroutine to wait for process exit
	go s.waitForExitWithFailureReason(proc, startedProc, rawFile, stderrFile, streamFile)

	// Start goroutines to periodically flush lastOutputAt and semantic status
	go s.runOutputFlushLoop(proc)
	go s.runSemanticStatusFlushLoop(proc)

	// PR-08: Start checkpoint engine goroutine
	go s.runCheckpointLoop(proc)

	return pid, pgid, nil
}

// buildRunnerArgsWithSandbox builds the command arguments for a runner, including sandbox path for codex.
func buildRunnerArgsWithSandbox(runner, prompt, sandboxPath string, extraArgs []string) ([]string, error) {
	return runners.BuildHeadlessArgs(runner, prompt, sandboxPath, extraArgs)
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// createChatRelay creates a stdin pipe and chat relay for stdin-capable runners,
// or a resume relay for session-resume runners.
// Returns (stdinReader, relay). stdinReader is nil for resume relays (stdin stays /dev/null).
//
// Uses os.Pipe() (not io.Pipe()) so that the *os.File read end is passed directly
// as the child's stdin FD. Go's exec package treats *os.File specially — it uses the
// descriptor without starting a copy goroutine, so cmd.Wait() does not block on stdin.
func createChatRelay(runner string) (*os.File, relay.ChatRelay) {
	mode, err := runners.ResolveChatMode(runner)
	if err != nil {
		return nil, nil
	}

	switch mode {
	case runners.ChatModeStdin:
		pr, pw, err := os.Pipe()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to create stdin pipe for chat relay: %v\n", err)
			return nil, nil
		}
		return pr, relay.NewStdinRelay(pw, runner)
	case runners.ChatModeResume:
		return nil, relay.NewResumeRelay(runner)
	default:
		return nil, nil
	}
}

func sendInitialPromptIfRequired(runner, prompt string, chatRelay relay.ChatRelay) error {
	if chatRelay == nil {
		return nil
	}
	mode, err := runners.ResolveInitialPromptMode(runner)
	if err != nil {
		return err
	}
	if mode != runners.InitialPromptStdin {
		return nil
	}
	return chatRelay.Send(context.Background(), prompt)
}

// buildRunnerArgsForHeaded builds interactive launch args for headed mode.
func buildRunnerArgsForHeaded(runner string, extraArgs []string) ([]string, error) {
	return runners.BuildHeadedArgs(runner, extraArgs)
}

// cleanupFailedInvocation cleans up after a failed invocation start.
func (s *Server) cleanupFailedInvocation(ctx context.Context, repoID string, result *invocation.CreateResult, repoRoot, failureReason string) {
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
func (s *Server) waitForExitWithFailureReason(proc *SupervisedProcess, startedProc *exec.StartedProcess, rawFile, stderrFile, streamFile *os.File) {
	defer func() { _ = rawFile.Close() }()
	defer func() { _ = stderrFile.Close() }()
	defer func() {
		if streamFile != nil {
			_ = streamFile.Close()
		}
	}()
	defer proc.CloseDone()
	defer func() {
		if proc.Relay != nil {
			_ = proc.Relay.Close()
		}
	}()

	// Wait for process to exit
	exitResult, waitErr := startedProc.WaitExit()

	// Wait for streaming goroutines to finish draining stdout/stderr pipes
	// to the log files before processing exit status. Without this, the meta
	// status can be set to terminal before all output is flushed, causing
	// readers to miss the final output.
	proc.streamWg.Wait()

	// Stop the parser
	if proc.Parser != nil {
		proc.Parser.Stop()
	}

	// Determine exit code and status
	exitCode := exitResult.ExitCode
	exitReason := "exited"
	failureReason := ""
	status := store.InvocationStatusFinished

	if waitErr != nil || exitResult.ExitCode != 0 || exitResult.Signal != "" {
		status = store.InvocationStatusFailed
		failureReason = "runner_exit_nonzero"
	}

	// Check if kill/stop set an override reason before the signal was sent.
	if override, ok := proc.exitReason.Load().(string); ok && override != "" {
		exitReason = override
		status = store.InvocationStatusFailed
	}
	if override, ok := proc.failureReason.Load().(string); ok && override != "" {
		failureReason = override
	}

	var queuedResumePrompts []string
	if resumeRelay, ok := proc.Relay.(*relay.ResumeRelay); ok {
		queuedResumePrompts = resumeRelay.Drain()
	}

	// PR-07: Get final semantic status
	var semanticStatus *string
	var semanticStatusUpdatedAt string
	if proc.Parser != nil {
		if s := proc.Parser.GetSemanticStatus(); s != nil {
			str := string(*s)
			semanticStatus = &str
			semanticStatusUpdatedAt = proc.Parser.GetSemanticStatusUpdatedAt().UTC().Format(time.RFC3339)
		}
		// Clear semantic status if invocation failed
		if status == store.InvocationStatusFailed {
			semanticStatus = nil
			semanticStatusUpdatedAt = ""
		}
	}

	// For resume-mode runners, a queued follow-up means "continue with next turn"
	// rather than terminating the invocation lifecycle.
	if status == store.InvocationStatusFinished && len(queuedResumePrompts) > 0 && runners.SupportsResumeTurns(proc.Runner) {
		pid, pgid, resumeErr := s.startRunnerResumeTurn(context.Background(), proc, queuedResumePrompts[0])
		if resumeErr != nil {
			status = store.InvocationStatusFailed
			failureReason = "runner_resume_failed"
		} else {
			if len(queuedResumePrompts) > 1 {
				s.enqueueFollowUpPrompts(proc.InvocationID, queuedResumePrompts[1:])
			}
			now := s.Clock().UTC().Format(time.RFC3339)
			daemonPID := os.Getpid()
			_ = s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
				meta.Status = store.InvocationStatusRunning
				meta.PID = &pid
				meta.PGID = &pgid
				meta.DaemonPID = &daemonPID
				meta.DaemonInstanceID = s.InstanceID
				meta.ClaimedAt = now
				meta.ExitReason = ""
				meta.FailureReason = ""
				meta.ExitCode = nil
				meta.FinishedAt = ""
				meta.LifecycleOwner = "daemon"
				meta.SemanticStatus = nil
				meta.SemanticStatusUpdatedAt = ""
				meta.Flags.NeedsAttention = false
			})
			return
		}
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
		// PR-07: Persist final semantic status
		if semanticStatus != nil {
			s := runnerstatus.Status(*semanticStatus)
			meta.SemanticStatus = &s
			meta.SemanticStatusUpdatedAt = semanticStatusUpdatedAt
		} else {
			meta.SemanticStatus = nil
			meta.SemanticStatusUpdatedAt = ""
		}
	})

	// Remove from supervised processes
	s.mu.Lock()
	if current, ok := s.processes[proc.InvocationID]; ok && current == proc {
		delete(s.processes, proc.InvocationID)
	}
	s.mu.Unlock()
}

func (s *Server) enqueueFollowUpPrompts(invocationID string, prompts []string) {
	if len(prompts) == 0 {
		return
	}
	s.mu.Lock()
	proc, ok := s.processes[invocationID]
	s.mu.Unlock()
	if !ok || proc == nil || proc.Relay == nil {
		return
	}
	for _, prompt := range prompts {
		if strings.TrimSpace(prompt) == "" {
			continue
		}
		_ = proc.Relay.Send(context.Background(), prompt)
	}
}

// ----- PR-10: Headed (tmux) Control Plane Handler -----

// handleControlPlaneStartHeaded handles POST /invocations/start_headed (PR-10 control plane).
// This endpoint creates and starts a headed (tmux) invocation end-to-end.
func (s *Server) handleControlPlaneStartHeaded(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)

	// Parse request body
	var req ControlPlaneStartHeadedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "", "", requestID)
		return
	}

	// 1. Validate required fields
	if req.ClientRequestID == "" {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "client_request_id is required", "provide a UUID for idempotency", "", requestID)
		return
	}
	if req.RepoRoot == "" {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "repo_root is required", "", req.ClientRequestID, requestID)
		return
	}
	if req.WorktreeRef == "" {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "worktree_ref is required", "", req.ClientRequestID, requestID)
		return
	}
	if req.Runner == "" {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "runner is required", "", req.ClientRequestID, requestID)
		return
	}

	// 2. Validate + canonicalize runner.
	canonicalRunner, err := runners.Canonicalize(req.Runner)
	if err != nil {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.ERunnerNotFound),
			err.Error(), "valid runners: "+strings.Join(runners.CanonicalIDs(), ", "), req.ClientRequestID, requestID)
		return
	}
	req.Runner = canonicalRunner

	// 2b. Validate reserved flags in runner args.
	if err := validateRunnerArgs(req.Runner, req.RunnerArgs); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerArgConflict
		}
		hint := "remove reserved flags from runner_args"
		if code == errors.ERunnerNotFound {
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		}
		s.writeHeadedError(w, http.StatusBadRequest, string(code),
			err.Error(), hint, req.ClientRequestID, requestID)
		return
	}

	// 2c. Resolve capability-defined headed launch args.
	headedRunnerArgs, err := buildRunnerArgsForHeaded(req.Runner, req.RunnerArgs)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}

		hint := ""
		switch code {
		case errors.ERunnerNotFound:
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		case errors.EInvocationInvalidMode:
			hint = "runner does not support headed mode"
		}

		status := http.StatusInternalServerError
		if code == errors.ERunnerNotFound || code == errors.EInvocationInvalidMode {
			status = http.StatusBadRequest
		}
		s.writeHeadedError(w, status, string(code), err.Error(), hint, req.ClientRequestID, requestID)
		return
	}

	// 3. Validate invocation name if provided
	if req.InvocationName != "" {
		if err := core.ValidateName(req.InvocationName); err != nil {
			s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvalidName),
				"invalid invocation name: "+err.Error(),
				"names must be 2-40 chars, lowercase alphanumeric + hyphens", req.ClientRequestID, requestID)
			return
		}
	}

	// 4. Resolve repo_root to canonical absolute path
	repoRoot, err := filepath.Abs(req.RepoRoot)
	if err != nil {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST",
			"failed to resolve repo_root: "+err.Error(), "", req.ClientRequestID, requestID)
		return
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST",
			"failed to resolve repo_root symlinks: "+err.Error(), "", req.ClientRequestID, requestID)
		return
	}

	// 5. Recursion guard: reject if repo_root is inside agency-managed worktree
	if isInsideAgencyManagedWorktree(repoRoot, s.Store.DataDir) {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EUnsafeRepoRoot),
			"repo_root is inside an agency-managed worktree",
			"use the original repository, not a sandbox or integration worktree", req.ClientRequestID, requestID)
		return
	}

	// 6. Derive git root via git rev-parse --show-toplevel
	gitRoot, err := git.GetRepoRoot(ctx, s.Runner, repoRoot)
	if err != nil {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.ENoRepo),
			"repo_root is not inside a git repository: "+err.Error(), "", req.ClientRequestID, requestID)
		return
	}
	repoRoot = gitRoot.Path // Use the actual git root

	// 7. Derive repo identity
	originInfo := git.GetOriginInfo(ctx, s.Runner, repoRoot)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot, originInfo.URL)

	// 8. Check idempotency before doing any work
	if entry, isDuplicate := s.checkHeadedIdempotency(repoIdentity.RepoID, req.ClientRequestID); isDuplicate {
		// Return the existing invocation
		meta, err := s.Store.ReadInvocationMeta(repoIdentity.RepoID, entry.InvocationID)
		if err == nil {
			s.writeHeadedSuccess(w, entry.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
			return
		}
		// If we can't read meta, fall through to create new (idempotency entry may be stale)
	}

	// 9. Self-register repo if needed
	if err := s.ensureRepoRegistered(repoIdentity, repoRoot); err != nil {
		s.writeHeadedError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to register repo: "+err.Error(), "", req.ClientRequestID, requestID)
		return
	}

	// 10. Resolve integration worktree
	wtSvc := integrationworktree.NewService(s.Store, s.Runner, s.FS, s.Clock)
	wtRecord, err := wtSvc.Resolve(repoIdentity.RepoID, req.WorktreeRef, false)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeHeadedError(w, http.StatusNotFound, string(code),
			err.Error(), "run 'agency worktree ls' to see available worktrees", req.ClientRequestID, requestID)
		return
	}

	if wtRecord.Broken || wtRecord.Meta == nil {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EWorktreeBroken),
			"integration worktree exists but meta.json is unreadable",
			"inspect or recreate the worktree", req.ClientRequestID, requestID)
		return
	}

	if wtRecord.Meta.State != store.WorktreeStatePresent {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EWorktreeNotFound),
			"integration worktree is archived", "use a present (non-archived) integration worktree", req.ClientRequestID, requestID)
		return
	}

	// 11. Check invocation name uniqueness if name provided
	if req.InvocationName != "" {
		if err := s.checkInvocationNameUniqueness(repoIdentity.RepoID, req.InvocationName); err != nil {
			s.writeHeadedError(w, http.StatusConflict, string(errors.EInvocationNameExists),
				err.Error(), "use a different name or wait for the existing invocation to complete", req.ClientRequestID, requestID)
			return
		}
	}

	// 12. Create invocation and sandbox via invocation.Service.Create
	// This is the unified path per PR-10 spec requirements
	invSvc := invocation.NewService(s.Store, s.Runner, s.FS, s.Clock)

	createResult, err := invSvc.Create(ctx, invocation.CreateOpts{
		IntegrationWorktreeID:   wtRecord.WorktreeID,
		IntegrationWorktreeMeta: wtRecord.Meta,
		RepoRoot:                repoRoot,
		RepoID:                  repoIdentity.RepoID,
		Runner:                  req.Runner,
		Mode:                    store.RunnerModeHeaded,
		InvocationName:          req.InvocationName,
		NoIncludeUntracked:      req.NoIncludeUntracked,
	})
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeHeadedError(w, http.StatusInternalServerError, string(code),
			err.Error(), "", req.ClientRequestID, requestID)
		return
	}

	// 13. Resolve runner command
	userCfg, err := s.LoadUserConfig()
	if err != nil {
		userCfg = config.UserConfig{}
	}

	runnerCmd, err := config.ResolveRunnerCmd(s.Runner, s.FS, s.ConfigDir, userCfg, req.Runner)
	if err != nil {
		s.markHeadedInvocationFailed(repoIdentity.RepoID, createResult.InvocationID, "start_failed")
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.ERunnerNotFound),
			"failed to resolve runner command: "+err.Error(),
			"ensure runner is installed and configured", req.ClientRequestID, requestID)
		return
	}

	// 14. Generate tmux session name
	sessionName := tmux.SessionName(createResult.InvocationID)

	// 15. Preflight: check if session already exists
	exists, err := s.TmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		// Non-fatal error checking, log but proceed
		fmt.Fprintf(os.Stderr, "warning: could not check for existing tmux session: %v\n", err)
	} else if exists {
		s.markHeadedInvocationFailed(repoIdentity.RepoID, createResult.InvocationID, "start_failed")
		s.writeHeadedError(w, http.StatusConflict, string(errors.ETmuxSessionExists),
			"tmux session already exists: "+sessionName,
			"a tmux session with this name already exists; kill it with 'tmux kill-session -t "+sessionName+"'",
			req.ClientRequestID, requestID)
		return
	}

	// 16. Create tmux session with CWD = sandbox tree, argv from capability plan.
	// Per spec: No shell wrapping, no prompt - headed mode gets prompt interactively
	argv := append([]string{runnerCmd}, headedRunnerArgs...)

	err = s.TmuxClient.NewSession(ctx, sessionName, createResult.SandboxPath, argv)
	if err != nil {
		s.markHeadedInvocationFailed(repoIdentity.RepoID, createResult.InvocationID, "start_failed")
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed),
			"failed to create tmux session: "+err.Error(),
			"ensure tmux is installed and working",
			req.ClientRequestID, requestID)
		return
	}

	// 17. Update invocation meta: status = "running", tmux_session set, daemon ownership
	now := s.Clock().UTC().Format(time.RFC3339)
	daemonPID := os.Getpid()
	runnerArgs := append([]string(nil), req.RunnerArgs...)
	err = s.Store.UpdateInvocationMeta(repoIdentity.RepoID, createResult.InvocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.TmuxSession = sessionName
		meta.DaemonPID = &daemonPID
		meta.DaemonInstanceID = s.InstanceID
		meta.ClaimedAt = now
		meta.LifecycleOwner = "daemon"
		meta.RunnerArgs = runnerArgs
	})
	if err != nil {
		// Best-effort: kill the session we just created
		_ = s.TmuxClient.KillSession(ctx, sessionName)
		s.writeHeadedError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to update invocation meta: "+err.Error(), "", req.ClientRequestID, requestID)
		return
	}

	// 18. Register headed process in supervised map
	proc := &SupervisedProcess{
		InvocationID:          createResult.InvocationID,
		RepoID:                repoIdentity.RepoID,
		IntegrationWorktreeID: wtRecord.WorktreeID,
		Mode:                  "headed",
		TmuxSession:           sessionName,
		Runner:                req.Runner,
		RepoRoot:              repoRoot,
		done:                  make(chan struct{}),
	}

	s.mu.Lock()
	s.processes[createResult.InvocationID] = proc
	s.mu.Unlock()

	// 19. Record idempotency entry
	s.recordHeadedIdempotency(repoIdentity.RepoID, req.ClientRequestID, createResult.InvocationID, sessionName, createResult.SandboxPath)

	// 20. Read final meta and return success
	meta, _ := s.Store.ReadInvocationMeta(repoIdentity.RepoID, createResult.InvocationID)
	s.writeHeadedSuccess(w, createResult.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, false)
}

// writeHeadedError writes an error response for headed control plane endpoints.
func (s *Server) writeHeadedError(w http.ResponseWriter, status int, code, message, hint, clientRequestID, requestID string) {
	resp := ControlPlaneStartHeadedResponse{
		OK:              false,
		ErrorCode:       code,
		Message:         message,
		Hint:            hint,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    version.FullVersion(),
		GitSHA:          version.Commit,
		ClientRequestID: clientRequestID,
	}
	s.writeJSON(w, status, resp)
}

// writeHeadedSuccess writes a success response for headed control plane endpoints.
func (s *Server) writeHeadedSuccess(w http.ResponseWriter, invocationID string, meta *store.InvocationMeta, repoID, clientRequestID, requestID string, alreadyRunning bool) {
	resp := ControlPlaneStartHeadedResponse{
		OK:                    true,
		InvocationID:          invocationID,
		SandboxPath:           meta.SandboxPath,
		RepoID:                repoID,
		IntegrationWorktreeID: meta.IntegrationWorktreeID,
		TmuxSession:           meta.TmuxSession,
		AlreadyRunning:        alreadyRunning,
		RequestID:             requestID,
		APIVersion:            APIVersion,
		BuildVersion:          version.FullVersion(),
		GitSHA:                version.Commit,
		ClientRequestID:       clientRequestID,
		DaemonInstanceID:      s.InstanceID,
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// markHeadedInvocationFailed marks a headed invocation as failed.
func (s *Server) markHeadedInvocationFailed(repoID, invocationID, exitReason string) {
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFailed
		meta.ExitReason = exitReason
		meta.FinishedAt = now
		meta.Flags.NeedsAttention = true
	})
}
