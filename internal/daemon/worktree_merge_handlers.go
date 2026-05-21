package daemon

import (
	"context"
	stderrors "errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/mergeflow"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/verify"
)

// worktreeMergeArchiveRemoveTimeout bounds the git worktree removal performed after archive cleanup.
const worktreeMergeArchiveRemoveTimeout = 30 * time.Second

const (
	worktreeMergeRepoLockAcquireTimeout = 30 * time.Second
	worktreeMergeRepoLockPollInterval   = 50 * time.Millisecond
)

// handleWorktreePRMerge handles POST /worktrees/{ref}/pr/merge.
func (s *Server) handleWorktreePRMerge(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := prepareRequestID(w, r)

	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeWorktreeMergeError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidRequest),
			"repo_id query parameter is required",
			"pass ?repo_id=<repo_id>",
		)
		return
	}

	var req WorktreePRMergeRequest
	if err := decodeOptionalStrictJSON(r.Body, &req); err != nil {
		s.writeWorktreeMergeError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidRequest),
			strictJSONDecodeErrorMessage(err),
			"",
		)
		return
	}

	normalizedReq, err := normalizeMergeRequest(req)
	if err != nil {
		code := errors.CodeOr(err, errors.EInvalidArgument)
		s.writeWorktreeMergeError(w, httpStatusForCode(code), requestID, string(code), err.Error(), mergeHintFromError(err))
		return
	}

	record, err := s.resolveWorktreeRefForRepo(worktreeRef, repoID)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		s.writeWorktreeMergeError(w, httpStatusForCode(code), requestID, string(code), apiErrorMessage(err), "use 'agency worktree ls' to list worktrees")
		return
	}
	if record == nil || record.Broken || record.Meta == nil {
		s.writeWorktreeMergeError(w, http.StatusBadRequest, requestID, string(errors.EWorktreeBroken), "integration worktree exists but meta.json is unreadable", "inspect or recreate the worktree")
		return
	}
	if record.Meta.State != store.WorktreeStatePresent {
		mergeMeta, readErr := s.Store.ReadIntegrationWorktreeMerge(record.RepoID, record.WorktreeID)
		if readErr != nil {
			code := errors.CodeOr(readErr, errors.EStoreCorrupt)
			s.writeWorktreeMergeError(w, httpStatusForCode(code), requestID, string(code), apiErrorMessage(readErr), "inspect worktree merge state")
			return
		}
		if record.Meta.State != store.WorktreeStateArchived || worktreeRef != record.WorktreeID || mergeMeta == nil || mergeMeta.Status == store.WorktreeMergeStatusSucceeded || mergeMeta.Stage != store.WorktreeMergeStageArchive {
			s.writeWorktreeMergeError(w, http.StatusNotFound, requestID, string(errors.EWorktreeNotFound), "integration worktree is archived", "archived worktree merge cleanup retries must use the exact worktree_id")
			return
		}
	}

	resp, status, err := s.startWorktreePRMerge(record, worktreeRef, requestID, normalizedReq)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		s.writeWorktreeMergeError(w, httpStatusForCode(code), requestID, string(code), apiErrorMessage(err), mergeHintFromError(err))
		return
	}

	s.writeJSON(w, status, *resp)
}

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

	attemptID, err := core.NewRunID(s.Clock())
	if err != nil {
		return nil, 0, errors.New(errors.EInternal, "failed to create merge attempt id")
	}

	proc, attached, err := s.beginWorktreeMerge(record.RepoID, record.WorktreeID, attemptID, requestID, req)
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
	if !sameNormalizedMergeRequest(existing.Request, req) {
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
	mergeMeta, err := s.Store.ReadIntegrationWorktreeMerge(repoID, worktreeID)
	if err != nil {
		if errors.GetCode(err) == "" {
			return nil, errors.New(errors.EStoreCorrupt, err.Error())
		}
		return nil, err
	}
	if mergeMeta == nil {
		return nil, errors.New(errors.EInternal, "worktree merge is active but merge.json is missing")
	}
	return mergeMeta, nil
}

