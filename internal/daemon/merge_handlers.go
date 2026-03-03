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
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/mergeflow"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/verify"
	"github.com/NielsdaWheelz/agency/internal/version"
)

const (
	mergeEventStarted   = "agency.merge_started"
	mergeEventSucceeded = "agency.merge_succeeded"
	mergeEventFailed    = "agency.merge_failed"

	mergeConfirmationYes   = "yes"
	mergeConfirmationTyped = "typed"
)

type mergeStrategy string

const (
	mergeStrategySquash mergeStrategy = "squash"
	mergeStrategyMerge  mergeStrategy = "merge"
	mergeStrategyRebase mergeStrategy = "rebase"
)

type normalizedMergeRequest struct {
	Strategy         mergeStrategy
	ConfirmationMode string
	DeleteBranch     bool
}

type mergeResult struct {
	Branch       string
	PRNumber     int
	PRURL        string
	Strategy     mergeStrategy
	DeleteBranch bool
	MergeLogPath string
	VerifyLog    string
}

type mergePRView struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	State       string `json:"state"`
	IsDraft     bool   `json:"isDraft"`
	Mergeable   string `json:"mergeable"`
	HeadRefName string `json:"headRefName"`
}

// handleMerge handles POST /invocations/{ref}/merge.
func (s *Server) handleMerge(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeMergeError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidArgument),
			"repo_id query parameter is required",
			"pass ?repo_id=<repo_id>",
		)
		return
	}

	var req MergeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if err != io.EOF {
			s.writeMergeError(
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
			s.writeMergeError(
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
		s.writeMergeError(w, mergeHTTPStatusForCode(code), requestID, string(code), err.Error(), mergeHintFromError(err))
		return
	}

	record, err := s.resolveInvocationRef(invocationRef, repoID)
	if err != nil {
		code := errors.GetCode(err)
		s.writeMergeError(w, mergeHTTPStatusForCode(code), requestID, string(code), err.Error(), "use 'agency agent ls' to list invocations")
		return
	}

	if record.Meta.Status == store.InvocationStatusStarting || record.Meta.Status == store.InvocationStatusRunning {
		s.writeMergeError(
			w,
			http.StatusConflict,
			requestID,
			string(errors.EInvocationStillRunning),
			"invocation is still running; wait for completion before merge",
			"run 'agency agent review <invocation_ref>' to check readiness",
		)
		return
	}
	if record.Meta.Status != store.InvocationStatusFinished {
		s.writeMergeError(
			w,
			http.StatusConflict,
			requestID,
			string(errors.EInvalidArgument),
			"invocation is not in a mergeable lifecycle state",
			"run 'agency agent review <invocation_ref>' for blocking reasons",
		)
		return
	}
	if record.Meta.LandingStatus != store.LandingStatusLanded {
		s.writeMergeError(
			w,
			http.StatusConflict,
			requestID,
			string(errors.EInvalidArgument),
			"invocation must be landed before merge",
			"run 'agency agent land <invocation_ref>' first",
		)
		return
	}

	wtMeta, err := s.Store.ReadIntegrationWorktreeMeta(record.RepoID, record.Meta.IntegrationWorktreeID)
	if err != nil {
		s.writeMergeError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to read integration worktree metadata", "")
		return
	}

	unlock, err := s.repoLock.Lock(record.RepoID, "merge")
	if err != nil {
		s.writeMergeError(
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

	if err := s.appendMergeEvent(record.RepoID, record.InvocationID, mergeEventStarted, map[string]any{
		"strategy":          string(normalizedReq.Strategy),
		"confirmation_mode": normalizedReq.ConfirmationMode,
		"delete_branch":     normalizedReq.DeleteBranch,
		"branch":            wtMeta.Branch,
	}); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EPersistFailed
		}
		s.writeMergeError(w, mergeHTTPStatusForCode(code), requestID, string(code), err.Error(), "")
		return
	}

	result, err := s.runMerge(r.Context(), record, wtMeta, normalizedReq)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		if appendErr := s.appendMergeEvent(record.RepoID, record.InvocationID, mergeEventFailed, map[string]any{
			"error_code": string(code),
			"message":    err.Error(),
		}); appendErr != nil {
			appendCode := errors.GetCode(appendErr)
			if appendCode == "" {
				appendCode = errors.EPersistFailed
			}
			s.writeMergeError(w, mergeHTTPStatusForCode(appendCode), requestID, string(appendCode), appendErr.Error(), "")
			return
		}
		s.writeMergeError(w, mergeHTTPStatusForCode(code), requestID, string(code), err.Error(), mergeHintFromError(err))
		return
	}

	if err := s.appendMergeEvent(record.RepoID, record.InvocationID, mergeEventSucceeded, map[string]any{
		"branch":         result.Branch,
		"pr_number":      result.PRNumber,
		"pr_url":         result.PRURL,
		"strategy":       string(result.Strategy),
		"delete_branch":  result.DeleteBranch,
		"merge_log_path": result.MergeLogPath,
	}); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EPersistFailed
		}
		s.writeMergeError(w, mergeHTTPStatusForCode(code), requestID, string(code), err.Error(), "")
		return
	}

	resp := MergeResponse{
		OK:                    true,
		APIVersion:            APIVersion,
		BuildVersion:          version.FullVersion(),
		RequestID:             requestID,
		InvocationID:          record.InvocationID,
		RepoID:                record.RepoID,
		IntegrationWorktreeID: record.Meta.IntegrationWorktreeID,
		Branch:                result.Branch,
		PRNumber:              result.PRNumber,
		PRURL:                 result.PRURL,
		Strategy:              string(result.Strategy),
		DeleteBranch:          result.DeleteBranch,
		MergeLogPath:          result.MergeLogPath,
		VerifyLogPath:         result.VerifyLog,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func normalizeMergeRequest(req MergeRequest) (normalizedMergeRequest, error) {
	mode := strings.TrimSpace(req.ConfirmationMode)
	if mode != mergeConfirmationYes && mode != mergeConfirmationTyped {
		return normalizedMergeRequest{}, errors.NewWithDetails(
			errors.EInvalidArgument,
			"confirmation_mode must be one of: yes, typed",
			map[string]string{
				"hint": "for non-interactive automation, pass --yes",
			},
		)
	}
	if !req.Confirmed {
		return normalizedMergeRequest{}, errors.NewWithDetails(
			errors.EConfirmationRequired,
			"merge requires explicit confirmation",
			map[string]string{
				"hint": "re-run with --yes (non-interactive) or provide interactive typed confirmation",
			},
		)
	}

	strategy := mergeStrategy(strings.TrimSpace(req.Strategy))
	if strategy == "" {
		strategy = mergeStrategySquash
	}
	switch strategy {
	case mergeStrategySquash, mergeStrategyMerge, mergeStrategyRebase:
	default:
		return normalizedMergeRequest{}, errors.NewWithDetails(
			errors.EInvalidArgument,
			"strategy must be one of: squash, merge, rebase",
			map[string]string{
				"strategy": string(strategy),
			},
		)
	}

	return normalizedMergeRequest{
		Strategy:         strategy,
		ConfirmationMode: mode,
		DeleteBranch:     !req.NoDeleteBranch,
	}, nil
}

func (s *Server) runMerge(
	ctx context.Context,
	record *resolvedInvocation,
	wtMeta *store.IntegrationWorktreeMeta,
	req normalizedMergeRequest,
) (*mergeResult, error) {
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

	verifyLogPath, err := s.runMergeVerify(ctx, record, wtMeta, pr)
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

	mergeLogPath := filepath.Join(s.Store.InvocationDir(record.RepoID, record.InvocationID), "merge.log")
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
		Branch:       wtMeta.Branch,
		PRNumber:     pr.Number,
		PRURL:        pr.URL,
		Strategy:     req.Strategy,
		DeleteBranch: req.DeleteBranch,
		MergeLogPath: mergeLogPath,
		VerifyLog:    verifyLogPath,
	}, nil
}

