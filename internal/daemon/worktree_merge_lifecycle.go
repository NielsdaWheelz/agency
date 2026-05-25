package daemon

import (
	"context"
	stderrors "errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Server) startWorktreePRMerge(
	record *store.IntegrationWorktreeRecord,
	worktreeRef string,
	requestID string,
	req normalizedMergeRequest,
) (*WorktreePRMergeResponse, int, error) {
	if record == nil || record.Meta == nil {
		return nil, 0, errors.New(errors.EInternal, "worktree metadata missing")
	}

	if resp, attached, err := s.attachWorktreePRMerge(record, requestID, req); err != nil {
		return nil, 0, err
	} else if attached {
		return resp, http.StatusOK, nil
	}

	unlock, err := s.repoLock.Lock(record.RepoID, "worktree_merge")
	if err != nil {
		return nil, 0, errors.NewWithDetails(
			errors.ERepoLocked,
			"repository is locked by another operation",
			map[string]string{"hint": "wait for the other operation to complete"},
		)
	}
	defer func() { _ = unlock() }()

	if err := s.repairInterruptedWorktreeMerge(record); err != nil {
		return nil, 0, err
	}
	if err := s.ensureWorktreeMergeStartAllowed(record, worktreeRef); err != nil {
		return nil, 0, err
	}

	attemptID, err := core.NewRunID(s.clock())
	if err != nil {
		return nil, 0, errors.New(errors.EInternal, "failed to create merge attempt id")
	}

	proc, attached, err := s.beginWorktreeMerge(record.RepoID, record.WorktreeID, attemptID, req)
	if err != nil {
		return nil, 0, err
	}
	if attached {
		resp, _, attachErr := s.attachWorktreePRMerge(record, requestID, req)
		if attachErr != nil {
			return nil, 0, attachErr
		}
		return resp, http.StatusOK, nil
	}

	mergeMeta, err := s.persistStartedWorktreePRMerge(record, proc, requestID, req)
	if err != nil {
		s.releaseWorktreeMerge(proc)
		return nil, 0, err
	}

	go s.runAcceptedWorktreeMerge(proc, record)
	return s.worktreePRMergeResponse(record, requestID, "started", mergeMeta), http.StatusAccepted, nil
}

func (s *Server) attachWorktreePRMerge(
	record *store.IntegrationWorktreeRecord,
	requestID string,
	req normalizedMergeRequest,
) (*WorktreePRMergeResponse, bool, error) {
	if record == nil || record.Meta == nil {
		return nil, false, errors.New(errors.EInternal, "worktree metadata missing")
	}

	existing := s.activeWorktreeMerge(record.RepoID, record.WorktreeID)
	if existing == nil {
		return nil, false, nil
	}
	if !sameNormalizedMergeRequest(existing.request, req) {
		return nil, false, errors.NewWithDetails(
			errors.EWorktreeMergeActive,
			"worktree merge is already running for this worktree",
			map[string]string{
				"hint": "wait for the active merge to finish or rerun with the same options to attach",
			},
		)
	}

	mergeMeta, err := s.readActiveWorktreeMerge(record.RepoID, record.WorktreeID)
	if err != nil {
		return nil, false, err
	}
	return s.worktreePRMergeResponse(record, requestID, "attached", mergeMeta), true, nil
}

func (s *Server) readActiveWorktreeMerge(repoID, worktreeID string) (*store.IntegrationWorktreeMergeMeta, error) {
	mergeMeta, err := s.store.ReadIntegrationWorktreeMerge(repoID, worktreeID)
	if err != nil {
		return nil, err
	}
	if mergeMeta == nil {
		return nil, errors.New(errors.EInternal, "worktree merge is active but merge.json is missing")
	}
	return mergeMeta, nil
}

