package daemon

import (
	"net/http"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/relay"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/NielsdaWheelz/agency/internal/version"
)

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request, invocationID string) {
	ctx := r.Context()
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	writeStopError := func(status int, code, message, hint string) {
		s.writeErrorWithRequestID(w, status, requestID, code, message, hint)
	}

	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		writeStopError(http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	meta, err := s.Store.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			writeStopError(http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		writeStopError(http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}
	if meta.Status == store.InvocationStatusFinished || meta.Status == store.InvocationStatusFailed {
		s.writeInvocationActionSuccess(w, requestID, invocationID)
		return
	}

	s.mu.RLock()
	completionProc, completionSupervised := s.processes[invocationID]
	s.mu.RUnlock()
	if completionSupervised && completionProc != nil && completionProc.SuccessfulCompletionObserved() {
		s.scheduleStdinCompletionFinalize(completionProc)
		s.writeInvocationActionSuccess(w, requestID, invocationID)
		return
	}

	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		if m.StopRequestedAt == "" {
			m.StopRequestedAt = now
		}
		m.Flags.NeedsAttention = true
	})

	if meta.Mode == store.RunnerModeHeaded {
		sessionName := meta.TmuxSession
		if sessionName == "" {
			sessionName = tmux.SessionName(invocationID)
		}

		exists, err := s.TmuxClient.HasSession(ctx, sessionName)
		if err != nil {
			s.recordInvocationWarning(repoID, invocationID, "stop_tmux_has_session_failed", err.Error(), map[string]any{
				"session_name": sessionName,
			})
		}
		if !exists {
			if meta.Status == store.InvocationStatusRunning {
				finishedAt := s.Clock().UTC().Format(time.RFC3339)
				_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
					m.Status = store.InvocationStatusFinished
					m.ExitReason = "exited"
					m.FinishedAt = finishedAt
				})
			}
			s.writeInvocationActionSuccess(w, requestID, invocationID)
			return
		}

		if err := s.TmuxClient.SendKeys(ctx, sessionName, []tmux.Key{tmux.KeyCtrlC}); err != nil {
			s.recordInvocationWarning(repoID, invocationID, "stop_tmux_send_ctrl_c_failed", err.Error(), map[string]any{
				"session_name": sessionName,
			})
		}

		s.writeInvocationActionSuccess(w, requestID, invocationID)
		return
	}

	pgid := safeIntPtr(meta.PGID)
	if pgid <= 0 {
		pgid = safeIntPtr(meta.PID)
	}
	if pgid <= 0 {
		writeStopError(http.StatusBadRequest, "E_INVALID_REQUEST", "no PGID available to signal", "invocation may not have started properly")
		return
	}

	s.mu.RLock()
	proc, supervised := s.processes[invocationID]
	s.mu.RUnlock()

	go s.stopEscalation(repoID, invocationID, pgid, supervised, proc)
	s.writeInvocationActionSuccess(w, requestID, invocationID)
}