func (s *Server) repairInterruptedWorktreeMerge(record *store.IntegrationWorktreeRecord) error {
	staleMerge, err := s.Store.ReadIntegrationWorktreeMerge(record.RepoID, record.WorktreeID)
	if err != nil {
		if errors.GetCode(err) == "" {
			return errors.New(errors.EStoreCorrupt, err.Error())
		}
		return err
	}
	if staleMerge == nil || staleMerge.Status != store.WorktreeMergeStatusRunning {
		return nil
	}
	if s.activeWorktreeMerge(record.RepoID, record.WorktreeID) != nil {
		return nil
	}

	now := s.Clock().UTC().Format(time.RFC3339)
	if err := s.Store.UpdateIntegrationWorktreeMerge(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
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
		if errors.GetCode(err) == "" {
			return errors.New(errors.EInternal, err.Error())
		}
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
	proc *WorktreeMergeProcess,
	requestID string,
	req normalizedMergeRequest,
) (*store.IntegrationWorktreeMergeMeta, error) {
	now := s.Clock()
	mergeMeta := store.NewIntegrationWorktreeMergeMeta(
		record.RepoID,
		record.WorktreeID,
		proc.AttemptID,
		requestID,
		string(req.Strategy),
		req.DeleteBranch,
		req.AgencyConfigPath,
		now,
	)
	mergeMeta.Branch = record.Meta.Branch
	mergeMeta.MergeLogPath = filepath.Join(s.Store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID), "merge.log")
	mergeMeta.VerifyLogPath = filepath.Join(s.Store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID), "verify.log")
	mergeMeta.ArchiveLogPath = filepath.Join(s.Store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID), "archive.log")

	if err := s.Store.WriteIntegrationWorktreeMerge(record.RepoID, record.WorktreeID, mergeMeta); err != nil {
		if errors.GetCode(err) == "" {
			return nil, errors.New(errors.EMetaWriteFailed, err.Error())
		}
		return nil, err
	}

	if err := s.Store.UpdateIntegrationWorktreeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.LastUsedAt = now.UTC().Format(time.RFC3339)
	}); err != nil {
		if errors.GetCode(err) == "" {
			return nil, errors.New(errors.EMetaWriteFailed, err.Error())
		}
		return nil, err
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, mergeEventStarted, map[string]any{
		"attempt_id":        proc.AttemptID,
		"strategy":          string(req.Strategy),
		"confirmation_mode": req.ConfirmationMode,
		"delete_branch":     req.DeleteBranch,
		"branch":            record.Meta.Branch,
	}); err != nil {
		finishedAt := s.Clock().UTC().Format(time.RFC3339)
		if updateErr := s.Store.UpdateIntegrationWorktreeMerge(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
			m.Status = store.WorktreeMergeStatusFailed
			m.UpdatedAt = finishedAt
			m.FinishedAt = finishedAt
			m.ErrorCode = string(errors.EPersistFailed)
			m.ErrorMessage = apiErrorMessage(err)
		}); updateErr != nil {
			return nil, errors.Wrap(errors.EPersistFailed, "failed to persist merge failure after event append failed", updateErr)
		}
		if errors.GetCode(err) == "" {
			return nil, errors.New(errors.EPersistFailed, err.Error())
		}
		return nil, err
	}

	return mergeMeta, nil
}

