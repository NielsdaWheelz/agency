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

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/render"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const (
	prSyncEventStarted   = "agency.pr_sync_started"
	prSyncEventSucceeded = "agency.pr_sync_succeeded"
	prSyncEventFailed    = "agency.pr_sync_failed"
)

type prSyncResult struct {
	Branch   string
	PRNumber int
	PRURL    string
	PRAction string
}

type prSyncPR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

func (s *Server) runPRSync(
	ctx context.Context,
	record *resolvedInvocation,
	wtMeta *store.IntegrationWorktreeMeta,
	req WorktreePRSyncRequest,
) (*prSyncResult, error) {
	profileEnv, err := s.executionProfileEnv(wtMeta.ExecutionProfile)
	if err != nil {
		return nil, err
	}
	env := prSyncNonInteractiveEnv(profileEnv)

	clean, dirtyStatus, err := prSyncDirtyStatus(ctx, s.Runner, wtMeta.TreePath, env)
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

	if err := prSyncCheckGHAuth(ctx, s.Runner, wtMeta.TreePath, env); err != nil {
		return nil, err
	}
	if err := prSyncGitFetchOrigin(ctx, s.Runner, wtMeta.TreePath, env); err != nil {
		return nil, err
	}

	baseRef, err := prSyncResolveBaseRef(ctx, s.Runner, wtMeta.TreePath, wtMeta.BaseBranch, env)
	if err != nil {
		return nil, err
	}
	ahead, err := prSyncComputeAhead(ctx, s.Runner, wtMeta.TreePath, baseRef, wtMeta.Branch, env)
	if err != nil {
		return nil, err
	}
	if ahead == 0 {
		return nil, errors.New(errors.EEmptyDiff, "no commits ahead of base branch; make at least one commit")
	}

	bodyPath, err := prSyncPrepareBody(s.FS, wtMeta.TreePath)
	if err != nil {
		return nil, err
	}

	if err := prSyncGitPush(ctx, s.Runner, wtMeta.TreePath, wtMeta.Branch, req.ForceWithLease, env); err != nil {
		return nil, err
	}

	owner, err := prSyncResolveGitHubOwner(ctx, s.Runner, wtMeta.TreePath, env)
	if err != nil {
		return nil, err
	}
	head := prSyncHeadRef(owner, wtMeta.Branch)
	title := strings.TrimSpace(wtMeta.Name)
	if title == "" {
		title = wtMeta.Branch
	}
	title = "[agency] " + title
	prs, err := prSyncListPRsByHead(ctx, s.Runner, wtMeta.TreePath, head, env)
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
		createErr := prSyncCreatePR(ctx, s.Runner, wtMeta.TreePath, wtMeta.BaseBranch, wtMeta.Branch, title, bodyPath, env)
		createdNow := createErr == nil
		if createErr != nil && !prSyncIsAlreadyExistsError(createErr) {
			return nil, createErr
		}

		pr, lookupErr := prSyncLookupPRAfterCreateWithRetry(ctx, s.Runner, wtMeta.TreePath, owner, wtMeta.Branch, env)
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
				Branch:   wtMeta.Branch,
				PRNumber: pr.Number,
				PRURL:    pr.URL,
				PRAction: "created",
			}, nil
		}

		// PR already existed (create raced/failed as already exists): update body.
		if err := prSyncEditPRBody(ctx, s.Runner, wtMeta.TreePath, pr.Number, bodyPath, env); err != nil {
			return nil, err
		}
		return &prSyncResult{
			Branch:   wtMeta.Branch,
			PRNumber: pr.Number,
			PRURL:    pr.URL,
			PRAction: "updated",
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
	if err := prSyncEditPRBody(ctx, s.Runner, wtMeta.TreePath, pr.Number, bodyPath, env); err != nil {
		return nil, err
	}

	return &prSyncResult{
		Branch:   wtMeta.Branch,
		PRNumber: pr.Number,
		PRURL:    pr.URL,
		PRAction: "updated",
	}, nil
}

