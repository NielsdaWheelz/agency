package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/landing"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// handleLand handles POST /invocations/{ref}/land.
func (s *Server) handleLand(w http.ResponseWriter, r *http.Request, invocationID string) {
	requestID := prepareRequestID(w, r)

	// Read repo_id from query params
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeLandError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "repo_id query parameter is required", "", nil)
		return
	}

	// Parse request body
	var req LandRequest
	if err := decodeOptionalStrictJSON(r.Body, &req); err != nil {
		s.writeLandError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "", nil)
		return
	}

	mutation, ok := s.prepareLandingMutation(w, r, requestID, invocationID, repoID, "land", func(w http.ResponseWriter, status int, requestID, code, message, hint string) {
		s.writeLandError(w, status, requestID, code, message, hint, nil)
	})
	if !ok {
		return
	}
	defer func() { _ = mutation.unlock() }()

	meta, err := s.store.ReadInvocationMeta(mutation.record.RepoID, mutation.record.InvocationID)
	if err != nil {
		s.writeLandError(w, http.StatusInternalServerError, requestID, string(errors.ELandFailed), "failed to read invocation meta: "+err.Error(), "", nil)
		return
	}
	switch meta.Status {
	case store.InvocationStatusStarting, store.InvocationStatusRunning, store.InvocationStatusStopping:
		now := s.clock().UTC().Format(time.RFC3339)
		record := store.InvocationRecord{
			InvocationID: mutation.record.InvocationID,
			RepoID:       mutation.record.RepoID,
			Meta:         meta,
		}
		switch meta.Mode {
		case store.RunnerModeHeaded:
			s.reconcileHeadedInvocation(r.Context(), mutation.record.RepoID, record, now)
		case store.RunnerModeHeadless:
			s.reconcileHeadlessInvocation(mutation.record.RepoID, record, now)
		}
	}

	// Execute land
	result, err := mutation.service.Land(r.Context(), landing.LandOpts{
		RepoID:       mutation.record.RepoID,
		InvocationID: mutation.record.InvocationID,
		RepoRoot:     mutation.repoRoot,
		Env:          mutation.landEnv,
		Apply:        req.Apply,
		RequireBase:  req.RequireBase,
	})
	if err != nil {
		code := errors.CodeOr(err, errors.ELandFailed)

		httpStatus := http.StatusInternalServerError
		var conflictFiles []string
		hint := ""

		switch code {
		case errors.EInvocationStillRunning:
			httpStatus = http.StatusConflict
			hint = "stop the invocation first with 'agency agent <invocation-ref> stop' or 'agency agent <invocation-ref> kill'"
		case errors.ELandAlreadyLanded, errors.ELandAlreadyDiscarded:
			httpStatus = http.StatusConflict
		case errors.ELandConflict:
			httpStatus = http.StatusConflict
			hint = "resolve conflicts manually or inspect sandbox with 'agency agent <invocation-ref> open'"
			if ae, ok := errors.AsAgencyError(err); ok && ae.Details != nil {
				if filesJSON, ok := ae.Details["conflict_files"]; ok {
					_ = json.Unmarshal([]byte(filesJSON), &conflictFiles)
				}
			}
		case errors.ELandNothingToLand:
			httpStatus = http.StatusBadRequest
		case errors.ELandApplyRequired:
			httpStatus = http.StatusBadRequest
			hint = "run 'agency agent <invocation-ref> land --apply' to apply uncommitted changes"
		case errors.ESandboxMissing, errors.EIntegrationTreeMissing:
			httpStatus = http.StatusNotFound
		case errors.ERepoLocked:
			httpStatus = http.StatusConflict
		}

		s.writeLandError(w, httpStatus, requestID, string(code), err.Error(), hint, conflictFiles)
		return
	}

	s.writeLandSuccess(
		w,
		requestID,
		mutation.record.InvocationID,
		LandingMode(result.Mode),
		result.IntegrationHeadBefore,
		result.IntegrationHeadAfter,
		result.CommitsLanded,
	)
}

// handleDiscard handles POST /invocations/{ref}/discard.
func (s *Server) handleDiscard(w http.ResponseWriter, r *http.Request, invocationID string) {
	requestID := prepareRequestID(w, r)

	// Read repo_id from query params
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeDiscardError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "repo_id query parameter is required", "")
		return
	}

	// Parse request body (currently empty, but allow for future expansion)
	var req struct{}
	if err := decodeOptionalStrictJSON(r.Body, &req); err != nil {
		s.writeDiscardError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "")
		return
	}

	mutation, ok := s.prepareLandingMutation(w, r, requestID, invocationID, repoID, "discard", s.writeDiscardError)
	if !ok {
		return
	}
	defer func() { _ = mutation.unlock() }()

	// Execute discard with stop callback
	err := mutation.service.Discard(r.Context(), landing.DiscardOpts{
		RepoID:       mutation.record.RepoID,
		InvocationID: mutation.record.InvocationID,
		RepoRoot:     mutation.repoRoot,
		Env:          mutation.discardEnv,
		StopCallback: s.stopInvocationForDiscard,
	})
	if err != nil {
		code := errors.CodeOr(err, errors.ELandFailed)

		httpStatus := http.StatusInternalServerError
		switch code {
		case errors.ELandAlreadyLanded, errors.ELandAlreadyDiscarded:
			httpStatus = http.StatusConflict
		case errors.ERepoLocked:
			httpStatus = http.StatusConflict
		}

		s.writeDiscardError(w, httpStatus, requestID, string(code), err.Error(), "")
		return
	}

	s.writeDiscardSuccess(w, requestID, mutation.record.InvocationID)
}