func (s *Server) runAcceptedWorktreeMerge(proc *WorktreeMergeProcess, record *store.IntegrationWorktreeRecord) {
	defer s.releaseWorktreeMerge(proc)

	result, err := s.runWorktreeMerge(proc.ctx, record, proc.Request)
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
		hint := mergeHintFromError(err)
		if code == errors.EWorktreeMergeInterrupted && hint == "" {
			hint = "rerun 'agency worktree <worktree-ref> pr merge' to resume from durable state"
		}
		s.failWorktreeMerge(record.RepoID, record.WorktreeID, code, message, hint)
		return
	}

	now := s.Clock().UTC().Format(time.RFC3339)
	if err := s.updateWorktreeMergeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
		m.Status = store.WorktreeMergeStatusSucceeded
		m.Stage = store.WorktreeMergeStageCompleted
		m.Branch = result.Branch
		m.PRNumber = result.PRNumber
		m.PRURL = result.PRURL
		m.MergeLogPath = result.MergeLogPath
		m.VerifyLogPath = result.VerifyLog
		m.ArchiveLogPath = result.ArchiveLogPath
		m.ErrorCode = ""
		m.ErrorMessage = ""
		m.Hint = ""
		m.FinishedAt = now
	}); err != nil {
		s.failWorktreeMerge(record.RepoID, record.WorktreeID, errors.EPersistFailed, "failed to persist merge success state: "+err.Error(), "inspect merge state and rerun worktree pr merge")
		return
	}
	if err := s.Store.UpdateIntegrationWorktreeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.LastUsedAt = now
	}); err != nil {
		s.failWorktreeMerge(record.RepoID, record.WorktreeID, errors.EPersistFailed, "failed to update worktree metadata after merge: "+err.Error(), "inspect merge state and rerun worktree pr merge")
		return
	}
	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, mergeEventSucceeded, map[string]any{
		"attempt_id":       proc.AttemptID,
		"branch":           result.Branch,
		"pr_number":        result.PRNumber,
		"pr_url":           result.PRURL,
		"strategy":         string(result.Strategy),
		"delete_branch":    result.DeleteBranch,
		"merge_log_path":   result.MergeLogPath,
		"verify_log_path":  result.VerifyLog,
		"archive_log_path": result.ArchiveLogPath,
	}); err != nil {
		s.failWorktreeMerge(record.RepoID, record.WorktreeID, errors.EPersistFailed, "failed to append merge success event: "+err.Error(), "inspect merge state and rerun worktree pr merge")
		return
	}
}

func (s *Server) updateWorktreeMergeMeta(repoID, worktreeID string, updateFn func(*store.IntegrationWorktreeMergeMeta)) error {
	now := s.Clock().UTC().Format(time.RFC3339)
	return s.Store.UpdateIntegrationWorktreeMerge(repoID, worktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
		updateFn(m)
		m.UpdatedAt = now
	})
}

