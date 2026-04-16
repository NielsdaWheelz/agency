package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

const (
	worktreeRebaseEventStarted   = "agency.worktree_rebase_started"
	worktreeRebaseEventSucceeded = "agency.worktree_rebase_succeeded"
	worktreeRebaseEventFailed    = "agency.worktree_rebase_failed"
)

// handleWorktreeRebase handles POST /worktrees/{ref}/rebase.
func (s *Server) handleWorktreeRebase(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)

	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeWorktreeRebaseError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	if decodeErr := decodeWorktreeRebaseRequest(r.Body); decodeErr != "" {
		s.writeWorktreeRebaseError(w, http.StatusBadRequest, requestID, string(errors.EInvalidArgument), decodeErr, "")
		return
	}

	record, err := s.resolveWorktreeRef(worktreeRef, repoID)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeWorktreeRebaseError(w, worktreeRebaseHTTPStatusForCode(code), requestID, string(code), err.Error(), "use 'agency worktree ls' to list worktrees")
		return
	}
	if record == nil || record.Meta == nil {
		s.writeWorktreeRebaseError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "worktree metadata missing", "")
		return
	}

	unlock, err := s.repoLock.Lock(record.RepoID, "worktree_rebase")
	if err != nil {
		s.writeWorktreeRebaseError(
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

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, worktreeRebaseEventStarted, map[string]any{
		"branch":      record.Meta.Branch,
		"base_branch": record.Meta.BaseBranch,
	}); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EPersistFailed
		}
		s.writeWorktreeRebaseError(w, worktreeRebaseHTTPStatusForCode(code), requestID, string(code), err.Error(), "")
		return
	}

	if err := s.runWorktreeRebase(r.Context(), record); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		hint := ""
		if ae, ok := errors.AsAgencyError(err); ok && ae.Details != nil {
			hint = strings.TrimSpace(ae.Details["hint"])
		}

		if appendErr := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, worktreeRebaseEventFailed, map[string]any{
			"error_code": string(code),
			"message":    err.Error(),
		}); appendErr != nil {
			appendCode := errors.GetCode(appendErr)
			if appendCode == "" {
				appendCode = errors.EPersistFailed
			}
			s.writeWorktreeRebaseError(w, worktreeRebaseHTTPStatusForCode(appendCode), requestID, string(appendCode), appendErr.Error(), "")
			return
		}

		s.writeWorktreeRebaseError(w, worktreeRebaseHTTPStatusForCode(code), requestID, string(code), err.Error(), hint)
		return
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, worktreeRebaseEventSucceeded, map[string]any{
		"branch":      record.Meta.Branch,
		"base_branch": record.Meta.BaseBranch,
	}); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EPersistFailed
		}
		s.writeWorktreeRebaseError(w, worktreeRebaseHTTPStatusForCode(code), requestID, string(code), err.Error(), "")
		return
	}

	s.writeJSON(w, http.StatusOK, WorktreeRebaseResponse{
		OK:                    true,
		APIVersion:            APIVersion,
		BuildVersion:          version.FullVersion(),
		RequestID:             requestID,
		RepoID:                record.RepoID,
		IntegrationWorktreeID: record.WorktreeID,
		Branch:                record.Meta.Branch,
		BaseBranch:            record.Meta.BaseBranch,
	})
}

func (s *Server) runWorktreeRebase(ctx context.Context, record *store.IntegrationWorktreeRecord) error {
	if record == nil || record.Meta == nil {
		return errors.New(errors.EInternal, "worktree metadata missing")
	}
	wtMeta := record.Meta

	clean, dirtyStatus, err := prSyncDirtyStatus(ctx, s.Runner, wtMeta.TreePath)
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

	if err := prSyncGitFetchOrigin(ctx, s.Runner, wtMeta.TreePath); err != nil {
		return err
	}

	baseBranch := strings.TrimSpace(wtMeta.BaseBranch)
	if baseBranch == "" {
		return errors.New(errors.EInternal, "worktree base branch is missing")
	}
	rebaseTarget := "origin/" + baseBranch
	rebaseResult, runErr := s.Runner.Run(ctx, "git", []string{"rebase", rebaseTarget}, exec.RunOpts{
		Dir: wtMeta.TreePath,
		Env: prSyncNonInteractiveEnv(),
	})
	if runErr != nil {
		return errors.Wrap(errors.EInternal, "git rebase failed to start", runErr)
	}
	if rebaseResult.ExitCode != 0 {
		abortResult, abortErr := s.Runner.Run(ctx, "git", []string{"rebase", "--abort"}, exec.RunOpts{
			Dir: wtMeta.TreePath,
			Env: prSyncNonInteractiveEnv(),
		})
		abortFailed := abortErr != nil || abortResult.ExitCode != 0
		details := map[string]string{
			"rebase_target": rebaseTarget,
			"stderr":        strings.TrimSpace(rebaseResult.Stderr),
			"hint":          "resolve the rebase conflicts locally, then retry",
		}
		if abortFailed {
			details["abort_failed"] = "true"
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
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if err == io.EOF {
			return ""
		}
		return prSyncDecodeErrorMessage(err)
	}

	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return "invalid request body: expected a single JSON object: " + err.Error()
	}

	return ""
}

func worktreeRebaseHTTPStatusForCode(code errors.Code) int {
	switch code {
	case errors.EWorktreeNotFound:
		return http.StatusNotFound
	case errors.EWorktreeIDAmbiguous, errors.ERepoLocked:
		return http.StatusConflict
	case errors.EDirtyWorktree, errors.ERebaseConflict:
		return http.StatusConflict
	case errors.EInvalidArgument:
		return http.StatusBadRequest
	case errors.EPersistFailed:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
