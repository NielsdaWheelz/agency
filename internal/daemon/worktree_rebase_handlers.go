package daemon

import (
	"context"
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const (
	worktreeRebaseEventStarted   = "agency.worktree_rebase_started"
	worktreeRebaseEventSucceeded = "agency.worktree_rebase_succeeded"
	worktreeRebaseEventFailed    = "agency.worktree_rebase_failed"
)

// handleWorktreeRebase handles POST /worktrees/{ref}/rebase.
func (s *Server) handleWorktreeRebase(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := prepareRequestID(w, r)

	var req struct{}
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

	if err := s.executeWorktreeRebase(r.Context(), record); err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		s.writeErrorWithRequestID(w, httpStatusForCode(code), requestID, string(code), apiErrorMessage(err), errors.Hint(err))
		return
	}
	s.writeWorktreeRebaseSuccess(w, requestID, record)
}

func (s *Server) executeWorktreeRebase(
	ctx context.Context,
	record *store.IntegrationWorktreeRecord,
) error {
	if record == nil || record.Meta == nil {
		return errors.New(errors.EInternal, "worktree metadata missing")
	}

	unlock, err := s.repoLock.Lock(record.RepoID, "worktree_rebase")
	if err != nil {
		return errors.NewWithDetails(
			errors.ERepoLocked,
			"repository is locked by another operation",
			map[string]string{"hint": "wait for the other operation to complete"},
		)
	}
	defer func() { _ = unlock() }()

	if err := s.ensureWorktreeMergeInactive(record.RepoID, record.WorktreeID, "rebase the worktree"); err != nil {
		return err
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, worktreeRebaseEventStarted, map[string]any{
		"branch":      record.Meta.Branch,
		"base_branch": record.Meta.BaseBranch,
	}); err != nil {
		return err
	}

	if err := s.performWorktreeRebase(ctx, record); err != nil {
		return s.recordWorktreeOpFailure(record.RepoID, record.WorktreeID, worktreeRebaseEventFailed, err)
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, worktreeRebaseEventSucceeded, map[string]any{
		"branch":      record.Meta.Branch,
		"base_branch": record.Meta.BaseBranch,
	}); err != nil {
		return err
	}

	return nil
}

func (s *Server) performWorktreeRebase(ctx context.Context, record *store.IntegrationWorktreeRecord) error {
	if record == nil || record.Meta == nil {
		return errors.New(errors.EInternal, "worktree metadata missing")
	}
	wtMeta := record.Meta
	profileEnv, err := s.executionProfileEnv(wtMeta.ExecutionProfile)
	if err != nil {
		return err
	}
	env := withNonInteractiveEnv(profileEnv)

	clean, dirtyStatus, err := dirtyStatus(ctx, s.runner, wtMeta.TreePath, env)
	if err != nil {
		return err
	}
	if !clean {
		return errors.NewWithDetails(
			errors.EDirtyWorktree,
			"worktree has uncommitted changes; rebase requires a clean integration tree",
			map[string]string{
				"dirty_status": dirtyStatus,
				"hint":         "commit/stash/reset integration changes before rebase",
			},
		)
	}

	if err := gitFetchOrigin(ctx, s.runner, wtMeta.TreePath, env); err != nil {
		return err
	}

	baseBranch := strings.TrimSpace(wtMeta.BaseBranch)
	if baseBranch == "" {
		return errors.New(errors.EInternal, "worktree base branch is missing")
	}
	rebaseTarget := "origin/" + baseBranch
	rebaseResult, runErr := s.runner.Run(ctx, "git", []string{"rebase", rebaseTarget}, exec.RunOpts{
		Dir: wtMeta.TreePath,
		Env: env,
	})
	if runErr != nil {
		return errors.Wrap(errors.EInternal, "git rebase failed to start", runErr)
	}
	if rebaseResult.ExitCode != 0 {
		abortResult, abortErr := s.runner.Run(ctx, "git", []string{"rebase", "--abort"}, exec.RunOpts{
			Dir: wtMeta.TreePath,
			Env: env,
		})
		abortFailed := abortErr != nil || abortResult.ExitCode != 0
		details := map[string]string{
			"rebase_target": rebaseTarget,
			"stderr":        strings.TrimSpace(rebaseResult.Stderr),
			"hint":          "the rebase was rolled back; rebase locally to resolve conflicts, then retry",
		}
		if abortFailed {
			details["abort_failed"] = "true"
			details["hint"] = "resolve the rebase conflicts locally, then retry; automatic rollback failed"
		}
		return errors.NewWithDetails(
			errors.ERebaseConflict,
			"rebase conflict while rebasing worktree",
			details,
		)
	}

	return nil
}