func (s *Server) failWorktreeMerge(repoID, worktreeID string, code errors.Code, message, hint string) {
	now := s.Clock().UTC().Format(time.RFC3339)
	if err := s.updateWorktreeMergeMeta(repoID, worktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
		m.Status = store.WorktreeMergeStatusFailed
		m.ErrorCode = string(code)
		m.ErrorMessage = message
		m.Hint = hint
		m.FinishedAt = now
	}); err != nil {
		log.Printf("agencyd: persist failed merge for worktree %s/%s: %v", repoID, worktreeID, err)
	}
	if err := s.Store.UpdateIntegrationWorktreeMeta(repoID, worktreeID, func(m *store.IntegrationWorktreeMeta) {
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

func (s *Server) runWorktreeMerge(
	ctx context.Context,
	record *store.IntegrationWorktreeRecord,
	req normalizedMergeRequest,
) (*mergeResult, error) {
	if record == nil || record.Meta == nil {
		return nil, errors.New(errors.EInternal, "worktree metadata missing")
	}
	wtMeta := record.Meta
	repoRoot, err := s.resolveMergeRepoRoot(ctx, record.RepoID, wtMeta.TreePath)
	if err != nil {
		return nil, err
	}
	profileEnv, err := s.executionProfileEnv(wtMeta.ExecutionProfile)
	if err != nil {
		return nil, err
	}
	env := prSyncNonInteractiveEnv(profileEnv)

	if err := prSyncCheckGHAuth(ctx, s.Runner, repoRoot, env); err != nil {
		return nil, err
	}

	ghRepo, owner, err := s.resolveMergeGitHubRepo(ctx, record.RepoID, repoRoot, env)
	if err != nil {
		return nil, err
	}

	pr, err := s.resolveMergePR(ctx, wtMeta, ghRepo, owner, repoRoot, env)
	if err != nil {
		return nil, err
	}
	agencyJSON, err := config.ResolveAgencyConfig(s.FS, repoRoot, s.ConfigDir, record.RepoID, req.AgencyConfigPath)
	if err != nil {
		return nil, err
	}

	mergeLogPath := filepath.Join(s.Store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID), "merge.log")
	plannedVerifyLogPath := filepath.Join(s.Store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID), "verify.log")
	archiveLogPath := filepath.Join(s.Store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID), "archive.log")
	if err := s.updateWorktreeMergeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
		m.Branch = wtMeta.Branch
		m.PRNumber = pr.Number
		m.PRURL = pr.URL
		m.MergeLogPath = mergeLogPath
		m.VerifyLogPath = plannedVerifyLogPath
		m.ArchiveLogPath = archiveLogPath
	}); err != nil {
		return nil, errors.Wrap(errors.EPersistFailed, "failed to persist merge plan state", err)
	}

	alreadyMerged := strings.EqualFold(strings.TrimSpace(pr.State), "MERGED")
	verifyLogPath := ""
	if !alreadyMerged {
		if err := s.updateWorktreeMergeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
			m.Stage = store.WorktreeMergeStageVerify
		}); err != nil {
			return nil, errors.Wrap(errors.EPersistFailed, "failed to persist merge verify stage", err)
		}

		clean, dirtyStatus, err := prSyncDirtyStatus(ctx, s.Runner, wtMeta.TreePath, env)
		if err != nil {
			return nil, err
		}
		if !clean {
			return nil, errors.NewWithDetails(
				errors.EDirtyWorktree,
				"worktree has uncommitted changes; merge requires a clean integration tree",
				map[string]string{
					"dirty_status": dirtyStatus,
					"hint":         "commit/stash/reset integration changes before merge",
				},
			)
		}

		verifyLogPath, err = s.runWorktreeMergeVerify(ctx, record, pr, repoRoot, agencyJSON.Config, profileEnv)
		if err != nil {
			return nil, err
		}
	}

	var unlock func() error
	deadline := s.Clock().Add(worktreeMergeRepoLockAcquireTimeout)
	for {
		unlock, err = s.repoLock.Lock(record.RepoID, "worktree_merge_finalize")
		if err == nil {
			break
		}
		var lockedErr *lock.ErrLocked
		if !stderrors.As(err, &lockedErr) {
			return nil, err
		}
		if !s.Clock().Before(deadline) {
			return nil, errors.NewWithDetails(
				errors.ERepoLocked,
				"repository remained locked while waiting to finalize merge",
				map[string]string{"hint": "wait for the active repo operation to finish, then rerun merge"},
			)
		}
		select {
		case <-ctx.Done():
			return nil, errors.Wrap(errors.EWorktreeMergeInterrupted, "merge interrupted while waiting for repository lock", ctx.Err())
		case <-time.After(worktreeMergeRepoLockPollInterval):
		}
	}
	defer func() { _ = unlock() }()

	pr, err = s.resolveMergePR(ctx, wtMeta, ghRepo, owner, repoRoot, env)
	if err != nil {
		return nil, err
	}
	if err := s.updateWorktreeMergeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
		m.PRNumber = pr.Number
		m.PRURL = pr.URL
	}); err != nil {
		return nil, errors.Wrap(errors.EPersistFailed, "failed to persist refreshed PR state", err)
	}

	alreadyMerged = strings.EqualFold(strings.TrimSpace(pr.State), "MERGED")
	if alreadyMerged {
		skippedCommand := fmt.Sprintf("gh pr merge %d -R %s --%s (skipped: already merged)", pr.Number, ghRepo, req.Strategy)
		if req.DeleteBranch {
			skippedCommand += " --delete-branch"
		}
		if err := writeMergeLog(s.FS, mergeLogPath, skippedCommand, exec.CmdResult{ExitCode: 0}, nil); err != nil {
			return nil, errors.WrapWithDetails(
				errors.EPersistFailed,
				"failed to persist merge log",
				err,
				map[string]string{
					"merge_log_path": mergeLogPath,
					"hint":           "inspect PR state and retry archive cleanup if needed",
				},
			)
		}
	} else {
		if err := s.updateWorktreeMergeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
			m.Stage = store.WorktreeMergeStageMerge
		}); err != nil {
			return nil, errors.Wrap(errors.EPersistFailed, "failed to persist merge stage", err)
		}

		args := []string{
			"pr", "merge", fmt.Sprintf("%d", pr.Number),
			"-R", ghRepo,
			"--" + string(req.Strategy),
		}
		if req.DeleteBranch {
			args = append(args, "--delete-branch")
		}
		result, runErr := s.Runner.Run(ctx, "gh", args, exec.RunOpts{
			Dir: wtMeta.TreePath,
			Env: env,
		})

		command := "gh " + strings.Join(args, " ")
		if err := writeMergeLog(s.FS, mergeLogPath, command, result, runErr); err != nil {
			return nil, errors.WrapWithDetails(
				errors.EPersistFailed,
				"failed to persist merge log",
				err,
				map[string]string{
					"merge_log_path": mergeLogPath,
					"hint":           "merge may have completed; inspect PR state and retry if needed",
				},
			)
		}
		if runErr != nil {
			if ctx.Err() != nil {
				return nil, errors.Wrap(errors.EWorktreeMergeInterrupted, "merge interrupted while running gh pr merge", ctx.Err())
			}
			return nil, errors.WrapWithDetails(
				errors.EGHPRMergeFailed,
				"gh pr merge failed to start",
				runErr,
				map[string]string{"command": command},
			)
		}
		if result.ExitCode != 0 {
			return nil, errors.NewWithDetails(
				errors.EGHPRMergeFailed,
				fmt.Sprintf("gh pr merge exited %d", result.ExitCode),
				map[string]string{
					"command":   command,
					"exit_code": fmt.Sprintf("%d", result.ExitCode),
					"stderr":    strings.TrimSpace(result.Stderr),
				},
			)
		}

		merged, err := mergeConfirmPRMerged(ctx, s.Runner, wtMeta.TreePath, ghRepo, pr.Number, env)
		if err != nil {
			return nil, err
		}
		if !merged {
			return nil, errors.NewWithDetails(
				errors.EGHPRMergeFailed,
				"gh pr merge succeeded but merged state could not be confirmed",
				map[string]string{
					"hint": "re-run merge command; if PR is already merged this invocation may have succeeded",
				},
			)
		}
	}

	if err := s.updateWorktreeMergeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
		m.Stage = store.WorktreeMergeStageArchive
	}); err != nil {
		return nil, errors.Wrap(errors.EPersistFailed, "failed to persist archive stage", err)
	}
	archiveLogPath, err = s.runWorktreeArchive(ctx, record, pr, repoRoot, agencyJSON.Config, profileEnv)
	if err != nil {
		return nil, err
	}

	return &mergeResult{
		Branch:         wtMeta.Branch,
		PRNumber:       pr.Number,
		PRURL:          pr.URL,
		Strategy:       req.Strategy,
		DeleteBranch:   req.DeleteBranch,
		MergeLogPath:   mergeLogPath,
		ArchiveLogPath: archiveLogPath,
		VerifyLog:      verifyLogPath,
	}, nil
}

