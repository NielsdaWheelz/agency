package daemon

import (
	"context"
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// handleWorktreePRSync handles POST /worktrees/{ref}/pr/sync.
func (s *Server) handleWorktreePRSync(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := prepareRequestID(w, r)

	var req WorktreePRSyncRequest
	if err := decodeOptionalStrictJSON(r.Body, &req); err != nil {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "")
		return
	}

	record, ok := s.resolveWorktreeFromQuery(w, r, worktreeRef, requestID)
	if !ok {
		return
	}
	if record.Meta.State != store.WorktreeStatePresent {
		s.writeErrorWithRequestID(w, http.StatusNotFound, requestID, string(errors.EWorktreeNotFound), "integration worktree is archived", "use a present (non-archived) integration worktree")
		return
	}

	result, err := s.executeWorktreePRSync(r.Context(), record, req)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		s.writeErrorWithRequestID(w, httpStatusForCode(code), requestID, string(code), apiErrorMessage(err), errors.Hint(err))
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
		return nil, err
	}

	result, err := s.performWorktreePRSync(ctx, record, req)
	if err != nil {
		return nil, s.recordWorktreeOpFailure(record.RepoID, record.WorktreeID, prSyncEventFailed, err)
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, prSyncEventSucceeded, map[string]any{
		"branch":    result.Branch,
		"pr_number": result.PRNumber,
		"pr_url":    result.PRURL,
		"pr_action": result.PRAction,
	}); err != nil {
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
