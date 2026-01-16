// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// Sleeper is an interface for injectable sleep (for testing).
type Sleeper interface {
	Sleep(d time.Duration)
}

// realSleeper is the production implementation of Sleeper.
type realSleeper struct{}

func (realSleeper) Sleep(d time.Duration) {
	time.Sleep(d)
}

// ghPRView represents the JSON output of gh pr view --json number,url,state.
type ghPRView struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"` // OPEN, CLOSED, MERGED
}

// PushOpts holds options for the push command.
type PushOpts struct {
	// RunID is the run identifier (exact or unique prefix).
	RunID string

	// Force allows pushing with missing/empty report.
	// Does NOT bypass E_EMPTY_DIFF.
	Force bool

	// Sleeper is an injectable sleeper for testing. If nil, uses real time.Sleep.
	Sleeper Sleeper
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
	defer func() {
		// Unlock error logged but not returned; command result takes priority
		if uerr := unlock(); uerr != nil {
			_ = uerr // Lock package handles logging internally
		}
	}()

	// Step 4: Ensure origin exists (spec: exact error message)
	originURL := git.GetOriginURL(ctx, cr, meta.WorktreePath)
	if originURL == "" {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.ENoOrigin),
			"step":       "origin_check",
		})
		return errors.New(errors.ENoOrigin, "git remote 'origin' not configured")
	}

	// Step 5: Ensure origin host is exactly github.com (spec: exact error message)
	originHost := git.ParseOriginHost(originURL)
	if originHost != "github.com" {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.EUnsupportedOriginHost),
			"step":       "origin_check",
			"origin_url": originURL,
			"host":       originHost,
		})
		return errors.New(errors.EUnsupportedOriginHost, "origin host must be github.com")
	}

	// Step 6: Report gating
	reportPath := filepath.Join(meta.WorktreePath, ".agency", "report.md")
	reportEmpty, err := isReportEffectivelyEmpty(fsys, reportPath)
	if err != nil && !os.IsNotExist(err) {
		// Unexpected error reading report - warn user but continue
		_, _ = fmt.Fprintf(stderr, "warning: failed to read report: %v\n", err)
		reportEmpty = true
	}

	if reportEmpty && !opts.Force {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.EReportInvalid),
			"step":       "report_gate",
		})
		// spec: exact error message
		return errors.New(errors.EReportInvalid, "report missing or empty; use --force to push anyway")
	}

	if reportEmpty && opts.Force {
		_, _ = fmt.Fprintf(stderr, "warning: report missing or empty; proceeding due to --force\n")
	}

	// Step 7: Dirty worktree warning (spec: exact string)
	isClean, err := git.IsClean(ctx, cr, meta.WorktreePath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: failed to check worktree status: %v\n", err)
	} else if !isClean {
		_, _ = fmt.Fprintf(stderr, "warning: worktree has uncommitted changes; pushing commits anyway\n")
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
		// spec: exact error message
		return errors.New(errors.EEmptyDiff, "no commits ahead of parent; make at least one commit")
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

	// Step 13: Update last_push_at immediately after git push
	now := time.Now().UTC().Format(time.RFC3339)
	if err := st.UpdateMeta(repoID, meta.RunID, func(m *store.RunMeta) {
		m.LastPushAt = now
	}); err != nil {
		// Non-fatal: log warning but don't fail the push
		_, _ = fmt.Fprintf(stderr, "warning: failed to update meta.json: %v\n", err)
	}

	// Step 14: PR lookup / create / update
	sleeper := opts.Sleeper
	if sleeper == nil {
		sleeper = realSleeper{}
	}

	prResult, err := handlePR(ctx, cr, fsys, st, meta, repoID, reportPath, reportEmpty, opts.Force, sleeper, eventsPath, stderr)
	if err != nil {
		appendPushEvent(eventsPath, repoID, meta.RunID, "push_failed", map[string]any{
			"error_code": string(errors.GetCode(err)),
			"step":       "pr",
		})
		return err
	}

	// Append push_finished event
	appendPushEvent(eventsPath, repoID, meta.RunID, "push_finished", map[string]any{
		"pr_number": prResult.Number,
		"pr_url":    prResult.URL,
		"pr_action": prResult.Action,
	})

	// Print success output (spec: exactly one stdout line)
	_, _ = fmt.Fprintf(stdout, "pr: %s\n", prResult.URL)

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
			_, _ = fmt.Fprintf(stderr, "git push stderr:\n%s", result.Stderr)
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

// prResult holds the result of PR operations.
type prResult struct {
	Number int
	URL    string
	Action string // "created" or "updated"
}

// handlePR handles PR lookup, creation, and body sync.
// Returns PR info on success.
func handlePR(
	ctx context.Context,
	cr exec.CommandRunner,
	fsys fs.FS,
	st *store.Store,
	meta *store.RunMeta,
	repoID string,
	reportPath string,
	reportEmpty bool,
	force bool,
	sleeper Sleeper,
	eventsPath string,
	stderr io.Writer,
) (*prResult, error) {
	workDir := meta.WorktreePath

	// Step 1: Look up existing PR
	pr, prState, err := lookupPR(ctx, cr, workDir, meta)
	if err != nil {
		return nil, err
	}

	// Check if PR exists but is not open
	if pr != nil && prState != "OPEN" {
		return nil, errors.NewWithDetails(
			errors.EPRNotOpen,
			fmt.Sprintf("PR #%d exists but state is %s (expected OPEN)", pr.Number, prState),
			map[string]string{
				"pr_number": fmt.Sprintf("%d", pr.Number),
				"state":     prState,
				"hint":      "close the existing PR or clear meta.pr_number and try again",
			},
		)
	}

	// Step 2: Create PR if not found
	prCreated := false
	if pr == nil {
		createdPR, err := createPR(ctx, cr, fsys, meta, reportPath, reportEmpty, force, sleeper, workDir)
		if err != nil {
			return nil, err
		}
		pr = createdPR
		prCreated = true

		// Append pr_created event
		appendPushEvent(eventsPath, repoID, meta.RunID, "pr_created", map[string]any{
			"pr_number": pr.Number,
			"pr_url":    pr.URL,
		})
	}

	// Step 3: Persist PR metadata to meta.json
	now := time.Now().UTC().Format(time.RFC3339)
	if err := st.UpdateMeta(repoID, meta.RunID, func(m *store.RunMeta) {
		m.PRNumber = pr.Number
		m.PRURL = pr.URL
	}); err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: failed to update meta.json with PR info: %v\n", err)
	}

	// Step 4: Sync report body (if PR exists and report is non-empty)
	action := "updated"
	if prCreated {
		action = "created"
		// If we just created with --body-file, we already synced the body
		if !reportEmpty {
			reportHash := computeReportHash(fsys, reportPath)
			if err := st.UpdateMeta(repoID, meta.RunID, func(m *store.RunMeta) {
				m.LastReportSyncAt = now
				m.LastReportHash = reportHash
			}); err != nil {
				_, _ = fmt.Fprintf(stderr, "warning: failed to update meta.json with report sync info: %v\n", err)
			}
		}
	} else {
		// PR existed, potentially sync body
		if !reportEmpty {
			bodySynced, err := syncPRBody(ctx, cr, fsys, st, meta, repoID, reportPath, pr.Number, eventsPath, stderr)
			if err != nil {
				return nil, err
			}
			if bodySynced {
				appendPushEvent(eventsPath, repoID, meta.RunID, "pr_body_synced", map[string]any{
					"pr_number": pr.Number,
				})
			}
		}
	}

	return &prResult{
		Number: pr.Number,
		URL:    pr.URL,
		Action: action,
	}, nil
}

