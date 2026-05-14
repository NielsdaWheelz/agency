package daemon

import (
	"context"
	"io"
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

	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeWorktreeRebaseError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "repo_id query parameter is required", "")
		return
	}

	if decodeErr := decodeWorktreeRebaseRequest(r.Body); decodeErr != "" {
		s.writeWorktreeRebaseError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), decodeErr, "")
		return
	}

	record, err := s.resolveWorktreeRefForRepo(worktreeRef, repoID)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeWorktreeRebaseError(w, worktreeRebaseHTTPStatusForCode(code), requestID, string(code), apiErrorMessage(err), "use 'agency worktree ls' to list worktrees")
		return
	}
	if record == nil || record.Broken || record.Meta == nil {
		s.writeWorktreeRebaseError(w, http.StatusBadRequest, requestID, string(errors.EWorktreeBroken), "integration worktree exists but meta.json is unreadable", "inspect or recreate the worktree")
		return
	}
	if record.Meta.State != store.WorktreeStatePresent {
		s.writeWorktreeRebaseError(w, http.StatusNotFound, requestID, string(errors.EWorktreeNotFound), "integration worktree is archived", "use a present (non-archived) integration worktree")
		return
	}

	if err := s.executeWorktreeRebase(r.Context(), record); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		hint := ""
		if ae, ok := errors.AsAgencyError(err); ok && ae.Details != nil {
			hint = strings.TrimSpace(ae.Details["hint"])
		}
		s.writeWorktreeRebaseError(w, worktreeRebaseHTTPStatusForCode(code), requestID, string(code), apiErrorMessage(err), hint)
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
		code := errors.GetCode(err)
		if code == "" {
			return errors.New(errors.EPersistFailed, err.Error())
		}
		return err
	}

	if err := s.performWorktreeRebase(ctx, record); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		if appendErr := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, worktreeRebaseEventFailed, map[string]any{
			"error_code": string(code),
			"message":    apiErrorMessage(err),
		}); appendErr != nil {
			appendCode := errors.GetCode(appendErr)
			if appendCode == "" {
				return errors.New(errors.EPersistFailed, appendErr.Error())
			}
			return appendErr
		}
		return err
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, worktreeRebaseEventSucceeded, map[string]any{
		"branch":      record.Meta.Branch,
		"base_branch": record.Meta.BaseBranch,
	}); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			return errors.New(errors.EPersistFailed, err.Error())
		}
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
	env := prSyncNonInteractiveEnv(profileEnv)

	clean, dirtyStatus, err := prSyncDirtyStatus(ctx, s.Runner, wtMeta.TreePath, env)
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

	if err := prSyncGitFetchOrigin(ctx, s.Runner, wtMeta.TreePath, env); err != nil {
		return err
	}

	baseBranch := strings.TrimSpace(wtMeta.BaseBranch)
	if baseBranch == "" {
		return errors.New(errors.EInternal, "worktree base branch is missing")
	}
	rebaseTarget := "origin/" + baseBranch
	rebaseResult, runErr := s.Runner.Run(ctx, "git", []string{"rebase", rebaseTarget}, exec.RunOpts{
		Dir: wtMeta.TreePath,
		Env: env,
	})
	if runErr != nil {
		return errors.Wrap(errors.EInternal, "git rebase failed to start", runErr)
	}
	if rebaseResult.ExitCode != 0 {
		abortResult, abortErr := s.Runner.Run(ctx, "git", []string{"rebase", "--abort"}, exec.RunOpts{
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

func decodeWorktreeRebaseRequest(body io.Reader) string {
	var req struct{}
	if err := decodeOptionalStrictJSON(body, &req); err != nil {
		return prSyncDecodeErrorMessage(err)
	}
	return ""
}

func worktreeRebaseHTTPStatusForCode(code errors.Code) int {
	switch code {
	case errors.EWorktreeNotFound:
		return http.StatusNotFound
	case errors.EWorktreeIDAmbiguous, errors.ERepoLocked, errors.EWorktreeMergeActive:
		return http.StatusConflict
	case errors.EDirtyWorktree, errors.ERebaseConflict:
		return http.StatusConflict
	case errors.EGitFetchFailed:
		return http.StatusBadGateway
	case errors.EInvalidArgument:
		return http.StatusBadRequest
	case errors.EPersistFailed:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
