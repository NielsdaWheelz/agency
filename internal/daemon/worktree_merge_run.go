package daemon

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/mergeflow"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/verify"
)

const (
	worktreeMergeRepoLockAcquireTimeout = 30 * time.Second
	worktreeMergeRepoLockPollInterval   = 50 * time.Millisecond
)

func (s *Server) runWorktreeMerge(
	ctx context.Context,
	record *store.IntegrationWorktreeRecord,
	req normalizedMergeRequest,
) (*mergeResult, error) {
	if record == nil || record.Meta == nil {
		return nil, errors.New(errors.EInternal, "worktree metadata missing")
	}
	wtMeta := record.Meta
	repoRoot, err := s.resolveMergeRepoRoot(record.RepoID, wtMeta.TreePath)
	if err != nil {
		return nil, err
	}
	profileEnv, err := s.executionProfileEnv(wtMeta.ExecutionProfile)
	if err != nil {
		return nil, err
	}
	env := withNonInteractiveEnv(profileEnv)

	if err := checkGHAuth(ctx, s.runner, repoRoot, env); err != nil {
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
	agencyJSON, err := config.ResolveAgencyConfig(s.fsys, repoRoot, s.configDir, record.RepoID, req.AgencyConfigPath)
	if err != nil {
		return nil, err
	}

	logsDir := s.store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID)
	mergeLogPath := filepath.Join(logsDir, "merge.log")
	plannedVerifyLogPath := filepath.Join(logsDir, "verify.log")
	archiveLogPath := filepath.Join(logsDir, "archive.log")
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

		clean, dirtyStatus, err := dirtyStatus(ctx, s.runner, wtMeta.TreePath, env)
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
	deadline := s.clock().Add(worktreeMergeRepoLockAcquireTimeout)
	for {
		unlock, err = s.repoLock.Lock(record.RepoID, "worktree_merge_finalize")
		if err == nil {
			break
		}
		var lockedErr *lock.ErrLocked
		if !stderrors.As(err, &lockedErr) {
			return nil, err
		}
		if !s.clock().Before(deadline) {
			return nil, errors.NewWithDetails(
				errors.ERepoLocked,
				"repository remained locked while waiting to finalize merge",
				map[string]string{"hint": "wait for the active repo operation to finish, then rerun merge"},
			)
		}
		if err := sleepCtx(ctx, worktreeMergeRepoLockPollInterval); err != nil {
			return nil, errors.Wrap(errors.EWorktreeMergeInterrupted, "merge interrupted while waiting for repository lock", err)
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
		if err := mergeflow.WriteMergeLog(s.fsys, mergeLogPath, skippedCommand, exec.CmdResult{ExitCode: 0}, nil); err != nil {
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
		result, runErr := s.runner.Run(ctx, "gh", args, exec.RunOpts{
			Dir: wtMeta.TreePath,
			Env: env,
		})

		command := "gh " + strings.Join(args, " ")
		if err := mergeflow.WriteMergeLog(s.fsys, mergeLogPath, command, result, runErr); err != nil {
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

		merged, err := mergeConfirmPRMerged(ctx, s.runner, wtMeta.TreePath, ghRepo, pr.Number, env)
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
		VerifyLogPath:  verifyLogPath,
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

	worktreeDir := s.store.IntegrationWorktreeDir(record.RepoID, record.WorktreeID)
	logsDir := s.store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID)
	verifyLogPath := filepath.Join(logsDir, "verify.log")

	env := buildWorktreeMergeScriptEnv(record, repoRoot, worktreeDir, pr, profileEnv)
	runCfg := verify.RunConfig{
		RepoID:  record.RepoID,
		RunID:   record.WorktreeID,
		WorkDir: wtMeta.TreePath,
		Script:  agencyJSON.Scripts.Verify.Path,
		Env:     env,
		Timeout: agencyJSON.Scripts.Verify.Timeout,
		LogPath: verifyLogPath,
		Clock:   s.clock,
	}

	verifyRecord, runErr := verify.Run(ctx, runCfg)
	if writeErr := s.store.WriteIntegrationWorktreeVerifyRecord(record.RepoID, record.WorktreeID, verifyRecord); writeErr != nil {
		return "", errors.Wrap(errors.EPersistFailed, "failed to persist verify record", writeErr)
	}
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
	if chmodDirErr := s.fsys.Chmod(logsDir, 0o700); chmodDirErr != nil {
		if !allowMissing || !os.IsNotExist(chmodDirErr) {
			return errors.Wrap(errors.EPersistFailed, "failed to set verify log directory permissions", chmodDirErr)
		}
	}
	if chmodFileErr := s.fsys.Chmod(verifyLogPath, 0o600); chmodFileErr != nil {
		if !allowMissing || !os.IsNotExist(chmodFileErr) {
			return errors.Wrap(errors.EPersistFailed, "failed to set verify log permissions", chmodFileErr)
		}
	}
	return nil
}