// lookupPR looks up an existing PR by number (from meta) or by branch.
// Returns (nil, "", nil) if no PR found.
// Returns (pr, state, nil) if PR found.
// Returns (nil, "", error) on lookup failure.
func lookupPR(ctx context.Context, cr exec.CommandRunner, workDir string, meta *store.RunMeta) (*ghPRView, string, error) {
	// Step 1: If meta has pr_number, try that first
	if meta.PRNumber != 0 {
		pr, err := viewPRByNumber(ctx, cr, workDir, meta.PRNumber)
		if err == nil {
			return pr, pr.State, nil
		}
		// Fallthrough to branch lookup on error
	}

	// Step 2: Try branch lookup
	pr, err := viewPRByBranch(ctx, cr, workDir, meta.Branch)
	if err != nil {
		// No PR found
		return nil, "", nil
	}

	return pr, pr.State, nil
}

// viewPRByNumber runs: gh pr view <number> --json number,url,state
func viewPRByNumber(ctx context.Context, cr exec.CommandRunner, workDir string, prNumber int) (*ghPRView, error) {
	result, err := cr.Run(ctx, "gh", []string{
		"pr", "view", fmt.Sprintf("%d", prNumber),
		"--json", "number,url,state",
	}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("gh pr view exited with code %d: %s", result.ExitCode, result.Stderr)
	}

	var pr ghPRView
	if err := json.Unmarshal([]byte(result.Stdout), &pr); err != nil {
		return nil, fmt.Errorf("failed to parse gh pr view output: %w", err)
	}

	return &pr, nil
}

// viewPRByBranch runs: gh pr view --head <branch> --json number,url,state
func viewPRByBranch(ctx context.Context, cr exec.CommandRunner, workDir, branch string) (*ghPRView, error) {
	result, err := cr.Run(ctx, "gh", []string{
		"pr", "view",
		"--head", branch,
		"--json", "number,url,state",
	}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("gh pr view exited with code %d: %s", result.ExitCode, result.Stderr)
	}

	var pr ghPRView
	if err := json.Unmarshal([]byte(result.Stdout), &pr); err != nil {
		return nil, fmt.Errorf("failed to parse gh pr view output: %w", err)
	}

	return &pr, nil
}

