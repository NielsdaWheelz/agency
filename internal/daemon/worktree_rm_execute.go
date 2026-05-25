package daemon

import (
	"context"
	stderrors "errors"
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/landing"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// rmFailure describes a typed failure from executeWorktreeRm so the handler can
// emit worktree_rm_failed and write a structured HTTP error.
type rmFailure struct {
	status  int
	code    errors.Code
	message string
	hint    string
}

// executeWorktreeRm performs the rm work under an already-held repo lock, after
// worktree_rm_started has been emitted. It returns nil on success or a typed
// failure for the handler to convert into worktree_rm_failed + HTTP error.
func (s *Server) executeWorktreeRm(
	ctx context.Context,
	req WorktreeRmRequest,
	record *store.IntegrationWorktreeRecord,
	repoRoot string,
	treeMissing bool,
	unresolved []store.InvocationRecord,
) *rmFailure {
	if req.Force && len(unresolved) > 0 {
		if fail := s.discardUnresolvedInvocations(ctx, repoRoot, record.RepoID, unresolved); fail != nil {
			return fail
		}
	}

	if treeMissing {
		if err := s.archiveWorktreeState(record.RepoID, record.WorktreeID); err != nil {
			return &rmFailure{http.StatusInternalServerError, errors.EWorktreeRemoveFailed,
				"worktree tree is missing and metadata archive failed: " + err.Error(),
				"inspect worktree metadata before retrying"}
		}
		return nil
	}

	if !integrationworktree.HasIntegrationMarker(record.Meta.TreePath) {
		return &rmFailure{http.StatusBadRequest, errors.ENotAnIntegrationWorktree,
			"tree missing .agency/INTEGRATION_MARKER - not an integration worktree",
			"this safety check prevents accidentally deleting user-managed worktrees"}
	}

	profileEnv, err := s.executionProfileEnv(record.Meta.ExecutionProfile)
	if err != nil {
		return &rmFailure{http.StatusBadRequest, errors.CodeOr(err, errors.EExecutionProfileNotFound),
			apiErrorMessage(err), ""}
	}
	worktreeEnv := withNonInteractiveEnv(profileEnv)

	if !req.Force {
		if fail := s.ensureCleanWorktreeTree(ctx, record.Meta.TreePath, worktreeEnv); fail != nil {
			return fail
		}
	}

	if fail := s.gitWorktreeRemove(ctx, req.Force, repoRoot, record.Meta.TreePath, worktreeEnv); fail != nil {
		return fail
	}

	if err := s.archiveWorktreeState(record.RepoID, record.WorktreeID); err != nil {
		return &rmFailure{http.StatusInternalServerError, errors.EWorktreeRemoveFailed,
			"failed to archive worktree metadata after git remove: " + err.Error(),
			"inspect worktree metadata before retrying"}
	}
	return nil
}

func (s *Server) discardUnresolvedInvocations(ctx context.Context, repoRoot, repoID string, unresolved []store.InvocationRecord) *rmFailure {
	discardSvc := landing.NewService(s.store, s.runner, s.fsys, s.clock, s.invocationEvents)
	for _, inv := range unresolved {
		profileEnv, err := s.executionProfileEnv(inv.Meta.ExecutionProfile)
		if err != nil {
			return &rmFailure{http.StatusBadRequest, errors.CodeOr(err, errors.EExecutionProfileNotFound),
				apiErrorMessage(err), ""}
		}
		if err := discardSvc.Discard(ctx, landing.DiscardOpts{
			RepoID:       repoID,
			InvocationID: inv.InvocationID,
			RepoRoot:     repoRoot,
			Env:          withNonInteractiveEnv(profileEnv),
			StopCallback: s.stopInvocationForDiscard,
		}); err != nil {
			return &rmFailure{http.StatusConflict, errors.CodeOr(err, errors.ELandFailed),
				err.Error(), errors.Hint(err)}
		}
	}
	return nil
}

func (s *Server) ensureCleanWorktreeTree(ctx context.Context, treePath string, env map[string]string) *rmFailure {
	clean, err := git.IsClean(ctx, s.runner, treePath, env)
	if err != nil {
		return &rmFailure{http.StatusInternalServerError, errors.EWorktreeRemoveFailed,
			"failed to check worktree cleanliness: " + err.Error(), ""}
	}
	if !clean {
		return &rmFailure{http.StatusConflict, errors.EDirtyWorktree,
			"worktree has uncommitted changes", "commit/stash your changes or use --force"}
	}
	return nil
}

func (s *Server) gitWorktreeRemove(ctx context.Context, force bool, repoRoot, treePath string, env map[string]string) *rmFailure {
	args := []string{"-C", repoRoot, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, treePath)

	removeCtx, cancel := context.WithTimeout(ctx, worktreeRmGitRemoveTimeout)
	defer cancel()

	result, runErr := s.runner.Run(removeCtx, "git", args, exec.RunOpts{Env: env})
	if runErr != nil {
		if stderrors.Is(runErr, context.DeadlineExceeded) {
			return &rmFailure{http.StatusInternalServerError, errors.EWorktreeRemoveFailed,
				"git worktree remove timed out",
				"retry the removal or inspect the worktree for a blocked git process"}
		}
		return &rmFailure{http.StatusInternalServerError, errors.EWorktreeRemoveFailed,
			"failed to execute git worktree remove: " + runErr.Error(), ""}
	}
	if result.ExitCode != 0 {
		stderr := strings.TrimSpace(result.Stderr)
		if !force && (strings.Contains(stderr, "untracked") || strings.Contains(stderr, "modified")) {
			return &rmFailure{http.StatusConflict, errors.EDirtyWorktree,
				"worktree has uncommitted changes", "commit/stash your changes or use --force"}
		}
		return &rmFailure{http.StatusInternalServerError, errors.EWorktreeRemoveFailed,
			"git worktree remove failed: " + stderr, ""}
	}
	return nil
}

func (s *Server) archiveWorktreeState(repoID, worktreeID string) error {
	return s.store.UpdateIntegrationWorktreeMeta(repoID, worktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.State = store.WorktreeStateArchived
	})
}