func (s *Server) repairInterruptedWorktreeMerge(record *store.IntegrationWorktreeRecord) error {
	staleMerge, err := s.store.ReadIntegrationWorktreeMerge(record.RepoID, record.WorktreeID)
	if err != nil {
		return err
	}
	if staleMerge == nil || staleMerge.Status != store.WorktreeMergeStatusRunning {
		return nil
	}
	if s.activeWorktreeMerge(record.RepoID, record.WorktreeID) != nil {
		return nil
	}

	now := s.nowRFC3339()
	if err := s.store.UpdateIntegrationWorktreeMerge(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
		m.Status = store.WorktreeMergeStatusFailed
		m.UpdatedAt = now
		m.FinishedAt = now
		m.ErrorCode = string(errors.EWorktreeMergeInterrupted)
		m.ErrorMessage = "merge attempt lost daemon supervision before reaching a terminal state"
		m.Hint = "rerun 'agency worktree <worktree-ref> pr merge' to resume from durable state"
	}); err != nil {
		return err
	}
	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, mergeEventFailed, map[string]any{
		"error_code": string(errors.EWorktreeMergeInterrupted),
		"message":    "merge attempt lost daemon supervision before reaching a terminal state",
	}); err != nil {
		return err
	}
	return nil
}

func (s *Server) ensureWorktreeMergeStartAllowed(record *store.IntegrationWorktreeRecord, worktreeRef string) error {
	unresolved, err := s.unresolvedInvocationsForWorktree(record.RepoID, record.WorktreeID)
	if err != nil {
		return err
	}
	if len(unresolved) == 0 {
		return nil
	}
	return errors.NewWithDetails(
		errors.EWorktreeHasUnresolvedInvocations,
		fmt.Sprintf("%d unresolved invocations exist for this worktree", len(unresolved)),
		map[string]string{
			"hint": "run 'agency agent ls --worktree " + worktreeRef + "' and land or discard each invocation",
		},
	)
}

func (s *Server) persistStartedWorktreePRMerge(
	record *store.IntegrationWorktreeRecord,
	proc *worktreeMergeProcess,
	requestID string,
	req normalizedMergeRequest,
) (*store.IntegrationWorktreeMergeMeta, error) {
	now := s.clock()
	mergeMeta := store.NewIntegrationWorktreeMergeMeta(
		record.RepoID,
		record.WorktreeID,
		proc.attemptID,
		requestID,
		string(req.Strategy),
		req.DeleteBranch,
		req.AgencyConfigPath,
		now,
	)
	mergeMeta.Branch = record.Meta.Branch
	logsDir := s.store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID)
	mergeMeta.MergeLogPath = filepath.Join(logsDir, "merge.log")
	mergeMeta.VerifyLogPath = filepath.Join(logsDir, "verify.log")
	mergeMeta.ArchiveLogPath = filepath.Join(logsDir, "archive.log")

	if err := s.store.WriteIntegrationWorktreeMerge(record.RepoID, record.WorktreeID, mergeMeta); err != nil {
		return nil, err
	}

	if err := s.store.UpdateIntegrationWorktreeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.LastUsedAt = now.UTC().Format(time.RFC3339)
	}); err != nil {
		return nil, err
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, mergeEventStarted, map[string]any{
		"attempt_id":        proc.attemptID,
		"strategy":          string(req.Strategy),
		"confirmation_mode": req.ConfirmationMode,
		"delete_branch":     req.DeleteBranch,
		"branch":            record.Meta.Branch,
	}); err != nil {
		finishedAt := s.nowRFC3339()
		if updateErr := s.store.UpdateIntegrationWorktreeMerge(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
			m.Status = store.WorktreeMergeStatusFailed
			m.UpdatedAt = finishedAt
			m.FinishedAt = finishedAt
			m.ErrorCode = string(errors.EPersistFailed)
			m.ErrorMessage = apiErrorMessage(err)
		}); updateErr != nil {
			return nil, errors.Wrap(errors.EPersistFailed, "failed to persist merge failure after event append failed", updateErr)
		}
		return nil, err
	}

	return mergeMeta, nil
}

