package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/render"
	"github.com/NielsdaWheelz/agency/internal/report"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

const (
	prSyncEventStarted   = "agency.pr_sync_started"
	prSyncEventSucceeded = "agency.pr_sync_succeeded"
	prSyncEventFailed    = "agency.pr_sync_failed"

	prSyncMaxReportBytes = report.MaxPRBodyReportBytes
)

type prSyncResult struct {
	Branch             string
	PRNumber           int
	PRURL              string
	PRAction           string
	ReportSource       string
	ReportFallbackUsed bool
	ReportDiagnostics  []report.Diagnostic
}

type prSyncPR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

// handlePRSync handles POST /invocations/{ref}/pr/sync.
func (s *Server) handlePRSync(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writePRSyncError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	req, decodeErr := decodePRSyncRequest(r.Body)
	if decodeErr != "" {
		s.writePRSyncError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidArgument),
			decodeErr,
			"",
		)
		return
	}

	record, err := s.resolveInvocationRef(invocationRef, repoID)
	if err != nil {
		code := errors.GetCode(err)
		status := prSyncHTTPStatusForCode(code)
		s.writePRSyncError(w, status, requestID, string(code), err.Error(), "use 'agency agent ls' to list invocations")
		return
	}
	if record.Meta.LandingStatus != store.LandingStatusLanded {
		s.writePRSyncError(
			w,
			http.StatusConflict,
			requestID,
			"E_INVALID_REQUEST",
			"invocation must be landed before PR sync",
			"run 'agency agent land <invocation_ref>' first",
		)
		return
	}

	wtMeta, err := s.Store.ReadIntegrationWorktreeMeta(record.RepoID, record.Meta.IntegrationWorktreeID)
	if err != nil {
		s.writePRSyncError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to read integration worktree metadata", "")
		return
	}

	unlock, err := s.repoLock.Lock(record.RepoID, "pr_sync")
	if err != nil {
		s.writePRSyncError(
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

	if err := s.appendPRSyncEvent(record.RepoID, record.InvocationID, prSyncEventStarted, map[string]any{
		"allow_dirty":      req.AllowDirty,
		"force_with_lease": req.ForceWithLease,
		"branch":           wtMeta.Branch,
	}); err != nil {
		s.writePRSyncError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), err.Error(), "")
		return
	}

	result, err := s.runPRSync(r.Context(), record, wtMeta, req)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		hint := prSyncHintFromError(err)

		if appendErr := s.appendPRSyncEvent(record.RepoID, record.InvocationID, prSyncEventFailed, map[string]any{
			"error_code": string(code),
			"message":    err.Error(),
		}); appendErr != nil {
			s.writePRSyncError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), appendErr.Error(), "")
			return
		}

		s.writePRSyncError(w, prSyncHTTPStatusForCode(code), requestID, string(code), err.Error(), hint)
		return
	}

	if err := s.appendPRSyncEvent(record.RepoID, record.InvocationID, prSyncEventSucceeded, map[string]any{
		"branch":    result.Branch,
		"pr_number": result.PRNumber,
		"pr_url":    result.PRURL,
		"pr_action": result.PRAction,
	}); err != nil {
		s.writePRSyncError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), err.Error(), "")
		return
	}

	resp := PRSyncResponse{
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
		PRAction:              result.PRAction,
		ReportSource:          result.ReportSource,
		ReportFallbackUsed:    result.ReportFallbackUsed,
		ReportDiagnostics:     reportDiagnosticsToDaemon(result.ReportDiagnostics),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) runPRSync(
	ctx context.Context,
	record *resolvedInvocation,
	wtMeta *store.IntegrationWorktreeMeta,
	req PRSyncRequest,
) (*prSyncResult, error) {
	clean, dirtyStatus, err := prSyncDirtyStatus(ctx, s.Runner, wtMeta.TreePath)
	if err != nil {
		return nil, err
	}
	if !clean && !req.AllowDirty {
		return nil, errors.NewWithDetails(
			errors.EDirtyWorktree,
			"worktree has uncommitted changes; use --allow-dirty to proceed",
			map[string]string{
				"dirty_status": dirtyStatus,
				"hint":         "retry with --allow-dirty if you intentionally want to proceed",
			},
		)
	}

	if err := prSyncCheckGHAuth(ctx, s.Runner, wtMeta.TreePath); err != nil {
		return nil, err
	}
	if err := prSyncGitFetchOrigin(ctx, s.Runner, wtMeta.TreePath); err != nil {
		return nil, err
	}

	parentRef, err := prSyncResolveParentRef(ctx, s.Runner, wtMeta.TreePath, wtMeta.ParentBranch)
	if err != nil {
		return nil, err
	}
	ahead, err := prSyncComputeAhead(ctx, s.Runner, wtMeta.TreePath, parentRef, wtMeta.Branch)
	if err != nil {
		return nil, err
	}
	if ahead == 0 {
		return nil, errors.New(errors.EEmptyDiff, "no commits ahead of parent; make at least one commit")
	}

	bodyPath, reportSource, reportFallbackUsed, reportDiagnostics, err := prSyncPrepareBody(
		s.FS,
		wtMeta.TreePath,
		record.InvocationID,
		record.Meta.Mode == store.RunnerModeHeadless,
	)
	if err != nil {
		return nil, err
	}

	if err := prSyncGitPush(ctx, s.Runner, wtMeta.TreePath, wtMeta.Branch, req.ForceWithLease); err != nil {
		return nil, err
	}

	owner, err := prSyncResolveGitHubOwner(ctx, s.Runner, wtMeta.TreePath)
	if err != nil {
		return nil, err
	}
	head := prSyncHeadRef(owner, wtMeta.Branch)
	title := strings.TrimSpace(wtMeta.Name)
	if title == "" {
		title = wtMeta.Branch
	}
	title = "[agency] " + title
	prs, err := prSyncListPRsByHead(ctx, s.Runner, wtMeta.TreePath, head)
	if err != nil {
		return nil, err
	}

	if len(prs) > 1 {
		return nil, errors.NewWithDetails(
			errors.EGHPRViewFailed,
			fmt.Sprintf("multiple PRs found for head %q", head),
			map[string]string{"head": head, "count": fmt.Sprintf("%d", len(prs))},
		)
	}

	if len(prs) == 0 {
		createErr := prSyncCreatePR(ctx, s.Runner, wtMeta.TreePath, wtMeta.ParentBranch, wtMeta.Branch, title, bodyPath)
		createdNow := createErr == nil
		if createErr != nil && !prSyncIsAlreadyExistsError(createErr) {
			return nil, createErr
		}

		pr, lookupErr := prSyncLookupPRAfterCreateWithRetry(ctx, s.Runner, wtMeta.TreePath, owner, wtMeta.Branch)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if pr == nil {
			return nil, errors.NewWithDetails(
				errors.EGHPRViewFailed,
				"failed to resolve PR after create",
				map[string]string{"head": head, "branch": wtMeta.Branch},
			)
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

		// Newly created PR already has body from --body-file.
		if createdNow {
			return &prSyncResult{
				Branch:             wtMeta.Branch,
				PRNumber:           pr.Number,
				PRURL:              pr.URL,
				PRAction:           "created",
				ReportSource:       reportSource,
				ReportFallbackUsed: reportFallbackUsed,
				ReportDiagnostics:  reportDiagnostics,
			}, nil
		}

		// PR already existed (create raced/failed as already exists): update body.
		if err := prSyncEditPRBody(ctx, s.Runner, wtMeta.TreePath, pr.Number, bodyPath); err != nil {
			return nil, err
		}
		return &prSyncResult{
			Branch:             wtMeta.Branch,
			PRNumber:           pr.Number,
			PRURL:              pr.URL,
			PRAction:           "updated",
			ReportSource:       reportSource,
			ReportFallbackUsed: reportFallbackUsed,
			ReportDiagnostics:  reportDiagnostics,
		}, nil
	}

	pr := prs[0]
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
	if err := prSyncEditPRBody(ctx, s.Runner, wtMeta.TreePath, pr.Number, bodyPath); err != nil {
		return nil, err
	}

	return &prSyncResult{
		Branch:             wtMeta.Branch,
		PRNumber:           pr.Number,
		PRURL:              pr.URL,
		PRAction:           "updated",
		ReportSource:       reportSource,
		ReportFallbackUsed: reportFallbackUsed,
		ReportDiagnostics:  reportDiagnostics,
	}, nil
}

func prSyncLookupPRAfterCreate(ctx context.Context, runner exec.CommandRunner, workDir, owner, branch string) (*prSyncPR, error) {
	head := prSyncHeadRef(owner, branch)
	prs, err := prSyncListPRsByHead(ctx, runner, workDir, head)
	if err != nil {
		return nil, err
	}
	if len(prs) == 0 && strings.TrimSpace(owner) != "" {
		prs, err = prSyncListPRsByHead(ctx, runner, workDir, branch)
		if err != nil {
			return nil, err
		}
	}
	if len(prs) == 0 {
		return nil, nil
	}
	if len(prs) > 1 {
		return nil, errors.NewWithDetails(
			errors.EGHPRViewFailed,
			fmt.Sprintf("multiple PRs found for branch %q", branch),
			map[string]string{"branch": branch, "count": fmt.Sprintf("%d", len(prs))},
		)
	}
	return &prs[0], nil
}

func prSyncLookupPRAfterCreateWithRetry(ctx context.Context, runner exec.CommandRunner, workDir, owner, branch string) (*prSyncPR, error) {
	delays := []time.Duration{
		0,
		250 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
	}

	var lastErr error
	for i, delay := range delays {
		if i > 0 && delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		pr, err := prSyncLookupPRAfterCreate(ctx, runner, workDir, owner, branch)
		if err != nil {
			lastErr = err
			continue
		}
		if pr != nil {
			return pr, nil
		}
	}

	return nil, lastErr
}

func prSyncIsAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "pull request") && strings.Contains(lower, "already exists")
}

