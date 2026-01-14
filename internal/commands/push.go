// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/events"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// PushOpts holds options for the push command.
type PushOpts struct {
	// RunID is the run identifier (exact or unique prefix).
	RunID string

	// Force allows pushing with missing/empty report.
	// Does NOT bypass E_EMPTY_DIFF.
	Force bool
}

// nonInteractiveEnv returns the environment overlay for non-interactive git/gh execution.
// Per spec: GIT_TERMINAL_PROMPT=0, GH_PROMPT_DISABLED=1, CI=1
func nonInteractiveEnv() map[string]string {
	return map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
		"GH_PROMPT_DISABLED":  "1",
		"CI":                  "1",
	}
}

// Push executes the agency push command.
// Pushes the run branch to origin and returns the branch name on success.
// This PR does NOT create/update GitHub PRs (deferred to PR-3).
func Push(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts PushOpts, stdout, stderr io.Writer) error {
	// Resolve data directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	dataDir := dirs.DataDir

	// Create store
	st := store.NewStore(fsys, dataDir, time.Now)

	// Step 1: Resolve run_id and load metadata
	runRef, meta, repoID, err := resolveRunForPush(ctx, cr, fsys, cwd, st, opts.RunID)
	if err != nil {
		return err
	}

	// Append push_started event
	eventsPath := filepath.Join(st.RunDir(repoID, meta.RunID), "events.jsonl")
	appendPushEvent(eventsPath, repoID, meta.RunID, "push_started", nil)

	// Step 2: Ensure worktree exists on disk
	if _, err := os.Stat(meta.WorktreePath); os.IsNotExist(err) {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.EWorktreeMissing),
			"step":       "worktree_check",
		})
		return errors.NewWithDetails(
			errors.EWorktreeMissing,
			"run worktree path is missing on disk",
			map[string]string{"worktree_path": meta.WorktreePath},
		)
	}

	// Step 3: Acquire repo lock
	repoLock := lock.NewRepoLock(dataDir)
	unlock, err := repoLock.Lock(repoID, "push")
	if err != nil {
		var lockErr *lock.ErrLocked
		if stderrors.As(err, &lockErr) {
			appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
				"error_code": string(errors.ERepoLocked),
				"step":       "repo_lock",
			})
			return errors.New(errors.ERepoLocked, lockErr.Error())
		}
		return errors.Wrap(errors.EInternal, "failed to acquire repo lock", err)
	}
	defer unlock()

	// Step 4: Ensure origin exists
	originURL := git.GetOriginURL(ctx, cr, meta.WorktreePath)
	if originURL == "" {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.ENoOrigin),
			"step":       "origin_check",
		})
		return errors.New(errors.ENoOrigin, "no origin remote configured; run `git remote add origin <url>` first")
	}

	// Step 5: Ensure origin host is exactly github.com
	originHost := git.ParseOriginHost(originURL)
	if originHost != "github.com" {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.EUnsupportedOriginHost),
			"step":       "origin_check",
			"origin_url": originURL,
			"host":       originHost,
		})
		hint := "agency push requires a github.com origin in v1"
		if originHost == "" {
			hint = "could not parse hostname from origin URL; expected github.com"
		}
		return errors.NewWithDetails(
			errors.EUnsupportedOriginHost,
			fmt.Sprintf("origin host %q is not supported; must be github.com", originHost),
			map[string]string{"hint": hint, "origin_url": originURL},
		)
	}

	// Step 6: Report gating
	reportPath := filepath.Join(meta.WorktreePath, ".agency", "report.md")
	reportEmpty, err := isReportEffectivelyEmpty(fsys, reportPath)
	if err != nil && !os.IsNotExist(err) {
		// Unexpected error reading report
		fmt.Fprintf(stderr, "warning: failed to read report: %v\n", err)
		reportEmpty = true
	}

	if reportEmpty && !opts.Force {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.EReportInvalid),
			"step":       "report_gate",
		})
		return errors.NewWithDetails(
			errors.EReportInvalid,
			"report is missing or effectively empty; use --force to push anyway",
			map[string]string{"report_path": reportPath},
		)
	}

	if reportEmpty && opts.Force {
		fmt.Fprintf(stderr, "warning: report is missing or effectively empty (proceeding with --force)\n")
	}

	// Step 7: Dirty worktree warning
	isClean, err := git.IsClean(ctx, cr, meta.WorktreePath)
	if err != nil {
		fmt.Fprintf(stderr, "warning: failed to check worktree status: %v\n", err)
	} else if !isClean {
		fmt.Fprintf(stderr, "warning: worktree has uncommitted changes; only committed changes will be pushed\n")
	}

	// Step 8: gh auth check
	if err := checkGhAuthForPush(ctx, cr, meta.WorktreePath); err != nil {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.GetCode(err)),
			"step":       "gh_auth",
		})
		return err
	}

	// === Network side effects begin here ===

	// Step 9: git fetch origin
	fetchStart := time.Now()
	if err := gitFetchOrigin(ctx, cr, meta.WorktreePath); err != nil {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.EInternal),
			"step":       "git_fetch",
		})
		return err
	}
	fetchDurationMs := time.Since(fetchStart).Milliseconds()
	appendPushEvent(eventsPath, repoID, meta.RunID, "git_fetch_finished", map[string]any{
		"duration_ms": fetchDurationMs,
	})

	// Step 10: Resolve parent ref
	parentRef, err := resolveParentRef(ctx, cr, meta.WorktreePath, meta.ParentBranch)
	if err != nil {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.GetCode(err)),
			"step":       "parent_ref",
		})
		return err
	}

	// Step 11: Compute ahead count
	ahead, err := computeAhead(ctx, cr, meta.WorktreePath, parentRef, meta.Branch)
	if err != nil {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.EInternal),
			"step":       "ahead_check",
		})
		return err
	}

	if ahead == 0 {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.EEmptyDiff),
			"step":       "ahead_check",
		})
		return errors.NewWithDetails(
			errors.EEmptyDiff,
			"no commits ahead of parent branch; make at least one commit",
			map[string]string{
				"parent_ref":       parentRef,
				"workspace_branch": meta.Branch,
			},
		)
	}

	// Step 12: git push -u origin <workspace_branch>
	pushStart := time.Now()
	if err := gitPushBranch(ctx, cr, meta.WorktreePath, meta.Branch, stderr); err != nil {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.GetCode(err)),
			"step":       "git_push",
		})
		return err
	}
	pushDurationMs := time.Since(pushStart).Milliseconds()
	appendPushEvent(eventsPath, repoID, meta.RunID, "git_push_finished", map[string]any{
		"duration_ms": pushDurationMs,
	})

	// Step 13: Update meta.json with last_push_at
	now := time.Now().UTC().Format(time.RFC3339)
	if err := st.UpdateMeta(repoID, meta.RunID, func(m *store.RunMeta) {
		m.LastPushAt = now
	}); err != nil {
		// Non-fatal: log warning but don't fail the push
		fmt.Fprintf(stderr, "warning: failed to update meta.json: %v\n", err)
	}

	// Append push_finished event
	appendPushEvent(eventsPath, repoID, meta.RunID, "push_finished", nil)

	// Print success output
	fmt.Fprintf(stdout, "pushed %s to origin\n", meta.Branch)

	_ = runRef // silence unused variable warning
	return nil
}

