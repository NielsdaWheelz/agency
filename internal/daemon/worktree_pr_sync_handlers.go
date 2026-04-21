package daemon

import (
	"context"
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// handleWorktreePRSync handles POST /worktrees/{ref}/pr/sync.
func (s *Server) handleWorktreePRSync(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeWorktreePRSyncError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	req, decodeErr := decodePRSyncRequest(r.Body)
	if decodeErr != "" {
		s.writeWorktreePRSyncError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidArgument),
			decodeErr,
			"",
		)
		return
	}
	record, err := s.resolveWorktreeRefForRepo(worktreeRef, repoID)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		status := prSyncHTTPStatusForCode(code)
		s.writeWorktreePRSyncError(w, status, requestID, string(code), err.Error(), "use 'agency worktree ls' to list worktrees")
		return
	}
	if record == nil || record.Broken || record.Meta == nil {
		s.writeWorktreePRSyncError(w, http.StatusBadRequest, requestID, string(errors.EWorktreeBroken), "integration worktree exists but meta.json is unreadable", "inspect or recreate the worktree")
		return
	}
	if record.Meta.State != store.WorktreeStatePresent {
		s.writeWorktreePRSyncError(w, http.StatusNotFound, requestID, string(errors.EWorktreeNotFound), "integration worktree is archived", "use a present (non-archived) integration worktree")
		return
	}

	unlock, err := s.repoLock.Lock(record.RepoID, "worktree_pr_sync")
	if err != nil {
		s.writeWorktreePRSyncError(
			w,
			http.StatusConflict,
			requestID,
			string(errors.ERepoLocked),
			"repository is locked by another operation",
			"wait for the other operation to complete",
		)
		return
	}
	defer func() { _ = unlock() }()

	if err := s.ensureWorktreeMergeInactive(record.RepoID, record.WorktreeID, "run pr sync"); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EWorktreeMergeActive
		}
		s.writeWorktreePRSyncError(w, prSyncHTTPStatusForCode(code), requestID, string(code), err.Error(), mergeHintFromError(err))
		return
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, prSyncEventStarted, map[string]any{
		"allow_dirty":      req.AllowDirty,
		"force_with_lease": req.ForceWithLease,
		"branch":           record.Meta.Branch,
	}); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EPersistFailed
		}
		s.writeWorktreePRSyncError(w, prSyncHTTPStatusForCode(code), requestID, string(code), err.Error(), "")
		return
	}

	result, err := s.runWorktreePRSync(r.Context(), record, req)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		hint := prSyncHintFromError(err)

		if appendErr := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, prSyncEventFailed, map[string]any{
			"error_code": string(code),
			"message":    err.Error(),
		}); appendErr != nil {
			appendCode := errors.GetCode(appendErr)
			if appendCode == "" {
				appendCode = errors.EPersistFailed
			}
			s.writeWorktreePRSyncError(w, prSyncHTTPStatusForCode(appendCode), requestID, string(appendCode), appendErr.Error(), "")
			return
		}

		s.writeWorktreePRSyncError(w, prSyncHTTPStatusForCode(code), requestID, string(code), err.Error(), hint)
		return
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, prSyncEventSucceeded, map[string]any{
		"branch":    result.Branch,
		"pr_number": result.PRNumber,
		"pr_url":    result.PRURL,
		"pr_action": result.PRAction,
	}); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EPersistFailed
		}
		s.writeWorktreePRSyncError(w, prSyncHTTPStatusForCode(code), requestID, string(code), err.Error(), "")
		return
	}

	resp := WorktreePRSyncResponse{
		OK:                    true,
		APIVersion:            APIVersion,
		BuildVersion:          version.FullVersion(),
		RequestID:             requestID,
		RepoID:                record.RepoID,
		IntegrationWorktreeID: record.WorktreeID,
		Branch:                result.Branch,
		PRNumber:              result.PRNumber,
		PRURL:                 result.PRURL,
		PRAction:              result.PRAction,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) runWorktreePRSync(
	ctx context.Context,
	record *store.IntegrationWorktreeRecord,
	req WorktreePRSyncRequest,
) (*prSyncResult, error) {
	if record == nil || record.Meta == nil {
		return nil, errors.New(errors.EInternal, "worktree metadata missing")
	}
	pseudoInvocation := &resolvedInvocation{
		InvocationID: record.WorktreeID,
		RepoID:       record.RepoID,
		Meta: &store.InvocationMeta{
			Mode: store.RunnerModeHeaded,
		},
	}
	return s.runPRSync(ctx, pseudoInvocation, record.Meta, WorktreePRSyncRequest{
		AllowDirty:     req.AllowDirty,
		ForceWithLease: req.ForceWithLease,
	})
}
