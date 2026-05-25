package daemon

import (
	"context"
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// executeTaskStartAfterCreated runs the worktree-setup and invocation-start
// phases of task start, returning the post-running task meta on success or a
// typed failure (with the failed phase name) for the caller to persist via
// markTaskFailed. The caller must already have written task_started.
func (s *Server) executeTaskStartAfterCreated(
	ctx context.Context,
	req TaskStartRequest,
	repoRoot, repoID, taskID, fingerprint string,
	execCtx executionContext,
	gitEnv map[string]string,
	requestEnv map[string]string,
) (*store.TaskMeta, string, *startFailure) {
	wtRecord, phase, fail := s.setupTaskWorktree(ctx, repoRoot, repoID, taskID, req, execCtx, gitEnv)
	if fail != nil {
		return nil, phase, fail
	}

	invMeta, phase, fail := s.startTaskInvocation(ctx, repoRoot, repoID, taskID, fingerprint, wtRecord, req, requestEnv, gitEnv)
	if fail != nil {
		return nil, phase, fail
	}

	if err := s.appendTaskEvent(repoID, taskID, "agency.task_running", map[string]any{
		"invocation_id": invMeta.InvocationID,
		"worktree_id":   wtRecord.WorktreeID,
	}); err != nil {
		s.abortStartedTaskInvocation(repoID, invMeta, "task_event_running_failed")
		f := newStartFailure(http.StatusInternalServerError, errors.EPersistFailed, "failed to append task event: "+err.Error(), "")
		return nil, "task_event_running", &f
	}

	taskMeta, err := s.markTaskRunning(repoID, taskID, invMeta)
	if err != nil {
		s.abortStartedTaskInvocation(repoID, invMeta, "task_running_update_failed")
		f := startFailureFromError(http.StatusInternalServerError, errors.EMetaWriteFailed, err, "")
		return nil, "task_running_update", &f
	}
	return taskMeta, "", nil
}

func (s *Server) setupTaskWorktree(
	ctx context.Context,
	repoRoot, repoID, taskID string,
	req TaskStartRequest,
	execCtx executionContext,
	gitEnv map[string]string,
) (*store.IntegrationWorktreeRecord, string, *startFailure) {
	wtSvc := integrationworktree.NewService(s.store, s.runner, s.fsys, s.clock)
	wtCreate, err := wtSvc.Create(ctx, integrationworktree.CreateOpts{
		Name:             req.Name,
		RepoRoot:         repoRoot,
		RepoID:           repoID,
		BaseBranch:       req.BaseBranch,
		CheckoutRoot:     execCtx.CheckoutRoot,
		ExecutionProfile: execCtx.Profile,
		Env:              gitEnv,
	})
	if err != nil {
		f := startFailureFromError(http.StatusInternalServerError, errors.EWorktreeCreateFailed, err, "")
		return nil, "worktree_create", &f
	}
	wtMeta, err := s.store.ReadIntegrationWorktreeMeta(repoID, wtCreate.WorktreeID)
	if err != nil {
		f := startFailureFromError(http.StatusInternalServerError, errors.EWorktreeBroken, err, "")
		return nil, "worktree_read", &f
	}
	if err := s.store.UpdateIntegrationWorktreeMeta(repoID, wtCreate.WorktreeID, func(meta *store.IntegrationWorktreeMeta) {
		meta.TaskID = taskID
	}); err != nil {
		f := startFailureFromError(http.StatusInternalServerError, errors.EMetaWriteFailed, err, "")
		return nil, "worktree_task_link", &f
	}
	if err := s.updateTaskWorktree(repoID, taskID, wtMeta, wtCreate); err != nil {
		f := startFailureFromError(http.StatusInternalServerError, errors.EMetaWriteFailed, err, "")
		return nil, "task_worktree_update", &f
	}
	if err := s.appendTaskEvent(repoID, taskID, "agency.task_worktree_created", map[string]any{
		"worktree_id":   wtCreate.WorktreeID,
		"worktree_name": req.Name,
		"branch":        wtCreate.Branch,
		"tree_path":     wtCreate.TreePath,
	}); err != nil {
		f := newStartFailure(http.StatusInternalServerError, errors.EPersistFailed, "failed to append task event: "+err.Error(), "")
		return nil, "task_event_worktree_created", &f
	}
	return &store.IntegrationWorktreeRecord{
		WorktreeID:  wtCreate.WorktreeID,
		RepoID:      repoID,
		Name:        req.Name,
		Meta:        wtMeta,
		WorktreeDir: s.store.IntegrationWorktreeDir(repoID, wtCreate.WorktreeID),
	}, "", nil
}

func (s *Server) startTaskInvocation(
	ctx context.Context,
	repoRoot, repoID, taskID, fingerprint string,
	wtRecord *store.IntegrationWorktreeRecord,
	req TaskStartRequest,
	requestEnv map[string]string,
	gitEnv map[string]string,
) (*store.InvocationMeta, string, *startFailure) {
	envKeys := sortedEnvKeys(requestEnv)
	var invMeta *store.InvocationMeta
	var err error
	if req.Mode == string(store.RunnerModeHeadless) {
		invMeta, err = s.startTaskHeadlessInvocation(ctx, repoRoot, repoID, taskID, fingerprint, wtRecord, req, envKeys, gitEnv)
	} else {
		invMeta, err = s.startTaskHeadedInvocation(ctx, repoRoot, repoID, taskID, fingerprint, wtRecord, req, envKeys, gitEnv)
	}
	if err != nil {
		f := asStartFailure(err)
		return nil, "invocation_start", &f
	}
	return invMeta, "", nil
}
