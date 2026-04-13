package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

// handleWorktreePRMerge handles POST /worktrees/{ref}/merge.
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
		"branch":          result.Branch,
		"pr_number":       result.PRNumber,
		"pr_url":          result.PRURL,
		"strategy":        string(result.Strategy),
		"delete_branch":   result.DeleteBranch,
		"merge_log_path":  result.MergeLogPath,
		"verify_log_path": result.VerifyLog,
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
		ReportSource:          result.ReportSource,
		ReportDiagnostics:     reportDiagnosticsToDaemon(result.ReportDiagnostics),
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
	reportSource := string(reportResolution.Source)
	reportDiagnostics := reportResolution.Diagnostics

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

	if err := prSyncCheckGHAuth(ctx, s.Runner, wtMeta.TreePath); err != nil {
		return nil, err
	}

	ghRepo, owner, err := s.resolveMergeGitHubRepo(ctx, record.RepoID, wtMeta.TreePath)
	if err != nil {
		return nil, err
	}

	pr, err := s.resolveMergePR(ctx, wtMeta, ghRepo, owner)
	if err != nil {
		return nil, err
	}

	verifyLogPath, err := s.runWorktreeMergeVerify(ctx, record, pr)
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

	mergeLogPath := filepath.Join(s.Store.IntegrationWorktreeLogsDir(record.RepoID, record.WorktreeID), "merge.log")
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

	return &mergeResult{
		Branch:            wtMeta.Branch,
		PRNumber:          pr.Number,
		PRURL:             pr.URL,
		Strategy:          req.Strategy,
		DeleteBranch:      req.DeleteBranch,
		MergeLogPath:      mergeLogPath,
		VerifyLog:         verifyLogPath,
		ReportSource:      reportSource,
		ReportDiagnostics: reportDiagnostics,
	}, nil
}

func (s *Server) runWorktreeMergeVerify(ctx context.Context, record *store.IntegrationWorktreeRecord, pr *mergePRView) (string, error) {
	if record == nil || record.Meta == nil {
		return "", errors.New(errors.EInternal, "worktree metadata missing")
	}
	wtMeta := record.Meta

	agencyJSON, err := config.LoadAgencyConfig(s.FS, wtMeta.TreePath)
	if err != nil {
		return "", errors.Wrap(errors.EInternal, "failed to load agency.json for verify", err)
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

	env := buildWorktreeMergeVerifyEnv(record, repoRoot, worktreeDir, pr)
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

func buildWorktreeMergeVerifyEnv(
	record *store.IntegrationWorktreeRecord,
	repoRoot string,
	worktreeDir string,
	pr *mergePRView,
) []string {
	name := ""
	runner := "worktree"
	workspaceRoot := ""
	branch := ""
	parentBranch := ""
	if record != nil && record.Meta != nil {
		name = strings.TrimSpace(record.Meta.Name)
		workspaceRoot = record.Meta.TreePath
		branch = record.Meta.Branch
		parentBranch = record.Meta.ParentBranch
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
		ParentBranch:  parentBranch,
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

func (s *Server) writeWorktreeMergeError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	resp := WorktreePRMergeResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		RequestID:    requestID,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	}
	s.writeJSON(w, status, resp)
}
