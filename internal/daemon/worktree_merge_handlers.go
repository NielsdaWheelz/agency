package daemon

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon/worktreeevents"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/mergeflow"
	"github.com/NielsdaWheelz/agency/internal/report"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/verify"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// worktreeMergeArchiveRemoveTimeout bounds the git worktree removal performed after archive cleanup.
const worktreeMergeArchiveRemoveTimeout = 30 * time.Second

// handleWorktreePRMerge handles POST /worktrees/{ref}/pr/merge.
func (s *Server) handleWorktreePRMerge(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeWorktreeMergeError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidArgument),
			"repo_id query parameter is required",
			"pass ?repo_id=<repo_id>",
		)
		return
	}

	var req WorktreePRMergeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if err != io.EOF {
			s.writeWorktreeMergeError(
				w,
				http.StatusBadRequest,
				requestID,
				string(errors.EInvalidArgument),
				"invalid request body: "+err.Error(),
				"",
			)
			return
		}
	} else {
		var trailing json.RawMessage
		if err := dec.Decode(&trailing); err != io.EOF {
			s.writeWorktreeMergeError(
				w,
				http.StatusBadRequest,
				requestID,
				string(errors.EInvalidArgument),
				"invalid request body: expected a single JSON object",
				"",
			)
			return
		}
	}

	normalizedReq, err := normalizeMergeRequest(req)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInvalidArgument
		}
		s.writeWorktreeMergeError(w, mergeHTTPStatusForCode(code), requestID, string(code), err.Error(), mergeHintFromError(err))
		return
	}

	record, err := s.resolveWorktreeRef(worktreeRef, repoID)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeWorktreeMergeError(w, mergeHTTPStatusForCode(code), requestID, string(code), err.Error(), "use 'agency worktree ls' to list worktrees")
		return
	}
	if record == nil || record.Meta == nil {
		s.writeWorktreeMergeError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "worktree metadata missing", "")
		return
	}

	unlock, err := s.repoLock.Lock(record.RepoID, "worktree_merge")
	if err != nil {
		s.writeWorktreeMergeError(
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

	unresolved, err := s.unresolvedInvocationsForWorktree(record.RepoID, record.WorktreeID)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeWorktreeMergeError(w, mergeHTTPStatusForCode(code), requestID, string(code), err.Error(), mergeHintFromError(err))
		return
	}
	if len(unresolved) > 0 {
		s.writeWorktreeMergeError(
			w,
			http.StatusConflict,
			requestID,
			string(errors.EWorktreeHasUnresolvedInvocations),
			fmt.Sprintf("%d unresolved invocations exist for this worktree", len(unresolved)),
			"run 'agency agent ls --worktree "+worktreeRef+"' and land or discard each invocation",
		)
		return
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, mergeEventStarted, map[string]any{
		"strategy":          string(normalizedReq.Strategy),
		"confirmation_mode": normalizedReq.ConfirmationMode,
		"delete_branch":     normalizedReq.DeleteBranch,
		"branch":            record.Meta.Branch,
	}); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EPersistFailed
		}
		s.writeWorktreeMergeError(w, mergeHTTPStatusForCode(code), requestID, string(code), err.Error(), "")
		return
	}

	result, err := s.runWorktreeMerge(r.Context(), record, normalizedReq)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		if appendErr := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, mergeEventFailed, map[string]any{
			"error_code": string(code),
			"message":    err.Error(),
		}); appendErr != nil {
			appendCode := errors.GetCode(appendErr)
			if appendCode == "" {
				appendCode = errors.EPersistFailed
			}
			s.writeWorktreeMergeError(w, mergeHTTPStatusForCode(appendCode), requestID, string(appendCode), appendErr.Error(), "")
			return
		}
		s.writeWorktreeMergeError(w, mergeHTTPStatusForCode(code), requestID, string(code), err.Error(), mergeHintFromError(err))
		return
	}

	if err := s.appendWorktreeEvent(record.RepoID, record.WorktreeID, mergeEventSucceeded, map[string]any{
		"branch":           result.Branch,
		"pr_number":        result.PRNumber,
		"pr_url":           result.PRURL,
		"strategy":         string(result.Strategy),
		"delete_branch":    result.DeleteBranch,
		"merge_log_path":   result.MergeLogPath,
		"verify_log_path":  result.VerifyLog,
		"archive_log_path": result.ArchiveLogPath,
	}); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EPersistFailed
		}
		s.writeWorktreeMergeError(w, mergeHTTPStatusForCode(code), requestID, string(code), err.Error(), "")
		return
	}

	resp := WorktreePRMergeResponse{
		OK:                    true,
		APIVersion:            APIVersion,
		BuildVersion:          version.FullVersion(),
		RequestID:             requestID,
		RepoID:                record.RepoID,
		IntegrationWorktreeID: record.WorktreeID,
		Branch:                result.Branch,
		PRNumber:              result.PRNumber,
		PRURL:                 result.PRURL,
		Strategy:              string(result.Strategy),
		DeleteBranch:          result.DeleteBranch,
		MergeLogPath:          result.MergeLogPath,
		VerifyLogPath:         result.VerifyLog,
		ArchiveLogPath:        result.ArchiveLogPath,
		ReportSource:          result.ReportSource,
		ReportDiagnostics:     reportDiagnostics(result.ReportDiagnostics),
	}
	s.writeJSON(w, http.StatusOK, resp)
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

	if err := prSyncCheckGHAuth(ctx, s.Runner, repoRoot); err != nil {
		return nil, err
	}

	ghRepo, owner, err := s.resolveMergeGitHubRepo(ctx, record.RepoID, repoRoot)
	if err != nil {
		return nil, err
	}

	pr, err := s.resolveMergePR(ctx, wtMeta, ghRepo, owner, repoRoot)
	if err != nil {
		if errors.GetCode(err) == errors.ENoPR {
			_, reportViolation, reportErr := report.ResolveCanonicalReport(s.FS, wtMeta.TreePath, report.ResolveOptions{
				MaxBytes: report.MaxPRBodyReportBytes,
			})
			if reportErr != nil {
				return nil, errors.Wrap(errors.EInternal, "failed to evaluate report contract", reportErr)
			}
			if reportViolation != nil {
				return nil, reportViolationToAgencyError(reportViolation)
			}
		}
		return nil, err
	}

	mergeLogPath := filepath.Join(s.Store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID), "merge.log")
	alreadyMerged := strings.EqualFold(strings.TrimSpace(pr.State), "MERGED")
	var verifyLogPath string
	var reportSource string
	var reportDiagnostics []report.Diagnostic

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
		reportResolution, reportViolation, reportErr := report.ResolveCanonicalReport(s.FS, wtMeta.TreePath, report.ResolveOptions{
			MaxBytes: report.MaxPRBodyReportBytes,
		})
		if reportErr != nil {
			return nil, errors.Wrap(errors.EInternal, "failed to evaluate report contract", reportErr)
		}
		if reportViolation != nil {
			return nil, reportViolationToAgencyError(reportViolation)
		}
		if reportResolution == nil {
			return nil, errors.New(errors.EInternal, "report resolution produced no result")
		}
		reportSource = string(reportResolution.Source)
		reportDiagnostics = reportResolution.Diagnostics

		clean, dirtyStatus, err := prSyncDirtyStatus(ctx, s.Runner, wtMeta.TreePath)
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

		verifyLogPath, err = s.runWorktreeMergeVerify(ctx, record, pr, req.AgencyConfigPath)
		if err != nil {
			return nil, err
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
			Env: prSyncNonInteractiveEnv(),
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

		merged, err := mergeConfirmPRMerged(ctx, s.Runner, wtMeta.TreePath, ghRepo, pr.Number)
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

	archiveLogPath, err := s.runWorktreeArchive(ctx, record, pr, repoRoot, req.AgencyConfigPath)
	if err != nil {
		return nil, err
	}

	return &mergeResult{
		Branch:            wtMeta.Branch,
		PRNumber:          pr.Number,
		PRURL:             pr.URL,
		Strategy:          req.Strategy,
		DeleteBranch:      req.DeleteBranch,
		MergeLogPath:      mergeLogPath,
		ArchiveLogPath:    archiveLogPath,
		VerifyLog:         verifyLogPath,
		ReportSource:      reportSource,
		ReportDiagnostics: reportDiagnostics,
	}, nil
}