func (s *Server) runWorktreeMergeVerify(
	ctx context.Context,
	record *store.IntegrationWorktreeRecord,
	pr *mergePRView,
	repoRoot string,
	agencyJSON config.AgencyConfig,
	profileEnv map[string]string,
) (string, error) {
	if record == nil || record.Meta == nil {
		return "", errors.New(errors.EInternal, "worktree metadata missing")
	}
	wtMeta := record.Meta

	worktreeDir := s.Store.IntegrationWorktreeDir(record.RepoID, record.WorktreeID)
	logsDir := s.Store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID)
	verifyLogPath := filepath.Join(logsDir, "verify.log")
	verifyRecordPath := s.Store.IntegrationWorktreeVerifyRecordPath(record.RepoID, record.WorktreeID)
	verifyJSONPath := filepath.Join(wtMeta.TreePath, ".agency", "out", "verify.json")

	env := buildWorktreeMergeScriptEnv(record, repoRoot, worktreeDir, pr, profileEnv)
	runCfg := verify.RunConfig{
		RepoID:         record.RepoID,
		RunID:          record.WorktreeID,
		WorkDir:        wtMeta.TreePath,
		Script:         agencyJSON.Scripts.Verify.Path,
		Env:            env,
		Timeout:        agencyJSON.Scripts.Verify.Timeout,
		LogPath:        verifyLogPath,
		VerifyJSONPath: verifyJSONPath,
		RecordPath:     verifyRecordPath,
	}

	verifyRecord, runErr := verify.Run(ctx, runCfg)
	if permsErr := s.ensureWorktreeVerifyLogPermissions(logsDir, verifyLogPath, runErr != nil); permsErr != nil {
		return "", permsErr
	}
	if runErr != nil {
		if ctx.Err() != nil {
			return "", errors.Wrap(errors.EWorktreeMergeInterrupted, "merge interrupted while running verify", ctx.Err())
		}
		return "", errors.Wrap(errors.EInternal, "verify runner failed", runErr)
	}
	if !verifyRecord.OK {
		return "", errors.NewWithDetails(
			errors.EScriptFailed,
			"verify failed; merge aborted",
			map[string]string{
				"verify_log_path": verifyLogPath,
				"hint":            "fix verify failures and retry merge",
			},
		)
	}

	return verifyLogPath, nil
}