type landingMutation struct {
	record     *resolvedInvocation
	repoRoot   string
	landEnv    map[string]string
	discardEnv map[string]string
	service    *landing.Service
	unlock     func() error
}

func (s *Server) prepareLandingMutation(w http.ResponseWriter, r *http.Request, requestID, invocationID, repoID, lockName string, writeError func(http.ResponseWriter, int, string, string, string, string)) (*landingMutation, bool) {
	record, resolveErr := s.resolveInvocationRef(invocationID, repoID)
	if resolveErr != nil {
		status, code := invocationResolveStatus(resolveErr)
		writeError(w, status, requestID, string(code), resolveErr.Error(), "use 'agency agent ls --repo <repo>' to list invocations")
		return nil, false
	}

	wtMeta, err := s.store.ReadIntegrationWorktreeMeta(record.RepoID, record.Meta.IntegrationWorktreeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, requestID, string(errors.ELandFailed), "failed to read integration worktree meta: "+err.Error(), "")
		return nil, false
	}
	profileEnv, err := s.executionProfileEnv(wtMeta.ExecutionProfile)
	if err != nil {
		code := errors.CodeOr(err, errors.EExecutionProfileNotFound)
		writeError(w, http.StatusBadRequest, requestID, string(code), apiErrorMessage(err), "")
		return nil, false
	}

	landEnv := prSyncNonInteractiveEnv(profileEnv)
	discardEnv := landEnv
	if record.Meta.ExecutionProfile != wtMeta.ExecutionProfile {
		invocationProfileEnv, err := s.executionProfileEnv(record.Meta.ExecutionProfile)
		if err != nil {
			code := errors.CodeOr(err, errors.EExecutionProfileNotFound)
			writeError(w, http.StatusBadRequest, requestID, string(code), apiErrorMessage(err), "")
			return nil, false
		}
		discardEnv = prSyncNonInteractiveEnv(invocationProfileEnv)
	}

	repoRoot, err := git.GetRepoRoot(r.Context(), s.runner, wtMeta.TreePath, landEnv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, requestID, string(errors.ELandFailed), "failed to get repo root: "+err.Error(), "")
		return nil, false
	}

	unlock, err := s.repoLock.Lock(record.RepoID, lockName)
	if err != nil {
		writeError(w, http.StatusConflict, requestID, string(errors.ERepoLocked), "repository is locked by another operation", "wait for the other operation to complete")
		return nil, false
	}

	return &landingMutation{
		record:     record,
		repoRoot:   repoRoot.Path,
		landEnv:    landEnv,
		discardEnv: discardEnv,
		service:    landing.NewService(s.store, s.runner, s.fsys, s.clock, s.invocationEvents),
		unlock:     unlock,
	}, true
}

// stopInvocationForDiscard stops a running invocation before discarding.
// This implements the stop -> wait 5s -> kill escalation.
func (s *Server) stopInvocationForDiscard(ctx context.Context, repoID, invocationID string) error {
	// Read invocation meta
	meta, err := s.store.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		return err
	}

	// Handle based on mode
	if meta.Mode == store.RunnerModeHeadless {
		// Get PGID
		pgid := 0
		if meta.PGID != nil {
			pgid = *meta.PGID
		} else if meta.PID != nil {
			pgid = *meta.PID
		}

		if pgid <= 0 {
			return nil // No process to stop
		}

		// Check if we're supervising this process
		s.mu.RLock()
		proc, supervised := s.processes[invocationID]
		s.mu.RUnlock()

		if supervised && proc != nil {
			proc.exitReason.Store(store.ExitReasonDiscarded)
			proc.failureReason.Store("discarded")
		}

		// SIGINT
		err := syscall.Kill(-pgid, syscall.SIGINT)
		if err == syscall.ESRCH {
			return nil // Already gone
		}
		if err != nil {
			return fmt.Errorf("send SIGINT to runner process group %d: %w", pgid, err)
		}

		// Wait 5s
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if supervised && proc != nil {
			select {
			case <-proc.done:
				return nil
			case <-waitCtx.Done():
			}
		} else {
			<-waitCtx.Done()
			if !isProcessGroupAlive(pgid) {
				return nil
			}
		}

		// SIGKILL
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return fmt.Errorf("send SIGKILL to runner process group %d: %w", pgid, err)
		}

		// Update meta if not supervised
		if !supervised {
			now := s.clock().UTC().Format(time.RFC3339)
			if _, err := s.store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
				m.Status = store.InvocationStatusFailed
				m.ExitReason = store.ExitReasonDiscarded
				m.FailureReason = "discarded"
				m.FinishedAt = now
				m.PID = nil
				m.LifecycleOwner = ""
			}); err != nil {
				return fmt.Errorf("update discarded invocation metadata: %w", err)
			}
		}

		return nil
	}

	if meta.Mode == store.RunnerModeHeaded {
		sessionName, ok := headedInvocationSessionName(meta)
		if !ok {
			return errors.New(errors.ETmuxSessionMissing, "headed invocation is missing tmux_session")
		}
		if err := s.tmuxClient.KillSession(ctx, sessionName); err != nil && !tmux.IsNoSessionErr(err) {
			return fmt.Errorf("kill tmux session %s: %w", sessionName, err)
		}
	}
	return nil
}