func prSyncLookupPRAfterCreate(ctx context.Context, runner exec.CommandRunner, workDir, owner, branch string, env map[string]string) (*prSyncPR, error) {
	head := prSyncHeadRef(owner, branch)
	prs, err := prSyncListPRsByHead(ctx, runner, workDir, head, env)
	if err != nil {
		return nil, err
	}
	if len(prs) == 0 && strings.TrimSpace(owner) != "" {
		prs, err = prSyncListPRsByHead(ctx, runner, workDir, branch, env)
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

func prSyncLookupPRAfterCreateWithRetry(ctx context.Context, runner exec.CommandRunner, workDir, owner, branch string, env map[string]string) (*prSyncPR, error) {
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

		pr, err := prSyncLookupPRAfterCreate(ctx, runner, workDir, owner, branch, env)
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

func prSyncDirtyStatus(ctx context.Context, runner exec.CommandRunner, workDir string, env map[string]string) (bool, string, error) {
	result, err := runner.Run(ctx, "git", []string{"status", "--porcelain", "--untracked-files=all"}, exec.RunOpts{
		Dir: workDir,
		Env: env,
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

	statusLines := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	kept := statusLines[:0]
	for _, line := range statusLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) >= 4 {
			path := strings.TrimSpace(line[3:])
			if path == ".agency" || strings.HasPrefix(path, ".agency/") {
				continue
			}
		}
		kept = append(kept, line)
	}
	status := strings.Join(kept, "\n")
	return strings.TrimSpace(status) == "", status, nil
}

func prSyncCheckGHAuth(ctx context.Context, runner exec.CommandRunner, workDir string, env map[string]string) error {
	result, err := runner.Run(ctx, "gh", []string{"--version"}, exec.RunOpts{
		Dir: workDir,
		Env: env,
	})
	if err != nil || result.ExitCode != 0 {
		return errors.New(errors.EGhNotInstalled, "gh CLI not found on PATH; install from https://cli.github.com")
	}

	result, err = runner.Run(ctx, "gh", []string{"auth", "status"}, exec.RunOpts{
		Dir: workDir,
		Env: env,
	})
	if err != nil || result.ExitCode != 0 {
		return errors.New(errors.EGhNotAuthenticated, "gh not authenticated; run `gh auth login` first")
	}

	return nil
}

func prSyncGitFetchOrigin(ctx context.Context, runner exec.CommandRunner, workDir string, env map[string]string) error {
	result, err := runner.Run(ctx, "git", []string{"fetch", "origin"}, exec.RunOpts{
		Dir: workDir,
		Env: env,
	})
	if err != nil {
		return errors.Wrap(errors.EGitFetchFailed, "git fetch origin failed to start", err)
	}
	if result.ExitCode != 0 {
		return errors.NewWithDetails(
			errors.EGitFetchFailed,
			fmt.Sprintf("git fetch origin failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{"exit_code": fmt.Sprintf("%d", result.ExitCode)},
		)
	}
	return nil
}

func prSyncResolveBaseRef(ctx context.Context, runner exec.CommandRunner, workDir, baseBranch string, env map[string]string) (string, error) {
	localExists, err := prSyncRefExists(ctx, runner, workDir, "refs/heads/"+baseBranch, env)
	if err != nil {
		return "", err
	}
	if localExists {
		return baseBranch, nil
	}

	remoteRef := "refs/remotes/origin/" + baseBranch
	remoteExists, err := prSyncRefExists(ctx, runner, workDir, remoteRef, env)
	if err != nil {
		return "", err
	}
	if remoteExists {
		return "origin/" + baseBranch, nil
	}

	return "", errors.NewWithDetails(
		errors.EBaseNotFound,
		fmt.Sprintf("base branch %q not found locally or on origin after fetch", baseBranch),
		map[string]string{"base_branch": baseBranch},
	)
}

func prSyncRefExists(ctx context.Context, runner exec.CommandRunner, workDir, ref string, env map[string]string) (bool, error) {
	result, err := runner.Run(ctx, "git", []string{"show-ref", "--verify", "--quiet", ref}, exec.RunOpts{
		Dir: workDir,
		Env: env,
	})
	if err != nil {
		return false, errors.Wrap(errors.EInternal, "git show-ref failed to start", err)
	}
	return result.ExitCode == 0, nil
}

func prSyncComputeAhead(ctx context.Context, runner exec.CommandRunner, workDir, baseRef, branch string, env map[string]string) (int, error) {
	revRange := baseRef + ".." + branch
	result, err := runner.Run(ctx, "git", []string{"rev-list", "--count", revRange}, exec.RunOpts{
		Dir: workDir,
		Env: env,
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

func prSyncGitPush(ctx context.Context, runner exec.CommandRunner, workDir, branch string, forceWithLease bool, env map[string]string) error {
	args := []string{"push", "-u", "origin", branch}
	if forceWithLease {
		args = []string{"push", "--force-with-lease", "-u", "origin", branch}
	}

	result, err := runner.Run(ctx, "git", args, exec.RunOpts{
		Dir: workDir,
		Env: env,
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

func prSyncResolveGitHubOwner(ctx context.Context, runner exec.CommandRunner, workDir string, env map[string]string) (string, error) {
	result, err := runner.Run(ctx, "git", []string{"config", "--get", "remote.origin.url"}, exec.RunOpts{
		Dir: workDir,
		Env: env,
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

func prSyncListPRsByHead(ctx context.Context, runner exec.CommandRunner, workDir, head string, env map[string]string) ([]prSyncPR, error) {
	result, err := runner.Run(ctx, "gh", []string{
		"pr", "list",
		"--head", head,
		"--state", "all",
		"--json", "number,url,state",
	}, exec.RunOpts{
		Dir: workDir,
		Env: env,
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

func prSyncCreatePR(ctx context.Context, runner exec.CommandRunner, workDir, base, branch, title, bodyPath string, env map[string]string) error {
	result, err := runner.Run(ctx, "gh", []string{
		"pr", "create",
		"--base", base,
		"--head", branch,
		"--title", title,
		"--body-file", bodyPath,
	}, exec.RunOpts{
		Dir: workDir,
		Env: env,
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

func prSyncEditPRBody(ctx context.Context, runner exec.CommandRunner, workDir string, number int, bodyPath string, env map[string]string) error {
	result, err := runner.Run(ctx, "gh", []string{
		"pr", "edit", fmt.Sprintf("%d", number),
		"--body-file", bodyPath,
	}, exec.RunOpts{
		Dir: workDir,
		Env: env,
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
) (string, error) {
	status, err := runnerstatus.Load(worktreePath)
	if err != nil {
		status = nil
	}

	summary := ""
	howToTest := ""
	if status != nil && strings.TrimSpace(status.SchemaVersion) == runnerstatus.SchemaVersion {
		summary = strings.TrimSpace(status.Summary)
		howToTest = strings.TrimSpace(status.HowToTest)
	}
	if summary == "" {
		summary = "Summary not provided."
	}
	if howToTest == "" {
		howToTest = "How to test not provided."
	}

	bodyPath := filepath.Join(worktreePath, ".agency", "tmp", "pr_body.md")
	bodyDir := filepath.Dir(bodyPath)
	if err := fsys.MkdirAll(bodyDir, 0o700); err != nil {
		return "", errors.Wrap(errors.EInternal, "failed to create PR body directory", err)
	}
	if err := fsys.Chmod(bodyDir, 0o700); err != nil {
		return "", errors.Wrap(errors.EInternal, "failed to set PR body directory permissions", err)
	}

	content := "## summary\n" + summary + "\n\n## how to test\n" + howToTest + "\n"
	if err := agencyfs.WriteFileAtomic(fsys, bodyPath, []byte(content), 0o600); err != nil {
		return "", errors.Wrap(errors.EInternal, "failed to write PR body", err)
	}
	return bodyPath, nil
}

func prSyncNonInteractiveEnv(profileEnv map[string]string) map[string]string {
	env := copyStringMap(profileEnv)
	if env == nil {
		env = map[string]string{}
	}
	env["GIT_TERMINAL_PROMPT"] = "0"
	env["GH_PROMPT_DISABLED"] = "1"
	env["CI"] = "1"
	return env
}

func prSyncHintFromError(err error) string {
	if ae, ok := errors.AsAgencyError(err); ok && ae.Details != nil {
		if hint := strings.TrimSpace(ae.Details["hint"]); hint != "" {
			return hint
		}
	}
	return ""
}

func decodePRSyncRequest(body io.Reader) (WorktreePRSyncRequest, string) {
	var req WorktreePRSyncRequest
	if err := decodeOptionalStrictJSON(body, &req); err != nil {
		return WorktreePRSyncRequest{}, prSyncDecodeErrorMessage(err)
	}
	return req, ""
}

func prSyncDecodeErrorMessage(err error) string {
	msg := strings.TrimSpace(err.Error())
	if msg == "expected a JSON object" || msg == "expected a single JSON object" {
		return "invalid request body: " + msg
	}

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
	case errors.EWorktreeNotFound:
		return http.StatusNotFound
	case errors.EWorktreeIDAmbiguous:
		return http.StatusConflict
	case errors.EInvocationNotFound:
		return http.StatusNotFound
	case errors.EInvocationIDAmbiguous:
		return http.StatusConflict
	case errors.ERepoLocked:
		return http.StatusConflict
	case errors.EWorktreeMergeActive:
		return http.StatusConflict
	case errors.EDirtyWorktree:
		return http.StatusConflict
	case errors.EGitPushFailed:
		return http.StatusConflict
	case errors.EGitFetchFailed:
		return http.StatusBadGateway
	case errors.EBaseNotFound:
		return http.StatusBadRequest
	case errors.EEmptyDiff:
		return http.StatusBadRequest
	case errors.EPRNotOpen:
		return http.StatusConflict
	case errors.EGHRepoParseFailed:
		return http.StatusBadRequest
	case errors.EGhNotInstalled, errors.EGhNotAuthenticated:
		return http.StatusBadRequest
	case errors.EInvalidArgument:
		return http.StatusBadRequest
	case errors.EPersistFailed:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
