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
)

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request, invocationID string) {
	ctx := r.Context()
	requestID := prepareRequestID(w, r)

	var req struct{}
	if err := decodeOptionalStrictJSON(r.Body, &req); err != nil {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "")
		return
	}

	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "repo_id query parameter is required", "")
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationID, repoID)
	if resolveErr != nil {
		status, code := invocationResolveStatus(resolveErr)
		s.writeErrorWithRequestID(w, status, requestID, string(code), resolveErr.Error(), "use 'agency agent ls --repo <repo>' to list invocations")
		return
	}

	meta, err := s.store.ReadInvocationMeta(record.RepoID, record.InvocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.writeErrorWithRequestID(w, http.StatusNotFound, requestID, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to read invocation meta: "+err.Error(), "")
		return
	}
	if meta.Status == store.InvocationStatusFinished || meta.Status == store.InvocationStatusFailed {
		s.writeInvocationActionSuccess(w, requestID, record.InvocationID)
		return
	}

	s.mu.RLock()
	completionProc, completionSupervised := s.processes[record.InvocationID]
	s.mu.RUnlock()
	if completionSupervised && completionProc != nil && completionProc.successfulCompletionObserved() {
		s.scheduleStdinCompletionFinalize(completionProc)
		s.writeInvocationActionSuccess(w, requestID, record.InvocationID)
		return
	}

	if meta.Mode == store.RunnerModeHeaded {
		s.requestInvocationStop(record.RepoID, record.InvocationID)

		sessionName, ok := headedInvocationSessionName(meta)
		if !ok {
			s.failInvocationStart(record.RepoID, record.InvocationID, "tmux_session_missing", false)
			s.clearInvocationProcess(record.InvocationID)
			s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.ETmuxSessionMissing), "headed invocation is missing tmux_session", "recreate the headed session or inspect invocation metadata")
			return
		}

		exists, err := s.tmuxClient.HasSession(ctx, sessionName)
		if err != nil {
			s.recordInvocationWarning(record.RepoID, record.InvocationID, "stop_tmux_has_session_failed", err.Error(), map[string]any{
				"session_name": sessionName,
			})
		}
		if !exists {
			s.failInvocationStopped(record.RepoID, record.InvocationID, "stopped")
			s.clearInvocationProcess(record.InvocationID)
			s.writeInvocationActionSuccess(w, requestID, record.InvocationID)
			return
		}

		if err := s.tmuxClient.InterruptSession(ctx, sessionName); err != nil {
			s.recordInvocationWarning(record.RepoID, record.InvocationID, "stop_tmux_interrupt_failed", err.Error(), map[string]any{
				"session_name": sessionName,
			})

			exists, checkErr := s.tmuxClient.HasSession(ctx, sessionName)
			if checkErr != nil {
				s.recordInvocationWarning(record.RepoID, record.InvocationID, "stop_tmux_has_session_failed_after_interrupt", checkErr.Error(), map[string]any{
					"session_name": sessionName,
				})
			}
			if !exists {
				s.failInvocationStopped(record.RepoID, record.InvocationID, "stopped")
				s.clearInvocationProcess(record.InvocationID)
			}
		}

		s.writeInvocationActionSuccess(w, requestID, record.InvocationID)
		return
	}

	pgid := safeIntPtr(meta.PGID)
	if pgid <= 0 {
		pgid = safeIntPtr(meta.PID)
	}
	if pgid <= 0 {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "no PGID available to signal", "invocation may not have started properly")
		return
	}

	s.requestInvocationStop(record.RepoID, record.InvocationID)

	s.mu.RLock()
	proc, supervised := s.processes[record.InvocationID]
	s.mu.RUnlock()

	go s.stopEscalation(record.RepoID, record.InvocationID, pgid, supervised, proc)
	s.writeInvocationActionSuccess(w, requestID, record.InvocationID)
}

func (s *Server) stopEscalation(repoID, invocationID string, pgid int, supervised bool, proc *supervisedProcess) {
	if supervised && proc != nil && proc.successfulCompletionObserved() {
		s.scheduleStdinCompletionFinalize(proc)
		return
	}
	if supervised && proc != nil {
		proc.exitReason.Store(store.ExitReasonStopped)
		proc.failureReason.Store("stopped")
	}

	if err := syscall.Kill(-pgid, syscall.SIGINT); err == syscall.ESRCH {
		if !supervised {
			s.failInvocationStopped(repoID, invocationID, "stopped")
		}
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
		if !isProcessGroupAlive(pgid) {
			s.failInvocationStopped(repoID, invocationID, "stopped")
			return
		}
	}

	if err := syscall.Kill(-pgid, syscall.SIGTERM); err == syscall.ESRCH {
		if !supervised {
			s.failInvocationStopped(repoID, invocationID, "stopped")
		}
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
		if !isProcessGroupAlive(pgid) {
			s.failInvocationStopped(repoID, invocationID, "stopped")
			return
		}
	}

	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	if supervised {
		return
	}

	s.failInvocationStoppedWithKill(repoID, invocationID)
}

func isProcessGroupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || err == syscall.EPERM
}

const (
	stdinCompletionSettleDelay = 500 * time.Millisecond
	stdinCompletionStopDelay   = 3 * time.Second
	stdinCompletionKillDelay   = 2 * time.Second
)

func (s *Server) handleSuccessfulFinalNotification(proc *supervisedProcess, notification stream.FinalNotification) {
	if proc == nil || !notification.Success {
		return
	}
	_, _, completionSatisfied := proc.recordSuccessfulFinalTurn()
	if completionSatisfied {
		s.scheduleStdinCompletionFinalize(proc)
	}
}

func (s *Server) scheduleStdinCompletionFinalize(proc *supervisedProcess) {
	if proc == nil || proc.relay == nil || proc.relay.Mode() != relay.ModeStdin {
		return
	}
	if !proc.tryBeginCompletionFinalize() {
		return
	}

	go func() {
		defer proc.endCompletionFinalize()

		timer := time.NewTimer(stdinCompletionSettleDelay)
		defer timer.Stop()

		select {
		case <-proc.done:
			return
		case <-s.shutdownCh:
			return
		case <-timer.C:
		}
		if !proc.successfulCompletionObserved() {
			return
		}

		_ = proc.relay.Close()

		select {
		case <-proc.done:
			return
		case <-s.shutdownCh:
			return
		case <-time.After(stdinCompletionStopDelay):
		}
		if !proc.successfulCompletionObserved() {
			return
		}
		if proc.pgid > 0 {
			_ = syscall.Kill(-proc.pgid, syscall.SIGTERM)
		}

		select {
		case <-proc.done:
			return
		case <-s.shutdownCh:
			return
		case <-time.After(stdinCompletionKillDelay):
		}
		if !proc.successfulCompletionObserved() {
			return
		}
		if proc.pgid > 0 {
			_ = syscall.Kill(-proc.pgid, syscall.SIGKILL)
		}
	}()
}

func (s *Server) handleKill(w http.ResponseWriter, r *http.Request, invocationID string) {
	ctx := r.Context()
	requestID := prepareRequestID(w, r)
	writeKillError := func(status int, code, message, hint string) {
		s.writeErrorWithRequestID(w, status, requestID, code, message, hint)
	}

	var req struct{}
	if err := decodeOptionalStrictJSON(r.Body, &req); err != nil {
		writeKillError(http.StatusBadRequest, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "")
		return
	}

	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		writeKillError(http.StatusBadRequest, string(errors.EInvalidRequest), "repo_id query parameter is required", "")
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationID, repoID)
	if resolveErr != nil {
		status, code := invocationResolveStatus(resolveErr)
		writeKillError(status, string(code), resolveErr.Error(), "use 'agency agent ls --repo <repo>' to list invocations")
		return
	}

	meta, err := s.store.ReadInvocationMeta(record.RepoID, record.InvocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			writeKillError(http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		writeKillError(http.StatusInternalServerError, string(errors.EInternal), "failed to read invocation meta: "+err.Error(), "")
		return
	}

	if meta.Mode == store.RunnerModeHeaded {
		sessionName, ok := headedInvocationSessionName(meta)
		if !ok {
			s.failInvocationStart(record.RepoID, record.InvocationID, "tmux_session_missing", false)
			s.clearInvocationProcess(record.InvocationID)
			writeKillError(http.StatusInternalServerError, string(errors.ETmuxSessionMissing), "headed invocation is missing tmux_session", "recreate the headed session or inspect invocation metadata")
			return
		}
		if err := s.tmuxClient.KillSession(ctx, sessionName); err != nil && !tmux.IsNoSessionErr(err) {
			s.recordInvocationWarning(record.RepoID, record.InvocationID, "kill_tmux_session_failed", err.Error(), map[string]any{
				"session_name": sessionName,
			})
		}

		s.failInvocationKilled(record.RepoID, record.InvocationID)
		s.clearInvocationProcess(record.InvocationID)

		s.writeInvocationActionSuccess(w, requestID, record.InvocationID)
		return
	}

	pgid := safeIntPtr(meta.PGID)
	if pgid <= 0 {
		pgid = safeIntPtr(meta.PID)
	}

	s.mu.RLock()
	proc, supervised := s.processes[record.InvocationID]
	s.mu.RUnlock()
	if supervised && proc != nil {
		proc.exitReason.Store(store.ExitReasonKilled)
		proc.failureReason.Store("killed")
	}
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	if !supervised {
		s.failInvocationKilled(record.RepoID, record.InvocationID)
	}

	s.writeInvocationActionSuccess(w, requestID, record.InvocationID)
}

func (s *Server) runOutputFlushLoop(proc *supervisedProcess) {
	defer s.supervisionWg.Done()

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