func (s *Server) ensureWorktreeVerifyLogPermissions(logsDir, verifyLogPath string, allowMissing bool) error {
	if chmodDirErr := s.FS.Chmod(logsDir, 0o700); chmodDirErr != nil {
		if !allowMissing || !os.IsNotExist(chmodDirErr) {
			return errors.Wrap(errors.EPersistFailed, "failed to set verify log directory permissions", chmodDirErr)
		}
	}
	if chmodFileErr := s.FS.Chmod(verifyLogPath, 0o600); chmodFileErr != nil {
		if !allowMissing || !os.IsNotExist(chmodFileErr) {
			return errors.Wrap(errors.EPersistFailed, "failed to set verify log permissions", chmodFileErr)
		}
	}
	return nil
}

func (s *Server) runWorktreeArchive(
	ctx context.Context,
	record *store.IntegrationWorktreeRecord,
	pr *mergePRView,
	repoRoot string,
	agencyJSON config.AgencyConfig,
	profileEnv map[string]string,
) (string, error) {
	if record == nil || record.Meta == nil {
		return "", errors.New(errors.EInternal, "worktree metadata missing")
	}
	wtMeta := record.Meta

	logsDir := s.Store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID)
	archiveLogPath := filepath.Join(logsDir, "archive.log")
	treeExists := true
	if _, err := s.FS.Stat(wtMeta.TreePath); err != nil {
		if os.IsNotExist(err) {
			treeExists = false
		} else {
			return "", errors.Wrap(errors.EInternal, "failed to stat integration worktree", err)
		}
	}

	if !treeExists {
		if err := s.Store.UpdateIntegrationWorktreeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMeta) {
			m.State = store.WorktreeStateArchived
		}); err != nil {
			code := errors.CodeOr(err, errors.EMetaWriteFailed)
			return "", errors.Wrap(code, "failed to persist archived state", err)
		}
		if err := writeMergeLog(s.FS, archiveLogPath, "archive skipped: worktree already removed", exec.CmdResult{ExitCode: 0}, nil); err != nil {
			return "", errors.WrapWithDetails(
				errors.EPersistFailed,
				"failed to persist archive log",
				err,
				map[string]string{
					"archive_log_path": archiveLogPath,
					"hint":             "inspect archive cleanup state and retry if needed",
				},
			)
		}
		return archiveLogPath, nil
	}

	worktreeDir := s.Store.IntegrationWorktreeDir(record.RepoID, record.WorktreeID)
	envList := buildWorktreeMergeScriptEnv(record, repoRoot, worktreeDir, pr, profileEnv)
	env := make(map[string]string, len(envList))
	for _, entry := range envList {
		key, val, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			env[key] = val
		}
	}

	archiveCmd := agencyJSON.Scripts.Archive.Path
	runCtx, cancel := context.WithTimeout(ctx, agencyJSON.Scripts.Archive.Timeout)
	defer cancel()
	result, runErr := s.Runner.Run(runCtx, archiveCmd, nil, exec.RunOpts{
		Dir: wtMeta.TreePath,
		Env: env,
	})
	if err := writeMergeLog(s.FS, archiveLogPath, archiveCmd, result, runErr); err != nil {
		return "", errors.WrapWithDetails(
			errors.EPersistFailed,
			"failed to persist archive log",
			err,
			map[string]string{
				"archive_log_path": archiveLogPath,
				"hint":             "inspect archive cleanup state and retry if needed",
			},
		)
	}
	if runErr != nil {
		if ctx.Err() != nil {
			return "", errors.Wrap(errors.EWorktreeMergeInterrupted, "merge interrupted while running archive script", ctx.Err())
		}
		return "", errors.WrapWithDetails(
			errors.EArchiveFailed,
			"archive script failed to start",
			runErr,
			map[string]string{
				"archive_log_path": archiveLogPath,
				"command":          archiveCmd,
			},
		)
	}
	if result.ExitCode != 0 {
		return "", errors.NewWithDetails(
			errors.EArchiveFailed,
			fmt.Sprintf("archive script exited %d", result.ExitCode),
			map[string]string{
				"archive_log_path": archiveLogPath,
				"command":          archiveCmd,
				"exit_code":        fmt.Sprintf("%d", result.ExitCode),
				"hint":             "inspect archive.log, fix the archive step, and rerun worktree pr merge",
			},
		)
	}

	removeArgs := []string{"-C", repoRoot, "worktree", "remove", "--force", wtMeta.TreePath}
	removeCtx, cancel := context.WithTimeout(ctx, worktreeMergeArchiveRemoveTimeout)
	defer cancel()

	removeResult, removeRunErr := s.Runner.Run(removeCtx, "git", removeArgs, exec.RunOpts{Env: prSyncNonInteractiveEnv(profileEnv)})
	removeCmd := "git " + strings.Join(removeArgs, " ")
	appendArchiveSection := func(title string, result exec.CmdResult, runErr error) {
		logFile, err := os.OpenFile(archiveLogPath, os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return
		}
		defer func() { _ = logFile.Close() }()

		_, _ = fmt.Fprintln(logFile)
		_, _ = fmt.Fprintf(logFile, "=== %s ===\n", title)
		_, _ = fmt.Fprintf(logFile, "Exit code: %d\n", result.ExitCode)
		if strings.TrimSpace(result.Stdout) != "" {
			_, _ = fmt.Fprintln(logFile)
			_, _ = fmt.Fprintln(logFile, "=== stdout ===")
			_, _ = fmt.Fprint(logFile, result.Stdout)
			if !strings.HasSuffix(result.Stdout, "\n") {
				_, _ = fmt.Fprintln(logFile)
			}
		}
		if strings.TrimSpace(result.Stderr) != "" {
			_, _ = fmt.Fprintln(logFile)
			_, _ = fmt.Fprintln(logFile, "=== stderr ===")
			_, _ = fmt.Fprint(logFile, result.Stderr)
			if !strings.HasSuffix(result.Stderr, "\n") {
				_, _ = fmt.Fprintln(logFile)
			}
		}
		if runErr != nil {
			_, _ = fmt.Fprintln(logFile)
			_, _ = fmt.Fprintln(logFile, "=== execution_error ===")
			_, _ = fmt.Fprintln(logFile, runErr.Error())
		}
		_ = s.FS.Chmod(archiveLogPath, 0o600)
	}
	appendArchiveSection(removeCmd, removeResult, removeRunErr)
	if removeRunErr != nil {
		if ctx.Err() != nil {
			return "", errors.Wrap(errors.EWorktreeMergeInterrupted, "merge interrupted while removing archived worktree", ctx.Err())
		}
		if stderrors.Is(removeRunErr, context.DeadlineExceeded) {
			return "", errors.NewWithDetails(
				errors.EArchiveFailed,
				"git worktree remove timed out after archive cleanup",
				map[string]string{
					"archive_log_path": archiveLogPath,
					"command":          removeCmd,
					"hint":             "inspect archive.log, retry the merge cleanup, or remove the worktree manually if git is blocked",
				},
			)
		}
		return "", errors.WrapWithDetails(
			errors.EArchiveFailed,
			"git worktree remove failed to start",
			removeRunErr,
			map[string]string{
				"archive_log_path": archiveLogPath,
				"command":          removeCmd,
			},
		)
	}
	if removeResult.ExitCode != 0 {
		return "", errors.NewWithDetails(
			errors.EArchiveFailed,
			fmt.Sprintf("git worktree remove exited %d", removeResult.ExitCode),
			map[string]string{
				"archive_log_path": archiveLogPath,
				"command":          removeCmd,
				"exit_code":        fmt.Sprintf("%d", removeResult.ExitCode),
				"stderr":           strings.TrimSpace(removeResult.Stderr),
			},
		)
	}

	if err := s.Store.UpdateIntegrationWorktreeMeta(record.RepoID, record.WorktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.State = store.WorktreeStateArchived
	}); err != nil {
		appendArchiveSection("metadata", exec.CmdResult{
			Stdout: fmt.Sprintf("failed to persist archived state: %v\n", err),
		}, nil)
		code := errors.CodeOr(err, errors.EMetaWriteFailed)
		return "", errors.WrapWithDetails(
			code,
			"failed to persist archived worktree state",
			err,
			map[string]string{
				"archive_log_path": archiveLogPath,
				"hint":             "worktree removal completed; fix metadata persistence and rerun worktree pr merge to reconcile archived state",
			},
		)
	}

	return archiveLogPath, nil
}