func (s *Server) stopEscalation(repoID, invocationID string, pgid int, supervised bool, proc *SupervisedProcess) {
	if supervised && proc != nil && proc.SuccessfulCompletionObserved() {
		s.scheduleStdinCompletionFinalize(proc)
		return
	}
	if supervised && proc != nil {
		proc.exitReason.Store("stopped")
		proc.failureReason.Store("stopped")
	}

	if err := syscall.Kill(-pgid, syscall.SIGINT); err == syscall.ESRCH {
		return
	}
	if supervised && proc != nil {
		select {
		case <-proc.done:
			return
		case <-time.After(5 * time.Second):
		}
	} else {
		time.Sleep(5 * time.Second)
		if !s.PIDChecker(pgid) {
			return
		}
	}

	if err := syscall.Kill(-pgid, syscall.SIGTERM); err == syscall.ESRCH {
		return
	}
	if supervised && proc != nil {
		select {
		case <-proc.done:
			return
		case <-time.After(2 * time.Second):
		}
	} else {
		time.Sleep(2 * time.Second)
		if !s.PIDChecker(pgid) {
			return
		}
	}

	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	if supervised {
		return
	}

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

const (
	stdinCompletionSettleDelay = 500 * time.Millisecond
	stdinCompletionStopDelay   = 3 * time.Second
	stdinCompletionKillDelay   = 2 * time.Second
)

func (s *Server) handleSuccessfulFinalNotification(proc *SupervisedProcess, notification stream.FinalNotification) {
	if proc == nil || !notification.Success {
		return
	}
	_, _, completionSatisfied := proc.RecordSuccessfulFinalTurn()
	if completionSatisfied {
		s.scheduleStdinCompletionFinalize(proc)
	}
}

func (s *Server) scheduleStdinCompletionFinalize(proc *SupervisedProcess) {
	if proc == nil || proc.Relay == nil || proc.Relay.Mode() != relay.ModeStdin {
		return
	}
	if !proc.TryBeginCompletionFinalize() {
		return
	}

	go func() {
		defer proc.EndCompletionFinalize()

		timer := time.NewTimer(stdinCompletionSettleDelay)
		defer timer.Stop()

		select {
		case <-proc.done:
			return
		case <-s.shutdownCh:
			return
		case <-timer.C:
		}
		if !proc.SuccessfulCompletionObserved() {
			return
		}

		_ = proc.Relay.Close()

		select {
		case <-proc.done:
			return
		case <-s.shutdownCh:
			return
		case <-time.After(stdinCompletionStopDelay):
		}
		if !proc.SuccessfulCompletionObserved() {
			return
		}
		if proc.PGID > 0 {
			_ = syscall.Kill(-proc.PGID, syscall.SIGTERM)
		}

		select {
		case <-proc.done:
			return
		case <-s.shutdownCh:
			return
		case <-time.After(stdinCompletionKillDelay):
		}
		if !proc.SuccessfulCompletionObserved() {
			return
		}
		if proc.PGID > 0 {
			_ = syscall.Kill(-proc.PGID, syscall.SIGKILL)
		}
	}()
}

func (s *Server) handleKill(w http.ResponseWriter, r *http.Request, invocationID string) {
	ctx := r.Context()
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	writeKillError := func(status int, code, message, hint string) {
		s.writeErrorWithRequestID(w, status, requestID, code, message, hint)
	}

	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		writeKillError(http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	meta, err := s.Store.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			writeKillError(http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		writeKillError(http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}

	if meta.Mode == store.RunnerModeHeaded {
		sessionName := meta.TmuxSession
		if sessionName == "" {
			sessionName = tmux.SessionName(invocationID)
		}
			if err := s.TmuxClient.KillSession(ctx, sessionName); err != nil && !tmux.IsNoSessionErr(err) {
				s.recordInvocationWarning(repoID, invocationID, "kill_tmux_session_failed", err.Error(), map[string]any{
					"session_name": sessionName,
				})
			}

		now := s.Clock().UTC().Format(time.RFC3339)
		_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
			m.Status = store.InvocationStatusFailed
			m.ExitReason = "killed"
			m.FinishedAt = now
		})

		s.mu.Lock()
		if proc, ok := s.processes[invocationID]; ok {
			proc.CloseDone()
			delete(s.processes, invocationID)
		}
		s.mu.Unlock()

		s.writeInvocationActionSuccess(w, requestID, invocationID)
		return
	}

	pgid := safeIntPtr(meta.PGID)
	if pgid <= 0 {
		pgid = safeIntPtr(meta.PID)
	}

	s.mu.RLock()
	proc, supervised := s.processes[invocationID]
	s.mu.RUnlock()
	if supervised && proc != nil {
		proc.exitReason.Store("killed")
		proc.failureReason.Store("killed")
	}
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
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

	s.writeInvocationActionSuccess(w, requestID, invocationID)
}

func (s *Server) runOutputFlushLoop(proc *SupervisedProcess) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastFlushed int64
	for {
		select {
		case <-proc.done:
			if current := s.latestOutputAtUnixNano(proc); current > lastFlushed {
				s.flushLastOutputAt(proc)
			}
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			if current := s.latestOutputAtUnixNano(proc); current > lastFlushed {
				s.flushLastOutputAt(proc)
				lastFlushed = current
			}
		}
	}
}

func (s *Server) markInvocationFailed(repoID, invocationID, exitReason string) {
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFailed
		meta.ExitReason = exitReason
		meta.FinishedAt = now
		meta.Flags.NeedsAttention = true
	})
}

func (s *Server) writeInvocationActionSuccess(w http.ResponseWriter, requestID, invocationID string) {
	s.writeJSON(w, http.StatusOK, InvocationActionResponse{
		OK:           true,
		InvocationID: invocationID,
		RequestID:    requestID,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
	})
}