// createPR creates a new PR and returns its info.
// Uses --body-file if report is non-empty, otherwise placeholder body (with --force).
func createPR(
	ctx context.Context,
	cr exec.CommandRunner,
	fsys fs.FS,
	meta *store.RunMeta,
	reportPath string,
	reportEmpty bool,
	force bool,
	sleeper Sleeper,
	workDir string,
) (*ghPRView, error) {
	// Build title
	title := "[agency] " + meta.Title
	if meta.Title == "" {
		title = "[agency] " + meta.Branch
	}

	// Build args
	args := []string{
		"pr", "create",
		"--base", meta.ParentBranch,
		"--head", meta.Branch,
		"--title", title,
	}

	// Body: use --body-file if report exists and non-empty, else placeholder
	if !reportEmpty {
		args = append(args, "--body-file", reportPath)
	} else {
		// Only allowed with --force (already validated in preflight)
		placeholder := fmt.Sprintf(
			"agency: report missing/empty (run_id=%s, branch=%s). see workspace .agency/report.md",
			meta.RunID, meta.Branch,
		)
		args = append(args, "--body", placeholder)
	}

	// Run gh pr create
	result, err := cr.Run(ctx, "gh", args, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return nil, errors.Wrap(errors.EGHPRCreateFailed, "gh pr create failed to start", err)
	}
	if result.ExitCode != 0 {
		return nil, errors.NewWithDetails(
			errors.EGHPRCreateFailed,
			fmt.Sprintf("gh pr create failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{
				"exit_code": fmt.Sprintf("%d", result.ExitCode),
				"stderr":    result.Stderr,
			},
		)
	}

	// Do NOT parse stdout from gh pr create. Instead, look up the PR by branch.
	// Retry with backoff per spec.
	pr, err := viewPRWithRetry(ctx, cr, workDir, meta.Branch, sleeper)
	if err != nil {
		return nil, err
	}

	// Verify state is OPEN
	if pr.State != "OPEN" {
		return nil, errors.NewWithDetails(
			errors.EPRNotOpen,
			fmt.Sprintf("PR #%d was created but state is %s (expected OPEN)", pr.Number, pr.State),
			map[string]string{
				"pr_number": fmt.Sprintf("%d", pr.Number),
				"state":     pr.State,
			},
		)
	}

	return pr, nil
}

// viewPRWithRetry attempts to view a PR by branch with retries.
// Per spec: try 3 times with delays of 0, 500ms, 1500ms.
func viewPRWithRetry(ctx context.Context, cr exec.CommandRunner, workDir, branch string, sleeper Sleeper) (*ghPRView, error) {
	delays := []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond}
	var lastErr error

	for _, delay := range delays {
		if delay > 0 {
			sleeper.Sleep(delay)
		}

		pr, err := viewPRByBranch(ctx, cr, workDir, branch)
		if err == nil {
			return pr, nil
		}
		lastErr = err
		// Continue to next retry if any remain
	}

	return nil, errors.NewWithDetails(
		errors.EGHPRViewFailed,
		"failed to view PR after create (retries exhausted)",
		map[string]string{
			"branch":     branch,
			"last_error": lastErr.Error(),
		},
	)
}

// syncPRBody syncs the report to the PR body if the hash has changed.
// Returns true if body was synced, false if skipped.
func syncPRBody(
	ctx context.Context,
	cr exec.CommandRunner,
	fsys fs.FS,
	st *store.Store,
	meta *store.RunMeta,
	repoID string,
	reportPath string,
	prNumber int,
	eventsPath string,
	stderr io.Writer,
) (bool, error) {
	// Compute current report hash
	reportHash := computeReportHash(fsys, reportPath)
	if reportHash == "" {
		// Report doesn't exist or can't be read; skip sync
		return false, nil
	}

	// Check if hash matches meta.last_report_hash
	if reportHash == meta.LastReportHash {
		// No change, skip edit
		return false, nil
	}

	// Run gh pr edit
	workDir := meta.WorktreePath
	result, err := cr.Run(ctx, "gh", []string{
		"pr", "edit", fmt.Sprintf("%d", prNumber),
		"--body-file", reportPath,
	}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return false, errors.Wrap(errors.EGHPREditFailed, "gh pr edit failed to start", err)
	}
	if result.ExitCode != 0 {
		return false, errors.NewWithDetails(
			errors.EGHPREditFailed,
			fmt.Sprintf("gh pr edit failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{
				"exit_code": fmt.Sprintf("%d", result.ExitCode),
				"pr_number": fmt.Sprintf("%d", prNumber),
				"stderr":    result.Stderr,
			},
		)
	}

	// Update meta with sync info
	now := time.Now().UTC().Format(time.RFC3339)
	if err := st.UpdateMeta(repoID, meta.RunID, func(m *store.RunMeta) {
		m.LastReportSyncAt = now
		m.LastReportHash = reportHash
	}); err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: failed to update meta.json with report sync info: %v\n", err)
	}

	return true, nil
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