func buildWorktreeMergeScriptEnv(
	record *store.IntegrationWorktreeRecord,
	repoRoot string,
	worktreeDir string,
	pr *mergePRView,
	profileEnv map[string]string,
) []string {
	name := ""
	runner := "worktree"
	workspaceRoot := ""
	branch := ""
	baseBranch := ""
	if record != nil && record.Meta != nil {
		name = strings.TrimSpace(record.Meta.Name)
		workspaceRoot = record.Meta.TreePath
		branch = record.Meta.Branch
		baseBranch = record.Meta.BaseBranch
	}
	if name == "" && record != nil {
		name = record.WorktreeID
	}

	return mergeflow.BuildVerifyEnv(exec.MergeEnv(os.Environ(), profileEnv), mergeflow.VerifyEnvInput{
		RunID:         record.WorktreeID,
		Name:          name,
		RepoRoot:      repoRoot,
		WorkspaceRoot: workspaceRoot,
		Branch:        branch,
		BaseBranch:    baseBranch,
		Runner:        runner,
		PRURL:         pr.URL,
		PRNumber:      pr.Number,
		InvocationDir: worktreeDir,
	})
}

func (s *Server) appendWorktreeEvent(repoID, worktreeID, kind string, data map[string]any) error {
	writer := s.WorktreeEvents
	if writer == nil {
		writer = eventlog.NewWriter("worktree_id", s.Clock)
		s.WorktreeEvents = writer
	}
	_, err := writer.Append(
		s.Store.IntegrationWorktreeEventsPath(repoID, worktreeID),
		worktreeID,
		kind,
		data,
		eventlog.AppendOptions{},
	)
	if err != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to append worktree event", err)
	}
	return nil
}