func (s *Server) resolveMergePR(ctx context.Context, wtMeta *store.IntegrationWorktreeMeta, ghRepo, owner string) (*mergePRView, error) {
	prs, err := mergeListPRsForBranchWithRetry(ctx, s.Runner, wtMeta.TreePath, owner, wtMeta.Branch)
	if err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, errors.NewWithDetails(
			errors.ENoPR,
			fmt.Sprintf("no pull request found for branch %q", wtMeta.Branch),
			map[string]string{
				"branch": wtMeta.Branch,
				"hint":   "run 'agency agent pr sync <invocation_ref>' first",
			},
		)
	}
	if len(prs) > 1 {
		return nil, errors.NewWithDetails(
			errors.EGHPRViewFailed,
			fmt.Sprintf("multiple PRs found for branch %q", wtMeta.Branch),
			map[string]string{
				"branch": wtMeta.Branch,
				"count":  fmt.Sprintf("%d", len(prs)),
			},
		)
	}

	pr, err := mergeViewPR(ctx, s.Runner, wtMeta.TreePath, ghRepo, prs[0].Number)
	if err != nil {
		return nil, err
	}
	if strings.ToUpper(pr.State) != "OPEN" {
		return nil, errors.NewWithDetails(
			errors.EPRNotOpen,
			fmt.Sprintf("PR #%d exists but state is %s (expected OPEN)", pr.Number, pr.State),
			map[string]string{
				"pr_number": fmt.Sprintf("%d", pr.Number),
				"state":     pr.State,
			},
		)
	}
	if pr.IsDraft {
		return nil, errors.NewWithDetails(
			errors.EPRDraft,
			fmt.Sprintf("PR #%d is in draft state", pr.Number),
			map[string]string{"pr_number": fmt.Sprintf("%d", pr.Number)},
		)
	}
	if strings.TrimSpace(pr.HeadRefName) != strings.TrimSpace(wtMeta.Branch) {
		return nil, errors.NewWithDetails(
			errors.EPRMismatch,
			fmt.Sprintf("PR head branch mismatch: expected %q, got %q", wtMeta.Branch, pr.HeadRefName),
			map[string]string{
				"expected_branch": wtMeta.Branch,
				"actual_branch":   pr.HeadRefName,
			},
		)
	}
	if err := mergeEnsureMergeable(ctx, s.Runner, wtMeta.TreePath, ghRepo, pr.Number); err != nil {
		return nil, err
	}

	return pr, nil
}