func (s *Server) runAcceptedWorktreeMerge(proc *worktreeMergeProcess, record *store.IntegrationWorktreeRecord) {
	defer s.releaseWorktreeMerge(proc)

	result, err := s.runWorktreeMerge(proc.ctx, record, proc.request)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			if stderrors.Is(err, context.Canceled) {
				code = errors.EWorktreeMergeInterrupted
			} else {
				code = errors.EInternal
			}
		}
		message := err.Error()
		hint := errors.Hint(err)
		if code == errors.EWorktreeMergeInterrupted && hint == "" {
			hint = "rerun 'agency worktree <worktree-ref> pr merge' to resume from durable state"
		}
		s.failWorktreeMerge(record.RepoID, record.WorktreeID, code, message, hint)
		return
	}

	now := s.nowRFC3339()
	if err := s.updateWorktreeMergeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
		m.Status = store.WorktreeMergeStatusSucceeded
		m.Stage = store.WorktreeMergeStageCompleted
		m.Branch = result.Branch
		m.PRNumber = result.PRNumber
		m.PRURL = result.PRURL
		m.MergeLogPath = result.MergeLogPath
		m.VerifyLogPath = result.VerifyLogPath
		m.ArchiveLogPath = result.ArchiveLogPath
		m.ErrorCode = ""
		m.ErrorMessage = ""
		m.Hint = ""
		m.FinishedAt = now
	}); err != nil {
		s.failWorktreeMerge(record.RepoID, record.WorktreeID, errors.EPersistFailed, "failed to persist merge success state: "+err.Error(), "inspect merge state and rerun worktree pr merge")
		return
	}
	if err := s.store.UpdateIntegrationWorktreeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.LastUsedAt = now
	}); err != nil {
		s.failWorktreeMerge(record.RepoID, record.WorktreeID, errors.EPersistFailed, "failed to update worktree metadata after merge: "+err.Error(), "inspect merge state and rerun worktree pr merge")
		return
	}
	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, mergeEventSucceeded, map[string]any{
		"attempt_id":       proc.attemptID,
		"branch":           result.Branch,
		"pr_number":        result.PRNumber,
		"pr_url":           result.PRURL,
		"strategy":         string(result.Strategy),
		"delete_branch":    result.DeleteBranch,
		"merge_log_path":   result.MergeLogPath,
		"verify_log_path":  result.VerifyLogPath,
		"archive_log_path": result.ArchiveLogPath,
	}); err != nil {
		s.failWorktreeMerge(record.RepoID, record.WorktreeID, errors.EPersistFailed, "failed to append merge success event: "+err.Error(), "inspect merge state and rerun worktree pr merge")
		return
	}
}

func (s *Server) updateWorktreeMergeMeta(repoID, worktreeID string, updateFn func(*store.IntegrationWorktreeMergeMeta)) error {
	now := s.nowRFC3339()
	return s.store.UpdateIntegrationWorktreeMerge(repoID, worktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
		updateFn(m)
		m.UpdatedAt = now
	})
}

func (s *Server) failWorktreeMerge(repoID, worktreeID string, code errors.Code, message, hint string) {
	now := s.nowRFC3339()
	if err := s.updateWorktreeMergeMeta(repoID, worktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
		m.Status = store.WorktreeMergeStatusFailed
		m.ErrorCode = string(code)
		m.ErrorMessage = message
		m.Hint = hint
		m.FinishedAt = now
	}); err != nil {
		log.Printf("agencyd: persist failed merge for worktree %s/%s: %v", repoID, worktreeID, err)
	}
	if err := s.store.UpdateIntegrationWorktreeMeta(repoID, worktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.LastUsedAt = now
	}); err != nil {
		log.Printf("agencyd: update worktree meta after failed merge %s/%s: %v", repoID, worktreeID, err)
	}
	if err := s.appendWorktreeEvent(repoID, worktreeID, mergeEventFailed, map[string]any{
		"error_code": string(code),
		"message":    message,
	}); err != nil {
		log.Printf("agencyd: append merge-failed event for worktree %s/%s: %v", repoID, worktreeID, err)
	}
}