func (s *Server) runWorktreeMergeVerify(ctx context.Context, record *store.IntegrationWorktreeRecord, pr *mergePRView, agencyConfigPath string) (string, error) {
	if record == nil || record.Meta == nil {
		return "", errors.New(errors.EInternal, "worktree metadata missing")
	}
	wtMeta := record.Meta

	agencyJSON, err := config.ResolveAgencyConfig(s.FS, wtMeta.TreePath, s.ConfigDir, record.RepoID, agencyConfigPath)
	if err != nil {
		return "", err
	}

	worktreeDir := s.Store.IntegrationWorktreeDir(record.RepoID, record.WorktreeID)
	logsDir := s.Store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID)
	verifyLogPath := filepath.Join(logsDir, "verify.log")
	verifyRecordPath := filepath.Join(worktreeDir, "verify_record.json")
	verifyJSONPath := filepath.Join(wtMeta.TreePath, ".agency", "out", "verify.json")

	repoRoot, err := s.resolveMergeRepoRoot(ctx, record.RepoID, wtMeta.TreePath)
	if err != nil {
		return "", err
	}

	env := buildWorktreeMergeScriptEnv(record, repoRoot, worktreeDir, pr)
	runCfg := verify.RunConfig{
		RepoID:         record.RepoID,
		RunID:          record.WorktreeID,
		WorkDir:        wtMeta.TreePath,
		Script:         agencyJSON.Config.Scripts.Verify.Path,
		Env:            env,
		Timeout:        agencyJSON.Config.Scripts.Verify.Timeout,
		LogPath:        verifyLogPath,
		VerifyJSONPath: verifyJSONPath,
		RecordPath:     verifyRecordPath,
	}

	verifyRecord, runErr := verify.Run(ctx, runCfg)
	if permsErr := s.ensureWorktreeVerifyLogPermissions(logsDir, verifyLogPath, runErr != nil); permsErr != nil {
		return "", permsErr
	}
	if runErr != nil {
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
	agencyConfigPath string,
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
			return "", errors.Wrap(errors.EArchiveFailed, "failed to persist archived state", err)
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

	agencyJSON, err := config.ResolveAgencyConfig(s.FS, wtMeta.TreePath, s.ConfigDir, record.RepoID, agencyConfigPath)
	if err != nil {
		return "", err
	}

	worktreeDir := s.Store.IntegrationWorktreeDir(record.RepoID, record.WorktreeID)
	envList := buildWorktreeMergeScriptEnv(record, repoRoot, worktreeDir, pr)
	env := make(map[string]string, len(envList))
	for _, entry := range envList {
		key, val, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			env[key] = val
		}
	}

	archiveCmd := agencyJSON.Config.Scripts.Archive.Path
	runCtx, cancel := context.WithTimeout(ctx, agencyJSON.Config.Scripts.Archive.Timeout)
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

	removeResult, removeRunErr := s.Runner.Run(removeCtx, "git", removeArgs, exec.RunOpts{})
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
	}

	return archiveLogPath, nil
}

func buildWorktreeMergeScriptEnv(
	record *store.IntegrationWorktreeRecord,
	repoRoot string,
	worktreeDir string,
	pr *mergePRView,
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

	return mergeflow.BuildVerifyEnv(os.Environ(), mergeflow.VerifyEnvInput{
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
		writer = worktreeevents.NewWriter(s.Clock)
		s.WorktreeEvents = writer
	}
	_, err := writer.Append(
		s.Store.IntegrationWorktreeEventsPath(repoID, worktreeID),
		worktreeID,
		kind,
		data,
		worktreeevents.AppendOptions{},
	)
	if err != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to append worktree event", err)
	}
	return nil
}