func mergeListPRsForBranchWithRetry(ctx context.Context, runner exec.CommandRunner, workDir, owner, branch string) ([]prSyncPR, error) {
	headWithOwner := prSyncHeadRef(owner, branch)
	prs, err := mergeListPRsByHeadWithRetry(ctx, runner, workDir, headWithOwner)
	if err != nil {
		return nil, err
	}
	if len(prs) > 0 {
		return prs, nil
	}

	// GitHub can surface head refs without owner prefix for same-repo branches.
	if strings.TrimSpace(owner) != "" {
		prs, err = mergeListPRsByHeadWithRetry(ctx, runner, workDir, branch)
		if err != nil {
			return nil, err
		}
	}
	return prs, nil
}

func mergeListPRsByHeadWithRetry(ctx context.Context, runner exec.CommandRunner, workDir, head string) ([]prSyncPR, error) {
	delays := []time.Duration{
		0,
		250 * time.Millisecond,
		750 * time.Millisecond,
		1500 * time.Millisecond,
		3 * time.Second,
	}

	var prs []prSyncPR
	for idx, delay := range delays {
		if idx > 0 && delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, errors.Wrap(errors.ENoPR, "context canceled while waiting for PR visibility", ctx.Err())
			case <-timer.C:
			}
		}

		var err error
		prs, err = prSyncListPRsByHead(ctx, runner, workDir, head)
		if err != nil {
			return nil, err
		}
		if len(prs) > 0 {
			return prs, nil
		}
	}

	return prs, nil
}

