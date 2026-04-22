package daemon

import (
	"context"
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// handleWorktreePRSync handles POST /worktrees/{ref}/pr/sync.
func (s *Server) handleWorktreePRSync(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := prepareRequestID(w, r)
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
		s.writeWorktreePRSyncError(w, status, requestID, string(code), apiErrorMessage(err), "use 'agency worktree ls' to list worktrees")
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

	result, err := s.executeWorktreePRSync(r.Context(), record, req)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeWorktreePRSyncError(w, prSyncHTTPStatusForCode(code), requestID, string(code), apiErrorMessage(err), prSyncHintFromError(err))
		return
	}
	s.writeWorktreePRSyncSuccess(w, requestID, record, result)
}

func (s *Server) executeWorktreePRSync(
	ctx context.Context,
	record *store.IntegrationWorktreeRecord,
	req WorktreePRSyncRequest,
) (*prSyncResult, error) {
	if record == nil || record.Meta == nil {
		return nil, errors.New(errors.EInternal, "worktree metadata missing")
	}

	unlock, err := s.repoLock.Lock(record.RepoID, "worktree_pr_sync")
	if err != nil {
		return nil, errors.NewWithDetails(
			errors.ERepoLocked,
			"repository is locked by another operation",
			map[string]string{"hint": "wait for the other operation to complete"},
		)
	}
	defer func() { _ = unlock() }()

	if err := s.ensureWorktreeMergeInactive(record.RepoID, record.WorktreeID, "run pr sync"); err != nil {
		return nil, err
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, prSyncEventStarted, map[string]any{
		"allow_dirty":      req.AllowDirty,
		"force_with_lease": req.ForceWithLease,
		"branch":           record.Meta.Branch,
	}); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			return nil, errors.New(errors.EPersistFailed, err.Error())
		}
		return nil, err
	}

	result, err := s.performWorktreePRSync(ctx, record, req)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		if appendErr := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, prSyncEventFailed, map[string]any{
			"error_code": string(code),
			"message":    apiErrorMessage(err),
		}); appendErr != nil {
			appendCode := errors.GetCode(appendErr)
			if appendCode == "" {
				return nil, errors.New(errors.EPersistFailed, appendErr.Error())
			}
			return nil, appendErr
		}
		return nil, err
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, prSyncEventSucceeded, map[string]any{
		"branch":    result.Branch,
		"pr_number": result.PRNumber,
		"pr_url":    result.PRURL,
		"pr_action": result.PRAction,
	}); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			return nil, errors.New(errors.EPersistFailed, err.Error())
		}
		return nil, err
	}

	return result, nil
}

func (s *Server) performWorktreePRSync(
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
