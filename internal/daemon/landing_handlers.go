package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/landing"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// handleLand handles POST /invocations/{id}/land.
func (s *Server) handleLand(w http.ResponseWriter, r *http.Request, invocationID string) {
	// Read repo_id from query params
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeLandError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id query parameter is required", "", nil)
		return
	}

	// Parse request body
	var req LandRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeLandError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "", nil)
			return
		}
	}

	// Read invocation meta
	meta, err := s.Store.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.writeLandError(w, http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "", nil)
			return
		}
		s.writeLandError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "", nil)
		return
	}

	// Get repo root from integration worktree meta
	wtMeta, err := s.Store.ReadIntegrationWorktreeMeta(repoID, meta.IntegrationWorktreeID)
	if err != nil {
		s.writeLandError(w, http.StatusInternalServerError, string(errors.ELandFailed),
			"failed to read integration worktree meta: "+err.Error(), "", nil)
		return
	}

	// Derive repo root from the integration tree path
	repoRoot, err := git.GetRepoRoot(r.Context(), s.Runner, wtMeta.TreePath)
	if err != nil {
		s.writeLandError(w, http.StatusInternalServerError, string(errors.ELandFailed),
			"failed to get repo root: "+err.Error(), "", nil)
		return
	}

	// Acquire repo lock
	unlock, err := s.repoLock.Lock(repoID, "land")
	if err != nil {
		s.writeLandError(w, http.StatusConflict, string(errors.ERepoLocked),
			"repository is locked by another operation",
			"wait for the other operation to complete", nil)
		return
	}
	defer func() { _ = unlock() }()

	// Create landing service
	landingSvc := landing.NewService(s.Store, s.Runner, s.FS, s.Clock)

	// Execute land
	result, err := landingSvc.Land(r.Context(), landing.LandOpts{
		RepoID:       repoID,
		InvocationID: invocationID,
		RepoRoot:     repoRoot.Path,
		Apply:        req.Apply,
		RequireBase:  req.RequireBase,
	})
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ELandFailed
		}

		httpStatus := http.StatusInternalServerError
		var conflictFiles []string
		hint := ""

		switch code {
		case errors.EInvocationStillRunning:
			httpStatus = http.StatusConflict
			hint = "stop the invocation first with 'agency agent stop' or 'agency agent kill'"
		case errors.ELandAlreadyLanded, errors.ELandAlreadyDiscarded:
			httpStatus = http.StatusConflict
		case errors.ELandConflict:
			httpStatus = http.StatusConflict
			hint = "resolve conflicts manually or inspect sandbox with 'agency agent open'"
			if ae, ok := errors.AsAgencyError(err); ok && ae.Details != nil {
				if filesJSON, ok := ae.Details["conflict_files"]; ok {
					_ = json.Unmarshal([]byte(filesJSON), &conflictFiles)
				}
			}
		case errors.ELandNothingToLand:
			httpStatus = http.StatusBadRequest
		case errors.ELandApplyRequired:
			httpStatus = http.StatusBadRequest
			hint = "run 'agency agent land --apply' to apply uncommitted changes"
		case errors.ESandboxMissing, errors.EIntegrationTreeMissing:
			httpStatus = http.StatusNotFound
		case errors.ERepoLocked:
			httpStatus = http.StatusConflict
		}

		s.writeLandError(w, httpStatus, string(code), err.Error(), hint, conflictFiles)
		return
	}

	// Success response
	resp := LandResponse{
		OK:                    true,
		APIVersion:            APIVersion,
		BuildVersion:          version.FullVersion(),
		InvocationID:          invocationID,
		AppliedMode:           LandingMode(result.Mode),
		IntegrationHeadBefore: result.IntegrationHeadBefore,
		IntegrationHeadAfter:  result.IntegrationHeadAfter,
		CommitsLanded:         result.CommitsLanded,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// handleDiscard handles POST /invocations/{id}/discard.
func (s *Server) handleDiscard(w http.ResponseWriter, r *http.Request, invocationID string) {
	// Read repo_id from query params
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeDiscardError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	// Parse request body (currently empty, but allow for future expansion)
	if r.ContentLength > 0 {
		var req DiscardRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeDiscardError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "")
			return
		}
	}

	// Read invocation meta
	meta, err := s.Store.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.writeDiscardError(w, http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		s.writeDiscardError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}

	// Get repo root from integration worktree meta
	wtMeta, err := s.Store.ReadIntegrationWorktreeMeta(repoID, meta.IntegrationWorktreeID)
	if err != nil {
		s.writeDiscardError(w, http.StatusInternalServerError, string(errors.ELandFailed),
			"failed to read integration worktree meta: "+err.Error(), "")
		return
	}

	// Derive repo root from the integration tree path
	repoRoot, err := git.GetRepoRoot(r.Context(), s.Runner, wtMeta.TreePath)
	if err != nil {
		s.writeDiscardError(w, http.StatusInternalServerError, string(errors.ELandFailed),
			"failed to get repo root: "+err.Error(), "")
		return
	}

	// Acquire repo lock
	unlock, err := s.repoLock.Lock(repoID, "discard")
	if err != nil {
		s.writeDiscardError(w, http.StatusConflict, string(errors.ERepoLocked),
			"repository is locked by another operation",
			"wait for the other operation to complete")
		return
	}
	defer func() { _ = unlock() }()

	// Create landing service
	landingSvc := landing.NewService(s.Store, s.Runner, s.FS, s.Clock)

	// Execute discard with stop callback
	err = landingSvc.Discard(r.Context(), landing.DiscardOpts{
		RepoID:       repoID,
		InvocationID: invocationID,
		RepoRoot:     repoRoot.Path,
		StopCallback: s.stopInvocationForDiscard,
	})
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ELandFailed
		}

		httpStatus := http.StatusInternalServerError
		switch code {
		case errors.ELandAlreadyLanded, errors.ELandAlreadyDiscarded:
			httpStatus = http.StatusConflict
		case errors.ERepoLocked:
			httpStatus = http.StatusConflict
		}

		s.writeDiscardError(w, httpStatus, string(code), err.Error(), "")
		return
	}

	// Success response
	resp := DiscardResponse{
		OK:           true,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		InvocationID: invocationID,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// stopInvocationForDiscard stops a running invocation before discarding.
// This implements the stop -> wait 5s -> kill escalation.
func (s *Server) stopInvocationForDiscard(ctx context.Context, repoID, invocationID string) error {
	// Read invocation meta
	meta, err := s.Store.ReadInvocationMeta(repoID, invocationID)
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
			proc.exitReason.Store("discarded")
			proc.failureReason.Store("discarded")
		}

		// SIGINT
		err := syscall.Kill(-pgid, syscall.SIGINT)
		if err == syscall.ESRCH {
			return nil // Already gone
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
			if !s.PIDChecker(pgid) {
				return nil
			}
		}

		// SIGKILL
		_ = syscall.Kill(-pgid, syscall.SIGKILL)

		// Update meta if not supervised
		if !supervised {
			now := s.Clock().UTC().Format(time.RFC3339)
			_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
				m.Status = store.InvocationStatusFailed
				m.ExitReason = "discarded"
				m.FailureReason = "discarded"
				m.FinishedAt = now
				m.PID = nil
				m.LifecycleOwner = ""
			})
		}

		return nil
	}

	// For headed mode, we would use tmux kill-session
	// This is handled elsewhere - headed invocations are managed by CLI
	return nil
}

// writeLandError writes an error response for land endpoints.
func (s *Server) writeLandError(w http.ResponseWriter, status int, code, message, hint string, conflictFiles []string) {
	resp := LandResponse{
		OK:            false,
		APIVersion:    APIVersion,
		BuildVersion:  version.FullVersion(),
		ErrorCode:     code,
		Message:       message,
		Hint:          hint,
		ConflictFiles: conflictFiles,
	}
	s.writeJSON(w, status, resp)
}

// writeDiscardError writes an error response for discard endpoints.
func (s *Server) writeDiscardError(w http.ResponseWriter, status int, code, message, hint string) {
	resp := DiscardResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	}
	s.writeJSON(w, status, resp)
}