func mergeViewPR(ctx context.Context, runner exec.CommandRunner, workDir, ghRepo string, prNumber int) (*mergePRView, error) {
	result, err := runner.Run(ctx, "gh", []string{
		"pr", "view", fmt.Sprintf("%d", prNumber),
		"-R", ghRepo,
		"--json", "number,url,state,isDraft,mergeable,headRefName",
	}, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil {
		return nil, errors.Wrap(errors.EGHPRViewFailed, "gh pr view failed to start", err)
	}
	if result.ExitCode != 0 {
		return nil, errors.NewWithDetails(
			errors.EGHPRViewFailed,
			fmt.Sprintf("gh pr view failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{
				"pr_number": fmt.Sprintf("%d", prNumber),
				"exit_code": fmt.Sprintf("%d", result.ExitCode),
			},
		)
	}

	var pr mergePRView
	if err := json.Unmarshal([]byte(result.Stdout), &pr); err != nil {
		return nil, errors.Wrap(errors.EGHPRViewFailed, "failed to parse gh pr view output", err)
	}
	if pr.Number == 0 || strings.TrimSpace(pr.URL) == "" || strings.TrimSpace(pr.State) == "" {
		return nil, errors.New(errors.EGHPRViewFailed, "gh pr view output missing required fields")
	}
	return &pr, nil
}

func mergeEnsureMergeable(ctx context.Context, runner exec.CommandRunner, workDir, ghRepo string, prNumber int) error {
	delays := []time.Duration{0, 250 * time.Millisecond, 750 * time.Millisecond, 1500 * time.Millisecond}

	for idx, delay := range delays {
		if idx > 0 && delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return errors.Wrap(errors.EPRMergeabilityUnknown, "context canceled while waiting for mergeability", ctx.Err())
			case <-timer.C:
			}
		}

		pr, err := mergeViewPR(ctx, runner, workDir, ghRepo, prNumber)
		if err != nil {
			continue
		}
		mergeable := strings.ToUpper(strings.TrimSpace(pr.Mergeable))
		switch mergeable {
		case "MERGEABLE":
			return nil
		case "CONFLICTING":
			return errors.NewWithDetails(
				errors.EPRNotMergeable,
				fmt.Sprintf("PR #%d is not mergeable (conflicting)", prNumber),
				map[string]string{
					"pr_number": fmt.Sprintf("%d", prNumber),
				},
			)
		case "UNKNOWN", "":
			continue
		default:
			return errors.NewWithDetails(
				errors.EPRMergeabilityUnknown,
				fmt.Sprintf("unexpected mergeable value %q", mergeable),
				map[string]string{
					"pr_number": fmt.Sprintf("%d", prNumber),
					"mergeable": mergeable,
				},
			)
		}
	}

	return errors.NewWithDetails(
		errors.EPRMergeabilityUnknown,
		fmt.Sprintf("mergeability for PR #%d remained UNKNOWN after retries", prNumber),
		map[string]string{"pr_number": fmt.Sprintf("%d", prNumber)},
	)
}

func (s *Server) runMergeVerify(ctx context.Context, record *resolvedInvocation, wtMeta *store.IntegrationWorktreeMeta, pr *mergePRView) (string, error) {
	agencyJSON, err := config.LoadAgencyConfig(s.FS, wtMeta.TreePath)
	if err != nil {
		return "", errors.Wrap(errors.EInternal, "failed to load agency.json for verify", err)
	}

	invocationDir := s.Store.InvocationDir(record.RepoID, record.InvocationID)
	verifyLogPath := filepath.Join(invocationDir, "logs", "verify.log")
	verifyRecordPath := filepath.Join(invocationDir, "verify_record.json")
	verifyJSONPath := filepath.Join(wtMeta.TreePath, ".agency", "out", "verify.json")

	repoRoot, err := s.resolveMergeRepoRoot(ctx, record.RepoID, wtMeta.TreePath)
	if err != nil {
		return "", err
	}

	env := buildMergeVerifyEnv(record, wtMeta, repoRoot, invocationDir, pr)
	runCfg := verify.RunConfig{
		RepoID:         record.RepoID,
		RunID:          record.InvocationID,
		WorkDir:        wtMeta.TreePath,
		Script:         agencyJSON.Scripts.Verify.Path,
		Env:            env,
		Timeout:        agencyJSON.Scripts.Verify.Timeout,
		LogPath:        verifyLogPath,
		VerifyJSONPath: verifyJSONPath,
		RecordPath:     verifyRecordPath,
	}

	verifyRecord, runErr := verify.Run(ctx, runCfg)
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

func buildMergeVerifyEnv(
	record *resolvedInvocation,
	wtMeta *store.IntegrationWorktreeMeta,
	repoRoot string,
	invocationDir string,
	pr *mergePRView,
) []string {
	invocationName := strings.TrimSpace(record.Meta.InvocationName)
	if invocationName == "" {
		invocationName = record.InvocationID
	}

	return mergeflow.BuildVerifyEnv(os.Environ(), mergeflow.VerifyEnvInput{
		RunID:         record.InvocationID,
		Name:          invocationName,
		RepoRoot:      repoRoot,
		WorkspaceRoot: wtMeta.TreePath,
		Branch:        wtMeta.Branch,
		ParentBranch:  wtMeta.ParentBranch,
		Runner:        record.Meta.Runner,
		PRURL:         pr.URL,
		PRNumber:      pr.Number,
		InvocationDir: invocationDir,
	})
}

func (s *Server) resolveMergeRepoRoot(ctx context.Context, repoID, workspaceRoot string) (string, error) {
	return mergeflow.ResolveRepoRoot(ctx, s.Runner, s.Store, repoID, workspaceRoot)
}

func (s *Server) resolveMergeGitHubRepo(ctx context.Context, repoID, workDir string) (string, string, error) {
	originURL := ""
	if repoRecord, exists, err := s.Store.LoadRepoRecord(repoID); err == nil && exists {
		originURL = strings.TrimSpace(repoRecord.OriginURL)
	}
	if originURL == "" {
		result, err := s.Runner.Run(ctx, "git", []string{"config", "--get", "remote.origin.url"}, exec.RunOpts{
			Dir: workDir,
			Env: prSyncNonInteractiveEnv(),
		})
		if err != nil || result.ExitCode != 0 {
			return "", "", errors.New(errors.EGHRepoParseFailed, "failed to determine GitHub repository from origin remote")
		}
		originURL = strings.TrimSpace(result.Stdout)
	}

	owner, repo, ok := identity.ParseGitHubOwnerRepo(originURL)
	if !ok {
		return "", "", errors.NewWithDetails(
			errors.EGHRepoParseFailed,
			"failed to parse owner/repo from origin URL",
			map[string]string{"origin_url": originURL},
		)
	}
	return owner + "/" + repo, owner, nil
}

func mergeConfirmPRMerged(ctx context.Context, runner exec.CommandRunner, workDir, ghRepo string, prNumber int) (bool, error) {
	delays := []time.Duration{0, 250 * time.Millisecond, 750 * time.Millisecond, 1500 * time.Millisecond}

	for idx, delay := range delays {
		if idx > 0 && delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false, errors.Wrap(errors.EGHPRMergeFailed, "context canceled while confirming merged state", ctx.Err())
			case <-timer.C:
			}
		}

		result, err := runner.Run(ctx, "gh", []string{
			"pr", "view", fmt.Sprintf("%d", prNumber),
			"-R", ghRepo,
			"--json", "state",
		}, exec.RunOpts{
			Dir: workDir,
			Env: prSyncNonInteractiveEnv(),
		})
		if err != nil || result.ExitCode != 0 {
			continue
		}

		var state struct {
			State string `json:"state"`
		}
		if jsonErr := json.Unmarshal([]byte(result.Stdout), &state); jsonErr != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(state.State), "MERGED") {
			return true, nil
		}
	}

	return false, nil
}