// resolveRunForPush resolves the run identifier and loads metadata.
// Returns the run reference, metadata, repoID, and any error.
func resolveRunForPush(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, st *store.Store, runID string) (ids.RunRef, *store.RunMeta, string, error) {
	// Scan all runs - use the store's DataDir
	allRuns, err := store.ScanAllRuns(st.DataDir)
	if err != nil {
		return ids.RunRef{}, nil, "", errors.Wrap(errors.EInternal, "failed to scan runs", err)
	}

	// Build refs list
	var refs []ids.RunRef
	for _, run := range allRuns {
		refs = append(refs, ids.RunRef{
			RepoID: run.RepoID,
			RunID:  run.RunID,
			Broken: run.Broken,
		})
	}

	// Resolve
	runRef, err := ids.ResolveRunRef(runID, refs)
	if err != nil {
		var notFound *ids.ErrNotFound
		if stderrors.As(err, &notFound) {
			return ids.RunRef{}, nil, "", errors.New(errors.ERunNotFound, fmt.Sprintf("run not found: %s", runID))
		}
		var ambiguous *ids.ErrAmbiguous
		if stderrors.As(err, &ambiguous) {
			candidates := make([]string, len(ambiguous.Candidates))
			for i, c := range ambiguous.Candidates {
				candidates[i] = c.RunID
			}
			return ids.RunRef{}, nil, "", errors.NewWithDetails(
				errors.ERunIDAmbiguous,
				fmt.Sprintf("ambiguous run id %q matches multiple runs", runID),
				map[string]string{"candidates": strings.Join(candidates, ", ")},
			)
		}
		return ids.RunRef{}, nil, "", errors.Wrap(errors.EInternal, "failed to resolve run id", err)
	}

	// Check if broken
	if runRef.Broken {
		return ids.RunRef{}, nil, "", errors.NewWithDetails(
			errors.ERunBroken,
			"run exists but meta.json is unreadable or invalid",
			map[string]string{"run_id": runRef.RunID, "repo_id": runRef.RepoID},
		)
	}

	// Load metadata
	meta, err := st.ReadMeta(runRef.RepoID, runRef.RunID)
	if err != nil {
		return ids.RunRef{}, nil, "", err
	}

	return runRef, meta, runRef.RepoID, nil
}

// isReportEffectivelyEmpty returns true if the report is missing or has < 20 trimmed chars.
func isReportEffectivelyEmpty(fsys fs.FS, reportPath string) (bool, error) {
	data, err := fsys.ReadFile(reportPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}

	trimmed := strings.TrimSpace(string(data))
	return len(trimmed) < 20, nil
}