func prSyncDirtyStatus(ctx context.Context, runner exec.CommandRunner, workDir string) (bool, string, error) {
	result, err := runner.Run(ctx, "git", []string{"status", "--porcelain", "--untracked-files=all"}, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil {
		return false, "", errors.Wrap(errors.EInternal, "git status --porcelain failed to start", err)
	}
	if result.ExitCode != 0 {
		return false, "", errors.NewWithDetails(
			errors.EInternal,
			fmt.Sprintf("git status --porcelain failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{"exit_code": fmt.Sprintf("%d", result.ExitCode)},
		)
	}

	status := strings.TrimRight(result.Stdout, "\n")
	return strings.TrimSpace(status) == "", status, nil
}

func prSyncCheckGHAuth(ctx context.Context, runner exec.CommandRunner, workDir string) error {
	result, err := runner.Run(ctx, "gh", []string{"--version"}, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil || result.ExitCode != 0 {
		return errors.New(errors.EGhNotInstalled, "gh CLI not found on PATH; install from https://cli.github.com")
	}

	result, err = runner.Run(ctx, "gh", []string{"auth", "status"}, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil || result.ExitCode != 0 {
		return errors.New(errors.EGhNotAuthenticated, "gh not authenticated; run `gh auth login` first")
	}

	return nil
}

func prSyncGitFetchOrigin(ctx context.Context, runner exec.CommandRunner, workDir string) error {
	result, err := runner.Run(ctx, "git", []string{"fetch", "origin"}, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil {
		return errors.Wrap(errors.EInternal, "git fetch origin failed to start", err)
	}
	if result.ExitCode != 0 {
		return errors.NewWithDetails(
			errors.EInternal,
			fmt.Sprintf("git fetch origin failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{"exit_code": fmt.Sprintf("%d", result.ExitCode)},
		)
	}
	return nil
}

func prSyncResolveParentRef(ctx context.Context, runner exec.CommandRunner, workDir, parentBranch string) (string, error) {
	localExists, err := prSyncRefExists(ctx, runner, workDir, "refs/heads/"+parentBranch)
	if err != nil {
		return "", err
	}
	if localExists {
		return parentBranch, nil
	}

	remoteRef := "refs/remotes/origin/" + parentBranch
	remoteExists, err := prSyncRefExists(ctx, runner, workDir, remoteRef)
	if err != nil {
		return "", err
	}
	if remoteExists {
		return "origin/" + parentBranch, nil
	}

	return "", errors.NewWithDetails(
		errors.EParentNotFound,
		fmt.Sprintf("parent branch %q not found locally or on origin after fetch", parentBranch),
		map[string]string{"parent_branch": parentBranch},
	)
}

func prSyncRefExists(ctx context.Context, runner exec.CommandRunner, workDir, ref string) (bool, error) {
	result, err := runner.Run(ctx, "git", []string{"show-ref", "--verify", "--quiet", ref}, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil {
		return false, errors.Wrap(errors.EInternal, "git show-ref failed to start", err)
	}
	return result.ExitCode == 0, nil
}

func prSyncComputeAhead(ctx context.Context, runner exec.CommandRunner, workDir, parentRef, branch string) (int, error) {
	revRange := parentRef + ".." + branch
	result, err := runner.Run(ctx, "git", []string{"rev-list", "--count", revRange}, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil {
		return 0, errors.Wrap(errors.EInternal, "git rev-list --count failed to start", err)
	}
	if result.ExitCode != 0 {
		return 0, errors.NewWithDetails(
			errors.EInternal,
			fmt.Sprintf("git rev-list --count failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{"rev_range": revRange},
		)
	}

	var count int
	if _, scanErr := fmt.Sscanf(strings.TrimSpace(result.Stdout), "%d", &count); scanErr != nil {
		return 0, errors.Wrap(errors.EInternal, "failed to parse commit count", scanErr)
	}
	return count, nil
}

func prSyncGitPush(ctx context.Context, runner exec.CommandRunner, workDir, branch string, forceWithLease bool) error {
	args := []string{"push", "-u", "origin", branch}
	if forceWithLease {
		args = []string{"push", "--force-with-lease", "-u", "origin", branch}
	}

	result, err := runner.Run(ctx, "git", args, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil {
		return errors.Wrap(errors.EGitPushFailed, "git push failed to start", err)
	}
	if result.ExitCode == 0 {
		return nil
	}

	stderrStr := strings.TrimSpace(result.Stderr)
	if !forceWithLease && render.IsNonFastForwardError(stderrStr) {
		return errors.NewWithDetails(
			errors.EGitPushFailed,
			"push rejected (non-fast-forward)",
			map[string]string{
				"branch": branch,
				"hint":   "branch was rebased or amended; retry with --force-with-lease",
			},
		)
	}

	return errors.NewWithDetails(
		errors.EGitPushFailed,
		"git push -u origin failed",
		map[string]string{
			"branch":    branch,
			"exit_code": fmt.Sprintf("%d", result.ExitCode),
			"stderr":    stderrStr,
		},
	)
}

func prSyncResolveGitHubOwner(ctx context.Context, runner exec.CommandRunner, workDir string) (string, error) {
	result, err := runner.Run(ctx, "git", []string{"config", "--get", "remote.origin.url"}, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil || result.ExitCode != 0 {
		return "", errors.New(errors.EGHRepoParseFailed, "failed to determine GitHub repository from origin remote")
	}

	owner, _, ok := identity.ParseGitHubOwnerRepo(strings.TrimSpace(result.Stdout))
	if !ok {
		return "", errors.New(errors.EGHRepoParseFailed, "failed to parse GitHub owner/repo from origin remote")
	}
	return owner, nil
}

func prSyncHeadRef(owner, branch string) string {
	if strings.TrimSpace(owner) == "" {
		return branch
	}
	return owner + ":" + branch
}

func prSyncListPRsByHead(ctx context.Context, runner exec.CommandRunner, workDir, head string) ([]prSyncPR, error) {
	result, err := runner.Run(ctx, "gh", []string{
		"pr", "list",
		"--head", head,
		"--state", "all",
		"--json", "number,url,state",
	}, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil {
		return nil, errors.Wrap(errors.EGHPRViewFailed, "gh pr list failed to start", err)
	}
	if result.ExitCode != 0 {
		return nil, errors.NewWithDetails(
			errors.EGHPRViewFailed,
			fmt.Sprintf("gh pr list failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{"exit_code": fmt.Sprintf("%d", result.ExitCode)},
		)
	}

	var prs []prSyncPR
	if unmarshalErr := json.Unmarshal([]byte(result.Stdout), &prs); unmarshalErr != nil {
		return nil, errors.Wrap(errors.EGHPRViewFailed, "failed to parse gh pr list output", unmarshalErr)
	}
	return prs, nil
}

func prSyncCreatePR(ctx context.Context, runner exec.CommandRunner, workDir, base, branch, title, bodyPath string) error {
	result, err := runner.Run(ctx, "gh", []string{
		"pr", "create",
		"--base", base,
		"--head", branch,
		"--title", title,
		"--body-file", bodyPath,
	}, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil {
		return errors.Wrap(errors.EGHPRCreateFailed, "gh pr create failed to start", err)
	}
	if result.ExitCode != 0 {
		return errors.NewWithDetails(
			errors.EGHPRCreateFailed,
			fmt.Sprintf("gh pr create failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{"exit_code": fmt.Sprintf("%d", result.ExitCode)},
		)
	}
	return nil
}

func prSyncEditPRBody(ctx context.Context, runner exec.CommandRunner, workDir string, number int, bodyPath string) error {
	result, err := runner.Run(ctx, "gh", []string{
		"pr", "edit", fmt.Sprintf("%d", number),
		"--body-file", bodyPath,
	}, exec.RunOpts{
		Dir: workDir,
		Env: prSyncNonInteractiveEnv(),
	})
	if err != nil {
		return errors.Wrap(errors.EGHPREditFailed, "gh pr edit failed to start", err)
	}
	if result.ExitCode != 0 {
		return errors.NewWithDetails(
			errors.EGHPREditFailed,
			fmt.Sprintf("gh pr edit failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{
				"exit_code": fmt.Sprintf("%d", result.ExitCode),
				"pr_number": fmt.Sprintf("%d", number),
			},
		)
	}
	return nil
}

func prSyncPrepareBody(
	fsys agencyfs.FS,
	worktreePath string,
	invocationID string,
	strict bool,
) (bodyPath, reportSource string, reportFallbackUsed bool, diagnostics []report.Diagnostic, err error) {
	resolution, violation, resolveErr := report.ResolveCanonicalReport(fsys, worktreePath, report.ResolveOptions{
		MaxBytes: prSyncMaxReportBytes,
	})
	if resolveErr != nil {
		return "", "", false, nil, errors.Wrap(errors.EInternal, "failed to evaluate report contract", resolveErr)
	}

	if violation != nil {
		if strict {
			return "", "", false, nil, reportViolationToAgencyError(violation)
		}

		fallbackPath := filepath.Join(worktreePath, ".agency", "pr_sync_fallback.md")
		fallbackDir := filepath.Dir(fallbackPath)
		if mkdirErr := fsys.MkdirAll(fallbackDir, 0o700); mkdirErr != nil {
			return "", "", false, nil, errors.Wrap(errors.EInternal, "failed to create .agency directory for fallback PR body", mkdirErr)
		}
		if chmodErr := fsys.Chmod(fallbackDir, 0o700); chmodErr != nil {
			return "", "", false, nil, errors.Wrap(errors.EInternal, "failed to set fallback PR body directory permissions", chmodErr)
		}
		fallback := fmt.Sprintf(
			"## summary\nfallback PR body generated for invocation %s.\n\n## how to test\n- run project tests and manual verification for this branch.\n",
			invocationID,
		)
		if writeErr := fsys.WriteFile(fallbackPath, []byte(fallback), 0o600); writeErr != nil {
			return "", "", false, nil, errors.Wrap(errors.EInternal, "failed to write fallback PR body", writeErr)
		}
		if chmodErr := fsys.Chmod(fallbackPath, 0o600); chmodErr != nil {
			return "", "", false, nil, errors.Wrap(errors.EInternal, "failed to set fallback PR body permissions", chmodErr)
		}
		violationDiagnostics := []report.Diagnostic{
			{
				Code:    string(violation.Code),
				Message: violation.Message,
				Source:  string(violation.Source),
			},
		}
		return fallbackPath, "fallback", true, violationDiagnostics, nil
	}

	if resolution == nil {
		return "", "", false, nil, errors.New(errors.EInternal, "report resolution produced no result")
	}

	diags := resolution.Diagnostics
	switch resolution.Source {
	case report.SourceJSON:
		canonicalPath, writeErr := report.WriteCanonicalMarkdownBody(fsys, worktreePath, "pr_sync_report_v2.md", resolution.Model)
		if writeErr != nil {
			return "", "", false, nil, errors.Wrap(errors.EInternal, "failed to materialize report.json body", writeErr)
		}
		return canonicalPath, string(resolution.Source), false, diags, nil
	case report.SourceMarkdown:
		reportPath := filepath.Join(worktreePath, ".agency", "report.md")
		return reportPath, string(resolution.Source), false, diags, nil
	default:
		return "", "", false, nil, errors.NewWithDetails(
			errors.EInternal,
			"unsupported report source",
			map[string]string{"source": string(resolution.Source)},
		)
	}
}

func (s *Server) appendPRSyncEvent(repoID, invocationID, kind string, data map[string]any) error {
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
		return errors.Wrap(errors.EInternal, "failed to append invocation event", err)
	}
	return nil
}

func prSyncNonInteractiveEnv() map[string]string {
	return map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
		"GH_PROMPT_DISABLED":  "1",
		"CI":                  "1",
	}
}

func prSyncHintFromError(err error) string {
	if ae, ok := errors.AsAgencyError(err); ok && ae.Details != nil {
		if hint := strings.TrimSpace(ae.Details["hint"]); hint != "" {
			return hint
		}
	}
	return ""
}

func decodePRSyncRequest(body io.Reader) (PRSyncRequest, string) {
	var req PRSyncRequest
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if err == io.EOF {
			return req, ""
		}
		return PRSyncRequest{}, prSyncDecodeErrorMessage(err)
	}

	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return PRSyncRequest{}, "invalid request body: expected a single JSON object"
	}

	return req, ""
}

func prSyncDecodeErrorMessage(err error) string {
	if unknownField, ok := prSyncUnknownFieldName(err); ok {
		return fmt.Sprintf("invalid request body: unknown field %q", unknownField)
	}

	if _, ok := err.(*json.SyntaxError); ok || err == io.ErrUnexpectedEOF {
		return "invalid request body: malformed JSON"
	}

	if typeErr, ok := err.(*json.UnmarshalTypeError); ok {
		if field := strings.TrimSpace(typeErr.Field); field != "" {
			return fmt.Sprintf("invalid request body: field %q must be %s", field, typeErr.Type.String())
		}
		return "invalid request body: invalid value type"
	}

	return "invalid request body: malformed JSON"
}

func prSyncUnknownFieldName(err error) (string, bool) {
	const prefix = "json: unknown field "
	msg := strings.TrimSpace(err.Error())
	if !strings.HasPrefix(msg, prefix) {
		return "", false
	}
	field := strings.TrimSpace(strings.Trim(strings.TrimPrefix(msg, prefix), `"`))
	if field == "" {
		return "", false
	}
	return field, true
}

func prSyncHTTPStatusForCode(code errors.Code) int {
	switch code {
	case errors.EInvocationNotFound:
		return http.StatusNotFound
	case errors.EInvocationIDAmbiguous:
		return http.StatusConflict
	case errors.ERepoLocked:
		return http.StatusConflict
	case errors.EDirtyWorktree:
		return http.StatusConflict
	case errors.EGitPushFailed:
		return http.StatusConflict
	case errors.EParentNotFound:
		return http.StatusBadRequest
	case errors.EEmptyDiff:
		return http.StatusBadRequest
	case errors.EPRNotOpen:
		return http.StatusConflict
	case errors.EReportMissing, errors.EReportMalformed, errors.EReportOversized, errors.EReportSchemaIncompatible, errors.EReportIncomplete:
		return http.StatusConflict
	case errors.EGHRepoParseFailed:
		return http.StatusBadRequest
	case errors.EGhNotInstalled, errors.EGhNotAuthenticated:
		return http.StatusBadRequest
	case errors.EInvalidArgument:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func (s *Server) writePRSyncError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	resp := PRSyncResponse{
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