func writeMergeLog(fsys agencyfs.FS, mergeLogPath, command string, result exec.CmdResult, runErr error) error {
	return mergeflow.WriteMergeLog(fsys, mergeLogPath, command, result, runErr)
}

func canonicalizePath(path string) string {
	return mergeflow.CanonicalizePath(path)
}

func (s *Server) appendMergeEvent(repoID, invocationID, kind string, data map[string]any) error {
	writer := s.InvocationEvents
	if writer == nil {
		writer = invocationevents.NewWriter(s.Clock)
	}
	_, err := writer.Append(
		s.Store.InvocationEventsPath(repoID, invocationID),
		invocationID,
		kind,
		data,
		invocationevents.AppendOptions{},
	)
	if err != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to append invocation event", err)
	}
	return nil
}

func mergeHintFromError(err error) string {
	if ae, ok := errors.AsAgencyError(err); ok && ae.Details != nil {
		if hint := strings.TrimSpace(ae.Details["hint"]); hint != "" {
			return hint
		}
	}
	return ""
}

func mergeHTTPStatusForCode(code errors.Code) int {
	switch code {
	case errors.EInvocationNotFound, errors.ENoPR:
		return http.StatusNotFound
	case errors.EInvocationIDAmbiguous:
		return http.StatusConflict
	case errors.ERepoLocked:
		return http.StatusConflict
	case errors.EInvocationStillRunning:
		return http.StatusConflict
	case errors.EConfirmationRequired:
		return http.StatusConflict
	case errors.EDirtyWorktree:
		return http.StatusConflict
	case errors.EPRNotOpen, errors.EPRDraft, errors.EPRMismatch, errors.EPRNotMergeable, errors.EPRMergeabilityUnknown:
		return http.StatusConflict
	case errors.EGHPRMergeFailed, errors.EGHPRViewFailed:
		return http.StatusConflict
	case errors.EScriptFailed:
		return http.StatusConflict
	case errors.EInvalidArgument, errors.EGHRepoParseFailed, errors.EGhNotInstalled, errors.EGhNotAuthenticated:
		return http.StatusBadRequest
	case errors.EPersistFailed:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func (s *Server) writeMergeError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	resp := MergeResponse{
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