// checkGhAuthForPush verifies gh is installed and authenticated with non-interactive env.
func checkGhAuthForPush(ctx context.Context, cr exec.CommandRunner, workDir string) error {
	// Check gh is installed by running gh --version
	result, err := cr.Run(ctx, "gh", []string{"--version"}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return errors.New(errors.EGhNotInstalled, "gh CLI not found on PATH; install from https://cli.github.com")
	}
	if result.ExitCode != 0 {
		return errors.New(errors.EGhNotInstalled, "gh CLI not found on PATH; install from https://cli.github.com")
	}

	// Check gh auth status
	result, err = cr.Run(ctx, "gh", []string{"auth", "status"}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return errors.New(errors.EGhNotAuthenticated, "gh not authenticated; run `gh auth login` first")
	}
	if result.ExitCode != 0 {
		return errors.New(errors.EGhNotAuthenticated, "gh not authenticated; run `gh auth login` first")
	}

	return nil
}

// gitFetchOrigin runs git fetch origin in the worktree.
func gitFetchOrigin(ctx context.Context, cr exec.CommandRunner, workDir string) error {
	result, err := cr.Run(ctx, "git", []string{"fetch", "origin"}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
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

// resolveParentRef resolves the parent ref for ahead computation.
// Prefers local <parent_branch>, falls back to origin/<parent_branch>.
func resolveParentRef(ctx context.Context, cr exec.CommandRunner, workDir, parentBranch string) (string, error) {
	// Check if local parent branch exists
	localExists, err := refExists(ctx, cr, workDir, "refs/heads/"+parentBranch)
	if err != nil {
		return "", err
	}
	if localExists {
		return parentBranch, nil
	}

	// Check if remote parent branch exists
	remoteRef := "refs/remotes/origin/" + parentBranch
	remoteExists, err := refExists(ctx, cr, workDir, remoteRef)
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

// refExists checks if a git ref exists.
func refExists(ctx context.Context, cr exec.CommandRunner, workDir, ref string) (bool, error) {
	result, err := cr.Run(ctx, "git", []string{"show-ref", "--verify", "--quiet", ref}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return false, errors.Wrap(errors.EInternal, "git show-ref failed to start", err)
	}
	// Exit code 0 = ref exists, non-zero = does not exist
	return result.ExitCode == 0, nil
}

// computeAhead returns the number of commits ahead of parentRef in the workspace branch.
func computeAhead(ctx context.Context, cr exec.CommandRunner, workDir, parentRef, workspaceBranch string) (int, error) {
	// git rev-list --count <parentRef>..<workspaceBranch>
	revRange := parentRef + ".." + workspaceBranch
	result, err := cr.Run(ctx, "git", []string{"rev-list", "--count", revRange}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
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

	countStr := strings.TrimSpace(result.Stdout)
	var count int
	_, err = fmt.Sscanf(countStr, "%d", &count)
	if err != nil {
		return 0, errors.Wrap(errors.EInternal, "failed to parse commit count", err)
	}

	return count, nil
}

// gitPushBranch pushes the workspace branch to origin with -u.
func gitPushBranch(ctx context.Context, cr exec.CommandRunner, workDir, branch string, stderr io.Writer) error {
	result, err := cr.Run(ctx, "git", []string{"push", "-u", "origin", branch}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return errors.Wrap(errors.EGitPushFailed, "git push failed to start", err)
	}
	if result.ExitCode != 0 {
		// Surface stderr for actionable debugging
		if result.Stderr != "" {
			fmt.Fprintf(stderr, "git push stderr:\n%s", result.Stderr)
		}
		return errors.NewWithDetails(
			errors.EGitPushFailed,
			"git push -u origin failed",
			map[string]string{
				"exit_code": fmt.Sprintf("%d", result.ExitCode),
				"branch":    branch,
			},
		)
	}
	return nil
}

// appendPushEvent appends an event to the events.jsonl file.
// Best-effort: errors are ignored.
func appendPushEvent(eventsPath, repoID, runID, eventName string, data map[string]any) {
	e := events.Event{
		SchemaVersion: "1.0",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         runID,
		Event:         eventName,
		Data:          data,
	}
	_ = events.AppendEvent(eventsPath, e)
}

// computeReportHash computes the sha256 hash of the report file.
// Returns empty string if report doesn't exist or can't be read.
func computeReportHash(fsys fs.FS, reportPath string) string {
	data, err := fsys.ReadFile(reportPath)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// ResolveRepoIdentity resolves the repo identity from cwd.
// Exported for use by push command.
func ResolveRepoIdentity(ctx context.Context, cr exec.CommandRunner, cwd string) (string, string, error) {
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return "", "", err
	}

	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	return repoIdentity.RepoID, repoRoot.Path, nil
}
