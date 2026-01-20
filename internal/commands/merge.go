// Package commands implements agency CLI commands.
package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/archive"
	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/events"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/NielsdaWheelz/agency/internal/verify"
)

// MergeStrategy represents the merge strategy for gh pr merge.
type MergeStrategy string

const (
	MergeStrategySquash MergeStrategy = "squash"
	MergeStrategyMerge  MergeStrategy = "merge"
	MergeStrategyRebase MergeStrategy = "rebase"
)

// MergeOpts holds options for the merge command.
type MergeOpts struct {
	// RunID is the run identifier to merge (required).
	RunID string

	// Strategy is the merge strategy (squash/merge/rebase).
	// Default: squash
	Strategy MergeStrategy

	// Force bypasses the verify-failed prompt (still runs verify, still records failure).
	Force bool

	// AllowDirty allows merge with a dirty worktree.
	AllowDirty bool

	// NoDeleteBranch preserves the remote branch after merge.
	// By default, the branch is deleted on merge (--delete-branch passed to gh pr merge).
	NoDeleteBranch bool

	// Sleeper is an injectable sleeper for testing. If nil, uses real time.Sleep.
	Sleeper Sleeper

	// TmuxClient is an injectable tmux client for testing. If nil, uses real tmux client.
	TmuxClient tmux.Client
}

// ghPRViewFull represents the full JSON output of gh pr view with all required fields.
type ghPRViewFull struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	State       string `json:"state"` // OPEN, CLOSED, MERGED
	IsDraft     bool   `json:"isDraft"`
	Mergeable   string `json:"mergeable"`   // MERGEABLE, CONFLICTING, UNKNOWN
	HeadRefName string `json:"headRefName"` // head branch name
}

// Merge executes the agency merge command.
// In PR-06b, this implements prechecks + verify but stops before actual merge.
func Merge(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts MergeOpts, stdin io.Reader, stdout, stderr io.Writer) error {
	// Validate run_id provided
	if opts.RunID == "" {
		return errors.New(errors.EUsage, "run_id is required")
	}

	// Default strategy
	if opts.Strategy == "" {
		opts.Strategy = MergeStrategySquash
	}

	// Check for interactive TTY (stdin and stderr must be TTYs)
	if !isInteractive() {
		return errors.New(errors.ENotInteractive, "merge requires an interactive terminal; stdin and stderr must be TTYs")
	}

	// Get home directory for path resolution
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}

	// Resolve data directory
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	dataDir := dirs.DataDir

	// Create store
	st := store.NewStore(fsys, dataDir, time.Now)

	// Resolve run_id and load metadata
	runRef, meta, repoID, err := resolveRunForMerge(ctx, cr, fsys, cwd, st, opts.RunID)
	if err != nil {
		return err
	}
	_ = runRef // silence unused variable warning

	// Get events path
	eventsPath := st.EventsPath(repoID, meta.RunID)

	sleeper := opts.Sleeper
	if sleeper == nil {
		sleeper = realSleeper{}
	}

	// Append merge_started event
	appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_started", map[string]any{
		"run_id":           meta.RunID,
		"strategy":         string(opts.Strategy),
		"force":            opts.Force,
		"no_delete_branch": opts.NoDeleteBranch,
	})

	// === Precheck 1: worktree exists on disk ===
	if meta.WorktreePath == "" {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
			"error_code": string(errors.EWorktreeMissing),
			"step":       "worktree_check",
		})
		return errors.NewWithDetails(errors.EWorktreeMissing, "meta.json has empty worktree_path",
			map[string]string{"run_id": opts.RunID, "repo_id": repoID})
	}

	if _, err := os.Stat(meta.WorktreePath); os.IsNotExist(err) {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
			"error_code": string(errors.EWorktreeMissing),
			"step":       "worktree_check",
		})
		return errors.NewWithDetails(errors.EWorktreeMissing, "worktree path does not exist",
			map[string]string{"run_id": opts.RunID, "worktree_path": meta.WorktreePath})
	}

	// Acquire repo lock
	repoLock := lock.NewRepoLock(dataDir)
	unlock, err := repoLock.Lock(repoID, "merge")
	if err != nil {
		var lockErr *lock.ErrLocked
		if isLockError(err, &lockErr) {
			appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
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

	// === Precheck 2: dirty worktree gate ===
	isClean, status, err := getDirtyStatus(ctx, cr, meta.WorktreePath)
	if err != nil {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
			"error_code": string(errors.GetCode(err)),
			"step":       "dirty_check",
		})
		return err
	}
	if !isClean {
		if !opts.AllowDirty {
			appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
				"error_code": string(errors.EDirtyWorktree),
				"step":       "dirty_check",
			})
			return dirtyErrorWithContext(status)
		}
		appendMergeEvent(eventsPath, repoID, meta.RunID, "dirty_allowed", map[string]any{
			"cmd":    "merge",
			"status": status,
		})
		printDirtyWarning(stderr, status)
	}

	// Print lock acquisition message (per spec, diagnostic output to stderr)
	_, _ = fmt.Fprintln(stderr, "lock: acquired repo lock (held during verify/merge/archive)")

	// === Precheck 3: origin exists ===
	originURL, err := getOriginURLForMerge(ctx, cr, st, repoID, meta.WorktreePath)
	if err != nil {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
			"error_code": string(errors.GetCode(err)),
			"step":       "origin_check",
		})
		return err
	}

	// === Precheck 4: origin host is github.com ===
	originHost := parseOriginHost(originURL)
	if originHost != "github.com" {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
			"error_code": string(errors.EUnsupportedOriginHost),
			"step":       "origin_host_check",
			"host":       originHost,
		})
		return errors.NewWithDetails(errors.EUnsupportedOriginHost, "origin host must be github.com",
			map[string]string{"origin_url": originURL, "host": originHost})
	}

	// === Precheck 5: gh authenticated ===
	if err := checkGhAuthForMerge(ctx, cr, meta.WorktreePath); err != nil {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
			"error_code": string(errors.GetCode(err)),
			"step":       "gh_auth",
		})
		return err
	}

	// === Precheck 6: resolve owner/repo for gh -R ===
	owner, repo, ok := identity.ParseGitHubOwnerRepo(originURL)
	if !ok {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
			"error_code": string(errors.EGHRepoParseFailed),
			"step":       "repo_parse",
			"origin_url": originURL,
		})
		return errors.NewWithDetails(errors.EGHRepoParseFailed, "failed to parse owner/repo from origin URL",
			map[string]string{"origin_url": originURL})
	}
	ghRepo := fmt.Sprintf("%s/%s", owner, repo)
	repoRef := newGHRepoRef(owner, repo)

	// === Precheck 6: PR resolution ===
	pr, err := resolvePRForMerge(ctx, cr, meta, ghRepo, repoRef, eventsPath, repoID, sleeper)
	if err != nil {
		if hint := hintFromError(err); hint != "" {
			printHint(stderr, hint)
		}
		if shouldPrintPRViewHint(errors.GetCode(err)) {
			printHint(stderr, prViewHint(repoRef, meta.Branch, meta.PRNumber))
		}
		return err
	}

	// Persist PR metadata if changed
	if meta.PRNumber != pr.Number || meta.PRURL != pr.URL {
		_ = st.UpdateMeta(repoID, meta.RunID, func(m *store.RunMeta) {
			m.PRNumber = pr.Number
			m.PRURL = pr.URL
		})
	}

	// === Precheck 7: PR state and mismatch checks ===
	prStateResult, err := validatePRState(pr, meta.Branch, eventsPath, repoID, meta.RunID)
	if err != nil {
		return err
	}

	// Handle already-merged PR (idempotent path)
	if prStateResult.AlreadyMerged {
		return handleAlreadyMergedPR(ctx, cr, fsys, st, meta, repoID, pr, ghRepo, opts, stdin, stdout, stderr, eventsPath, dataDir)
	}

	// === Precheck 8: mergeability ===
	if err := checkMergeability(ctx, cr, meta.WorktreePath, ghRepo, pr.Number, sleeper, eventsPath, repoID, meta.RunID); err != nil {
		return err
	}

	// === Precheck 9: remote head up-to-date ===
	if err := checkRemoteHeadUpToDate(ctx, cr, meta.WorktreePath, meta.Branch, eventsPath, repoID, meta.RunID); err != nil {
		return err
	}

	// === All prechecks passed ===
	appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_prechecks_passed", map[string]any{
		"pr_number": pr.Number,
		"pr_url":    pr.URL,
		"branch":    meta.Branch,
	})

	// === Run verify ===
	verifyResult, verifyErr := runVerifyForMerge(ctx, fsys, st, meta, repoID, eventsPath, stderr)

	// Update meta with verify results
	if verifyResult != nil {
		_ = st.UpdateMeta(repoID, meta.RunID, func(m *store.RunMeta) {
			m.LastVerifyAt = time.Now().UTC().Format(time.RFC3339)
			if !verifyResult.OK {
				if m.Flags == nil {
					m.Flags = &store.RunMetaFlags{}
				}
				m.Flags.NeedsAttention = true
				m.Flags.NeedsAttentionReason = "verify_failed"
			}
		})
	}

	// Handle verify failure
	if verifyErr != nil || (verifyResult != nil && !verifyResult.OK) {
		if !opts.Force {
			// Append verify_continue_prompted event
			appendMergeEvent(eventsPath, repoID, meta.RunID, "verify_continue_prompted", nil)

			// Prompt user
			_, _ = fmt.Fprint(stderr, "verify failed. continue anyway? [y/N] ")
			reader := bufio.NewReader(stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				input = ""
			}
			answer := strings.TrimSpace(input)

			if strings.ToLower(answer) != "y" {
				appendMergeEvent(eventsPath, repoID, meta.RunID, "verify_continue_rejected", map[string]any{
					"answer": answer,
				})
				appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_finished", events.MergeFinishedData(false, string(errors.EScriptFailed)))
				if verifyErr != nil {
					return verifyErr
				}
				// Return E_SCRIPT_FAILED for verify failure
				if verifyResult != nil && verifyResult.TimedOut {
					return errors.New(errors.EScriptTimeout, "verify timed out")
				}
				return errors.New(errors.EScriptFailed, "verify failed")
			}

			appendMergeEvent(eventsPath, repoID, meta.RunID, "verify_continue_accepted", map[string]any{
				"answer": answer,
			})
		}
		// With --force, we continue without prompting
	}

	// === Merge confirmation prompt ===
	appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_confirm_prompted", events.MergeConfirmPromptedData())

	_, _ = fmt.Fprint(stderr, "confirm: type 'merge' to proceed: ")
	reader := bufio.NewReader(stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		input = ""
	}
	confirmation := strings.TrimSpace(input)

	if confirmation != "merge" {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_finished", events.MergeFinishedData(false, string(errors.EAborted)))
		return errors.New(errors.EAborted, "merge confirmation failed; expected 'merge'")
	}

	appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_confirmed", events.MergeConfirmedData())

	// === Execute gh pr merge ===
	strategyFlag := "--" + string(opts.Strategy)
	deleteBranch := !opts.NoDeleteBranch // Delete branch by default

	appendMergeEvent(eventsPath, repoID, meta.RunID, "gh_merge_started", events.GHMergeStartedData(pr.Number, pr.URL, string(opts.Strategy)))

	// Capture merge output to logs/merge.log
	mergeLogPath := filepath.Join(st.RunLogsDir(repoID, meta.RunID), "merge.log")
	mergeErr := executeGHMerge(ctx, cr, meta.WorktreePath, ghRepo, pr.Number, strategyFlag, mergeLogPath, deleteBranch)

	if mergeErr != nil {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "gh_merge_finished", events.GHMergeFinishedData(false, pr.Number, pr.URL))
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_finished", events.MergeFinishedData(false, string(errors.EGHPRMergeFailed)))
		return mergeErr
	}

	// === Confirm PR reached MERGED state ===
	confirmed, confirmErr := confirmPRMerged(ctx, cr, meta.WorktreePath, ghRepo, pr.Number, sleeper)
	if confirmErr != nil || !confirmed {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "gh_merge_finished", events.GHMergeFinishedData(false, pr.Number, pr.URL))
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_finished", events.MergeFinishedData(false, string(errors.EGHPRMergeFailed)))
		if confirmErr != nil {
			return confirmErr
		}
		return errors.NewWithDetails(errors.EGHPRMergeFailed,
			"gh pr merge succeeded but could not confirm MERGED state",
			map[string]string{"hint": fmt.Sprintf("re-run `agency merge %s`; it may have merged but confirmation failed", meta.RunID)})
	}

	appendMergeEvent(eventsPath, repoID, meta.RunID, "gh_merge_finished", events.GHMergeFinishedData(true, pr.Number, pr.URL))

	// === Set merged_at ===
	_ = st.UpdateMeta(repoID, meta.RunID, func(m *store.RunMeta) {
		if m.Archive == nil {
			m.Archive = &store.RunMetaArchive{}
		}
		m.Archive.MergedAt = time.Now().UTC().Format(time.RFC3339)
	})

	// === Run archive pipeline ===
	return runArchivePipeline(ctx, cr, fsys, st, meta, repoID, ghRepo, opts, stdout, stderr, eventsPath, dataDir, true)
}

// handleAlreadyMergedPR handles the idempotent path when PR is already merged.
// Skips verify, mergeability, and remote head checks. Still requires confirmation.
func handleAlreadyMergedPR(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, st *store.Store, meta *store.RunMeta, repoID string, pr *ghPRViewFull, ghRepo string, opts MergeOpts, stdin io.Reader, stdout, stderr io.Writer, eventsPath, dataDir string) error {
	// Append already merged event
	appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_already_merged", events.MergeAlreadyMergedData(pr.Number, pr.URL))

	_, _ = fmt.Fprintf(stderr, "note: PR #%d is already merged; proceeding to archive\n", pr.Number)

	// Still require typed confirmation (archive is destructive)
	appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_confirm_prompted", events.MergeConfirmPromptedData())

	_, _ = fmt.Fprint(stderr, "confirm: type 'merge' to proceed: ")
	reader := bufio.NewReader(stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		input = ""
	}
	confirmation := strings.TrimSpace(input)

	if confirmation != "merge" {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_finished", events.MergeFinishedData(false, string(errors.EAborted)))
		return errors.New(errors.EAborted, "merge confirmation failed; expected 'merge'")
	}

	appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_confirmed", events.MergeConfirmedData())

	// Set merged_at if missing
	_ = st.UpdateMeta(repoID, meta.RunID, func(m *store.RunMeta) {
		if m.Archive == nil {
			m.Archive = &store.RunMetaArchive{}
		}
		if m.Archive.MergedAt == "" {
			m.Archive.MergedAt = time.Now().UTC().Format(time.RFC3339)
		}
	})

	// Run archive pipeline
	return runArchivePipeline(ctx, cr, fsys, st, meta, repoID, ghRepo, opts, stdout, stderr, eventsPath, dataDir, false)
}

// executeGHMerge runs gh pr merge and captures output to merge.log.
// If deleteBranch is true, passes --delete-branch to gh pr merge.
func executeGHMerge(ctx context.Context, cr exec.CommandRunner, workDir, ghRepo string, prNumber int, strategyFlag, mergeLogPath string, deleteBranch bool) error {
	// Ensure logs directory exists (non-fatal; continue anyway)
	logsDir := filepath.Dir(mergeLogPath)
	_ = os.MkdirAll(logsDir, 0o700)

	args := []string{
		"pr", "merge", fmt.Sprintf("%d", prNumber),
		"-R", ghRepo,
		strategyFlag,
	}
	if deleteBranch {
		args = append(args, "--delete-branch")
	}

	result, err := cr.Run(ctx, "gh", args, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})

	// Write output to merge.log regardless of result
	deleteBranchStr := ""
	if deleteBranch {
		deleteBranchStr = " --delete-branch"
	}
	logContent := fmt.Sprintf("=== gh pr merge %d -R %s %s%s ===\n", prNumber, ghRepo, strategyFlag, deleteBranchStr)
	logContent += fmt.Sprintf("Exit code: %d\n", result.ExitCode)
	if result.Stdout != "" {
		logContent += fmt.Sprintf("\n=== stdout ===\n%s", result.Stdout)
	}
	if result.Stderr != "" {
		logContent += fmt.Sprintf("\n=== stderr ===\n%s", result.Stderr)
	}
	_ = os.WriteFile(mergeLogPath, []byte(logContent), 0o644)

	if err != nil {
		return errors.Wrap(errors.EGHPRMergeFailed, "gh pr merge failed", err)
	}
	if result.ExitCode != 0 {
		return errors.NewWithDetails(errors.EGHPRMergeFailed,
			fmt.Sprintf("gh pr merge exited %d", result.ExitCode),
			map[string]string{"stderr": truncateString(result.Stderr, 256)})
	}

	return nil
}

// confirmPRMerged confirms the PR reached MERGED state with retries.
// Backoff: 250ms, 750ms, 1500ms
func confirmPRMerged(ctx context.Context, cr exec.CommandRunner, workDir, ghRepo string, prNumber int, sleeper Sleeper) (bool, error) {
	delays := []time.Duration{250 * time.Millisecond, 750 * time.Millisecond, 1500 * time.Millisecond}

	for i, delay := range delays {
		if i > 0 {
			sleeper.Sleep(delay)
		}

		result, err := cr.Run(ctx, "gh", []string{
			"pr", "view", fmt.Sprintf("%d", prNumber),
			"-R", ghRepo,
			"--json", "state",
		}, exec.RunOpts{
			Dir: workDir,
			Env: nonInteractiveEnv(),
		})
		if err != nil {
			continue // Retry on exec error
		}
		if result.ExitCode != 0 {
			continue // Retry on non-zero exit
		}

		var stateResp struct {
			State string `json:"state"`
		}
		if json.Unmarshal([]byte(result.Stdout), &stateResp) != nil {
			continue // Retry on parse error
		}

		if stateResp.State == "MERGED" {
			return true, nil
		}
	}

	return false, nil
}

// runArchivePipeline runs the archive pipeline after successful merge.
// mergeJustHappened indicates if gh pr merge was just executed (vs already-merged path).
func runArchivePipeline(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, st *store.Store, meta *store.RunMeta, repoID, ghRepo string, opts MergeOpts, stdout, stderr io.Writer, eventsPath, dataDir string, mergeJustHappened bool) error {
	// Append archive_started event
	appendMergeEvent(eventsPath, repoID, meta.RunID, "archive_started", events.ArchiveStartedData(meta.RunID))

	// Find repo root (best-effort for archive)
	repoRoot, repoRootErr := git.GetRepoRoot(ctx, cr, meta.WorktreePath)
	repoRootPath := ""
	if repoRootErr == nil {
		repoRootPath = repoRoot.Path
	}

	// Load agency.json for archive script
	agencyJSON, err := config.LoadAgencyConfig(fsys, meta.WorktreePath)
	if err != nil {
		// If config can't be loaded, still try with empty script
		agencyJSON = config.AgencyConfig{}
	}

	// Create tmux client
	tmuxClient := opts.TmuxClient
	if tmuxClient == nil {
		tmuxClient = tmux.NewExecClient(cr)
	}

	archiveCfg := archive.Config{
		Meta:          meta,
		RepoRoot:      repoRootPath,
		DataDir:       dataDir,
		ArchiveScript: agencyJSON.Scripts.Archive.Path,
		Timeout:       agencyJSON.Scripts.Archive.Timeout,
	}

	archiveDeps := archive.Deps{
		CR:         cr,
		TmuxClient: tmuxClient,
		Stdout:     stdout,
		Stderr:     stderr,
	}

	result := archive.Archive(ctx, archiveCfg, archiveDeps, st)

	// Append archive event based on result
	if result.Success() {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "archive_finished", events.ArchiveFinishedData(true))
	} else {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "archive_failed",
			events.ArchiveFailedData(result.ScriptOK, result.TmuxOK, result.DeleteOK, result.ScriptReason, result.TmuxReason, result.DeleteReason))
	}

	// Update meta on archive success
	if result.DeleteOK {
		_ = st.UpdateMeta(repoID, meta.RunID, func(m *store.RunMeta) {
			if m.Archive == nil {
				m.Archive = &store.RunMetaArchive{}
			}
			m.Archive.ArchivedAt = time.Now().UTC().Format(time.RFC3339)
		})
	}

	// Append merge_finished event
	if result.Success() {
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_finished", events.MergeFinishedData(true, ""))
		// Print success message
		_, _ = fmt.Fprintf(stdout, "merged: %s\n", meta.RunID)
		_, _ = fmt.Fprintf(stdout, "pr: %s\n", meta.PRURL)
		if result.LogPath != "" {
			_, _ = fmt.Fprintf(stdout, "log: %s\n", result.LogPath)
		}
		return nil
	}

	// Archive failed but merge may have succeeded
	appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_finished", events.MergeFinishedData(false, string(errors.EArchiveFailed)))

	if mergeJustHappened {
		// Merge succeeded but archive failed
		return errors.NewWithDetails(errors.EArchiveFailed,
			"merge succeeded; archive failed",
			map[string]string{"detail": truncateString(result.ToError().Error(), 256)})
	}

	// Already-merged path: archive failed
	return result.ToError()
}

// truncateString truncates s to maxLen characters.
func truncateString(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// resolveRunForMerge resolves the run identifier and loads metadata.
func resolveRunForMerge(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, st *store.Store, runID string) (interface{}, *store.RunMeta, string, error) {
	// Use the same resolution logic as push
	return resolveRunForPush(ctx, cr, fsys, cwd, st, runID)
}

// getOriginURLForMerge gets the origin URL, preferring repo.json if available.
func getOriginURLForMerge(ctx context.Context, cr exec.CommandRunner, st *store.Store, repoID, worktreePath string) (string, error) {
	// Try to read repo.json first
	repoRecordPath := st.RepoRecordPath(repoID)
	data, err := st.FS.ReadFile(repoRecordPath)
	if err == nil {
		var record store.RepoRecord
		if json.Unmarshal(data, &record) == nil && record.OriginURL != "" {
			return record.OriginURL, nil
		}
	}

	// Fallback to git remote
	result, err := cr.Run(ctx, "git", []string{"-C", worktreePath, "remote", "get-url", "origin"}, exec.RunOpts{
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return "", errors.New(errors.ENoOrigin, "git remote 'origin' not configured")
	}
	if result.ExitCode != 0 {
		return "", errors.New(errors.ENoOrigin, "git remote 'origin' not configured")
	}

	url := strings.TrimSpace(result.Stdout)
	if url == "" {
		return "", errors.New(errors.ENoOrigin, "git remote 'origin' not configured")
	}

	return url, nil
}

// parseOriginHost extracts the hostname from an origin URL.
func parseOriginHost(originURL string) string {
	// Handle scp-like URLs: git@github.com:owner/repo.git
	if strings.Contains(originURL, "@") && strings.Contains(originURL, ":") && !strings.Contains(originURL, "://") {
		atIdx := strings.Index(originURL, "@")
		colonIdx := strings.Index(originURL, ":")
		if colonIdx > atIdx {
			return originURL[atIdx+1 : colonIdx]
		}
	}

	// Handle https URLs: https://github.com/owner/repo.git
	if strings.HasPrefix(originURL, "https://") {
		rest := strings.TrimPrefix(originURL, "https://")
		slashIdx := strings.Index(rest, "/")
		if slashIdx > 0 {
			return rest[:slashIdx]
		}
	}

	// Handle http URLs: http://github.com/owner/repo.git
	if strings.HasPrefix(originURL, "http://") {
		rest := strings.TrimPrefix(originURL, "http://")
		slashIdx := strings.Index(rest, "/")
		if slashIdx > 0 {
			return rest[:slashIdx]
		}
	}

	return ""
}

// checkGhAuthForMerge verifies gh is installed and authenticated.
func checkGhAuthForMerge(ctx context.Context, cr exec.CommandRunner, workDir string) error {
	// Check gh is installed
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

// resolvePRForMerge resolves the PR for a run.
// Returns the PR info or an error.
func resolvePRForMerge(ctx context.Context, cr exec.CommandRunner, meta *store.RunMeta, ghRepo string, repoRef ghRepoRef, eventsPath, repoID string, sleeper Sleeper) (*ghPRViewFull, error) {
	workDir := meta.WorktreePath
	head := headRef(repoRef, meta.Branch)

	// Try by stored PR number first
	if meta.PRNumber != 0 {
		pr, err := viewPRByNumberFullWithRetry(ctx, cr, workDir, ghRepo, head, meta.PRNumber, sleeper, eventsPath, repoID, meta.RunID)
		if err == nil {
			return pr, nil
		}
		// Check if it's a "not found" error vs schema error
		if isGHPRViewSchemaError(err) {
			appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
				"error_code": string(errors.EGHPRViewFailed),
				"step":       "pr_resolution",
				"error":      err.Error(),
			})
			return nil, errors.Wrap(errors.EGHPRViewFailed, "gh pr view failed or returned invalid schema", err)
		}
		// Fallthrough to branch lookup
	}

	// Try by head branch
	pr, err := viewPRByHeadFullWithRetry(ctx, cr, workDir, ghRepo, head, meta.Branch, sleeper, eventsPath, repoID, meta.RunID)
	if err != nil {
		// Check if it's a "not found" error
		if isGHPRNotFound(err) {
			appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
				"error_code": string(errors.ENoPR),
				"step":       "pr_resolution",
			})
			return nil, errors.NewWithDetails(errors.ENoPR, "no PR exists for this run",
				map[string]string{"hint": fmt.Sprintf("run: agency push %s", meta.RunID)})
		}
		// Schema or other error
		appendMergeEvent(eventsPath, repoID, meta.RunID, "merge_failed", map[string]any{
			"error_code": string(errors.EGHPRViewFailed),
			"step":       "pr_resolution",
			"error":      err.Error(),
		})
		return nil, errors.Wrap(errors.EGHPRViewFailed, "gh pr view failed or returned invalid schema", err)
	}

	return pr, nil
}

func viewPRByNumberFullAttempt(ctx context.Context, cr exec.CommandRunner, workDir, ghRepo string, prNumber int) (*ghPRViewFull, prViewAttempt) {
	result, err := cr.Run(ctx, "gh", []string{
		"pr", "view", fmt.Sprintf("%d", prNumber),
		"-R", ghRepo,
		"--json", "number,url,state,isDraft,mergeable,headRefName",
	}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return nil, prViewAttempt{ExitCode: exec.ExitStartFail, Err: fmt.Errorf("gh pr view exec error: %w", err)}
	}
	if result.ExitCode != 0 {
		return nil, prViewAttempt{
			ExitCode: result.ExitCode,
			Stderr:   result.Stderr,
			Err:      fmt.Errorf("gh pr view exited %d: %s", result.ExitCode, result.Stderr),
		}
	}

	pr, parseErr := parseGHPRViewFull(result.Stdout)
	if parseErr != nil {
		return nil, prViewAttempt{
			ExitCode: result.ExitCode,
			Stderr:   result.Stderr,
			Err:      parseErr,
		}
	}

	return pr, prViewAttempt{}
}

func viewPRByHeadFullAttempt(ctx context.Context, cr exec.CommandRunner, workDir, ghRepo, headArg string) (*ghPRViewFull, prViewAttempt) {
	result, err := cr.Run(ctx, "gh", []string{
		"pr", "list",
		"--head", headArg,
		"-R", ghRepo,
		"--state", "all",
		"--json", "number,url,state",
	}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return nil, prViewAttempt{ExitCode: exec.ExitStartFail, Err: fmt.Errorf("gh pr list exec error: %w", err)}
	}
	if result.ExitCode != 0 {
		return nil, prViewAttempt{
			ExitCode: result.ExitCode,
			Stderr:   result.Stderr,
			Err:      fmt.Errorf("gh pr list exited %d: %s", result.ExitCode, result.Stderr),
		}
	}

	var prs []ghPRView
	if err := json.Unmarshal([]byte(result.Stdout), &prs); err != nil {
		return nil, prViewAttempt{
			ExitCode: result.ExitCode,
			Stderr:   result.Stderr,
			Err:      fmt.Errorf("failed to parse gh pr list output: %w", err),
		}
	}
	if len(prs) == 0 {
		return nil, prViewAttempt{ExitCode: result.ExitCode, Err: errPRNotFound}
	}
	if len(prs) > 1 {
		return nil, prViewAttempt{
			ExitCode: result.ExitCode,
			Err:      fmt.Errorf("multiple PRs found for head %q", headArg),
		}
	}

	pr, attempt := viewPRByNumberFullAttempt(ctx, cr, workDir, ghRepo, prs[0].Number)
	if attempt.Err != nil {
		return nil, attempt
	}
	return pr, prViewAttempt{}
}

func viewPRByNumberFullWithRetry(
	ctx context.Context,
	cr exec.CommandRunner,
	workDir, ghRepo, head string,
	prNumber int,
	sleeper Sleeper,
	eventsPath, repoID, runID string,
) (*ghPRViewFull, error) {
	return viewPRFullWithRetry(ctx, cr, workDir, ghRepo, head, sleeper, eventsPath, repoID, runID,
		func() (*ghPRViewFull, prViewAttempt) {
			return viewPRByNumberFullAttempt(ctx, cr, workDir, ghRepo, prNumber)
		})
}

func viewPRByHeadFullWithRetry(
	ctx context.Context,
	cr exec.CommandRunner,
	workDir, ghRepo, head, branch string,
	sleeper Sleeper,
	eventsPath, repoID, runID string,
) (*ghPRViewFull, error) {
	return viewPRFullWithRetry(ctx, cr, workDir, ghRepo, head, sleeper, eventsPath, repoID, runID,
		func() (*ghPRViewFull, prViewAttempt) {
			headArg := head
			if headArg == "" {
				headArg = branch
			}
			return viewPRByHeadFullAttempt(ctx, cr, workDir, ghRepo, headArg)
		})
}

func viewPRFullWithRetry(
	ctx context.Context,
	cr exec.CommandRunner,
	workDir, ghRepo, head string,
	sleeper Sleeper,
	eventsPath, repoID, runID string,
	view func() (*ghPRViewFull, prViewAttempt),
) (*ghPRViewFull, error) {
	var lastErr error

	for i, baseDelay := range prViewRetryDelays {
		delay := jitterDelay(baseDelay)
		if i > 0 && delay > 0 {
			sleeper.Sleep(delay)
		}

		pr, attempt := view()
		if eventsPath != "" {
			appendMergeEvent(eventsPath, repoID, runID, "pr_resolution_attempt", map[string]any{
				"owner_repo":  ghRepo,
				"head":        head,
				"attempt":     i + 1,
				"sleep_ms":    delay.Milliseconds(),
				"exit_code":   attempt.ExitCode,
				"stderr_tail": truncateString(attempt.Stderr, 256),
				"error":       errString(attempt.Err),
			})
		}
		if attempt.Err == nil {
			return pr, nil
		}

		lastErr = attempt.Err
	}

	return nil, lastErr
}

// parseGHPRViewFull parses gh pr view JSON output.
func parseGHPRViewFull(jsonStr string) (*ghPRViewFull, error) {
	var pr ghPRViewFull
	if err := json.Unmarshal([]byte(jsonStr), &pr); err != nil {
		return nil, fmt.Errorf("failed to parse gh pr view output: %w (schema_error)", err)
	}

	// Validate required fields
	if pr.Number == 0 {
		return nil, fmt.Errorf("gh pr view missing required field: number (schema_error)")
	}
	if pr.URL == "" {
		return nil, fmt.Errorf("gh pr view missing required field: url (schema_error)")
	}
	if pr.State == "" {
		return nil, fmt.Errorf("gh pr view missing required field: state (schema_error)")
	}
	// Validate state enum
	if pr.State != "OPEN" && pr.State != "CLOSED" && pr.State != "MERGED" {
		return nil, fmt.Errorf("gh pr view unexpected state value: %s (schema_error)", pr.State)
	}
	// Validate mergeable enum (if not empty)
	if pr.Mergeable != "" && pr.Mergeable != "MERGEABLE" && pr.Mergeable != "CONFLICTING" && pr.Mergeable != "UNKNOWN" {
		return nil, fmt.Errorf("gh pr view unexpected mergeable value: %s (schema_error)", pr.Mergeable)
	}

	return &pr, nil
}

// isGHPRViewSchemaError checks if the error is a schema/parsing error vs a "not found" error.
func isGHPRViewSchemaError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "schema_error")
}

// isGHPRNotFound checks if the error indicates "no PR found".
func isGHPRNotFound(err error) bool {
	return isPRNotFound(err)
}

// prStateResult holds the result of PR state validation.
type prStateResult struct {
	AlreadyMerged bool // true if PR state is MERGED
}

// validatePRState validates PR state and head branch match.
// Returns prStateResult with AlreadyMerged=true if PR is already merged (idempotent path).
// Returns error for CLOSED or DRAFT PRs, or branch mismatch.
func validatePRState(pr *ghPRViewFull, expectedBranch, eventsPath, repoID, runID string) (*prStateResult, error) {
	result := &prStateResult{}

	// Check state
	switch pr.State {
	case "OPEN":
		// Good, continue with normal flow
	case "MERGED":
		// Idempotent path: PR already merged, skip verify/mergeability but still archive
		result.AlreadyMerged = true
		return result, nil
	case "CLOSED":
		appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
			"error_code": string(errors.EPRNotOpen),
			"step":       "pr_state_check",
			"state":      pr.State,
		})
		return nil, errors.NewWithDetails(errors.EPRNotOpen, fmt.Sprintf("PR #%d is CLOSED (not merged)", pr.Number),
			map[string]string{"pr_number": fmt.Sprintf("%d", pr.Number), "state": pr.State})
	}

	// Check draft status (only for OPEN PRs)
	if pr.IsDraft {
		appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
			"error_code": string(errors.EPRDraft),
			"step":       "pr_draft_check",
		})
		return nil, errors.NewWithDetails(errors.EPRDraft, fmt.Sprintf("PR #%d is a draft", pr.Number),
			map[string]string{"pr_number": fmt.Sprintf("%d", pr.Number)})
	}

	// Check head branch matches (only for OPEN PRs)
	if pr.HeadRefName != expectedBranch {
		appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
			"error_code":      string(errors.EPRMismatch),
			"step":            "pr_branch_check",
			"expected_branch": expectedBranch,
			"pr_head_branch":  pr.HeadRefName,
		})
		return nil, errors.NewWithDetails(errors.EPRMismatch,
			fmt.Sprintf("PR #%d head branch %q does not match expected branch %q", pr.Number, pr.HeadRefName, expectedBranch),
			map[string]string{
				"pr_number":       fmt.Sprintf("%d", pr.Number),
				"expected_branch": expectedBranch,
				"pr_head_branch":  pr.HeadRefName,
				"hint":            "repair PR or meta.json and retry",
			})
	}

	return result, nil
}

// checkMergeability checks PR mergeability with retries for UNKNOWN.
func checkMergeability(ctx context.Context, cr exec.CommandRunner, workDir, ghRepo string, prNumber int, sleeper Sleeper, eventsPath, repoID, runID string) error {
	delays := []time.Duration{0, 1 * time.Second, 2 * time.Second, 2 * time.Second}
	var lastMergeable string

	for i, delay := range delays {
		if delay > 0 {
			sleeper.Sleep(delay)
		}

		// Query just mergeable field
		result, err := cr.Run(ctx, "gh", []string{
			"pr", "view", fmt.Sprintf("%d", prNumber),
			"-R", ghRepo,
			"--json", "mergeable",
		}, exec.RunOpts{
			Dir: workDir,
			Env: nonInteractiveEnv(),
		})
		if err != nil {
			appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
				"error_code": string(errors.EGHPRViewFailed),
				"step":       "mergeability_check",
				"error":      err.Error(),
			})
			return errors.Wrap(errors.EGHPRViewFailed, "gh pr view failed during mergeability check", err)
		}
		if result.ExitCode != 0 {
			appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
				"error_code": string(errors.EGHPRViewFailed),
				"step":       "mergeability_check",
				"stderr":     result.Stderr,
			})
			return errors.NewWithDetails(errors.EGHPRViewFailed, "gh pr view failed during mergeability check",
				map[string]string{"stderr": result.Stderr})
		}

		var mergeableResp struct {
			Mergeable string `json:"mergeable"`
		}
		if err := json.Unmarshal([]byte(result.Stdout), &mergeableResp); err != nil {
			appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
				"error_code": string(errors.EGHPRViewFailed),
				"step":       "mergeability_check",
				"error":      err.Error(),
			})
			return errors.Wrap(errors.EGHPRViewFailed, "failed to parse mergeability response", err)
		}

		lastMergeable = mergeableResp.Mergeable

		switch lastMergeable {
		case "MERGEABLE":
			return nil
		case "CONFLICTING":
			appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
				"error_code": string(errors.EPRNotMergeable),
				"step":       "mergeability_check",
				"mergeable":  lastMergeable,
			})
			return errors.NewWithDetails(errors.EPRNotMergeable,
				fmt.Sprintf("PR #%d has conflicts and cannot be merged", prNumber),
				map[string]string{"pr_number": fmt.Sprintf("%d", prNumber), "mergeable": lastMergeable})
		case "UNKNOWN":
			// Retry
			if i < len(delays)-1 {
				continue
			}
		default:
			appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
				"error_code": string(errors.EGHPRViewFailed),
				"step":       "mergeability_check",
				"mergeable":  lastMergeable,
			})
			return errors.NewWithDetails(errors.EGHPRViewFailed,
				fmt.Sprintf("unexpected mergeable value: %s", lastMergeable),
				map[string]string{"mergeable": lastMergeable})
		}
	}

	// Still UNKNOWN after retries
	appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
		"error_code": string(errors.EPRMergeabilityUnknown),
		"step":       "mergeability_check",
		"mergeable":  lastMergeable,
	})
	return errors.NewWithDetails(errors.EPRMergeabilityUnknown,
		fmt.Sprintf("PR #%d mergeability is UNKNOWN after retries", prNumber),
		map[string]string{"pr_number": fmt.Sprintf("%d", prNumber)})
}

// checkRemoteHeadUpToDate verifies local HEAD matches remote branch.
func checkRemoteHeadUpToDate(ctx context.Context, cr exec.CommandRunner, workDir, branch, eventsPath, repoID, runID string) error {
	// Fetch the specific branch
	fetchResult, err := cr.Run(ctx, "git", []string{
		"-C", workDir,
		"fetch", "origin",
		fmt.Sprintf("refs/heads/%s:refs/remotes/origin/%s", branch, branch),
	}, exec.RunOpts{
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
			"error_code": string(errors.EGitFetchFailed),
			"step":       "remote_head_check",
			"error":      err.Error(),
		})
		return errors.Wrap(errors.EGitFetchFailed, "git fetch failed", err)
	}
	if fetchResult.ExitCode != 0 {
		appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
			"error_code": string(errors.EGitFetchFailed),
			"step":       "remote_head_check",
			"stderr":     fetchResult.Stderr,
		})
		return errors.NewWithDetails(errors.EGitFetchFailed, "git fetch failed",
			map[string]string{"stderr": fetchResult.Stderr})
	}

	// Get local HEAD sha
	localResult, err := cr.Run(ctx, "git", []string{"-C", workDir, "rev-parse", "HEAD"}, exec.RunOpts{
		Env: nonInteractiveEnv(),
	})
	if err != nil || localResult.ExitCode != 0 {
		appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
			"error_code": string(errors.EInternal),
			"step":       "remote_head_check",
			"error":      "failed to get local HEAD",
		})
		return errors.New(errors.EInternal, "failed to get local HEAD sha")
	}
	localSHA := strings.TrimSpace(localResult.Stdout)

	// Get remote sha
	remoteRef := fmt.Sprintf("refs/remotes/origin/%s", branch)
	remoteResult, err := cr.Run(ctx, "git", []string{"-C", workDir, "rev-parse", remoteRef}, exec.RunOpts{
		Env: nonInteractiveEnv(),
	})
	if err != nil || remoteResult.ExitCode != 0 {
		appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
			"error_code":     string(errors.ERemoteOutOfDate),
			"step":           "remote_head_check",
			"local_sha":      localSHA,
			"remote_present": false,
		})
		return errors.NewWithDetails(errors.ERemoteOutOfDate,
			fmt.Sprintf("remote branch missing; run: agency push %s", runID),
			map[string]string{"hint": fmt.Sprintf("run: agency push %s", runID)})
	}
	remoteSHA := strings.TrimSpace(remoteResult.Stdout)

	// Compare
	if localSHA != remoteSHA {
		appendMergeEvent(eventsPath, repoID, runID, "merge_failed", map[string]any{
			"error_code":     string(errors.ERemoteOutOfDate),
			"step":           "remote_head_check",
			"local_sha":      localSHA,
			"remote_sha":     remoteSHA,
			"remote_present": true,
		})
		return errors.NewWithDetails(errors.ERemoteOutOfDate,
			fmt.Sprintf("local head differs from origin/%s; run: agency push %s", branch, runID),
			map[string]string{
				"local_sha":  localSHA,
				"remote_sha": remoteSHA,
				"hint":       fmt.Sprintf("run: agency push %s", runID),
			})
	}

	return nil
}

// runVerifyForMerge runs the verify script and returns the result.
func runVerifyForMerge(ctx context.Context, fsys fs.FS, st *store.Store, meta *store.RunMeta, repoID, eventsPath string, stderr io.Writer) (*store.VerifyRecord, error) {
	worktreePath := meta.WorktreePath
	runDir := st.RunDir(repoID, meta.RunID)
	logPath := filepath.Join(st.RunLogsDir(repoID, meta.RunID), "verify.log")
	recordPath := st.VerifyRecordPath(repoID, meta.RunID)
	verifyJSONPath := filepath.Join(worktreePath, ".agency", "out", "verify.json")

	// Load agency.json to get verify script and timeout
	agencyJSON, err := config.LoadAgencyConfig(fsys, worktreePath)
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to load agency.json for verify", err)
	}

	// Use verify timeout from config
	timeout := agencyJSON.Scripts.Verify.Timeout

	// Emit verify_started event
	appendMergeEvent(eventsPath, repoID, meta.RunID, "verify_started", map[string]any{
		"timeout_ms": timeout.Milliseconds(),
	})

	// Build environment
	env := buildVerifyEnvForMerge(meta, worktreePath, runDir)

	// Run verify
	runCfg := verify.RunConfig{
		RepoID:         repoID,
		RunID:          meta.RunID,
		WorkDir:        worktreePath,
		Script:         agencyJSON.Scripts.Verify.Path,
		Env:            env,
		Timeout:        timeout,
		LogPath:        logPath,
		VerifyJSONPath: verifyJSONPath,
		RecordPath:     recordPath,
	}

	record, runErr := verify.Run(ctx, runCfg)

	// Emit verify_finished event
	var exitCode *int
	if record.ExitCode != nil {
		exitCode = record.ExitCode
	}
	appendMergeEvent(eventsPath, repoID, meta.RunID, "verify_finished", map[string]any{
		"ok":          record.OK,
		"exit_code":   exitCode,
		"duration_ms": record.DurationMS,
	})

	if runErr != nil {
		return &record, errors.Wrap(errors.EInternal, "verify runner failed", runErr)
	}

	return &record, nil
}

// buildVerifyEnvForMerge builds environment for verify script.
func buildVerifyEnvForMerge(meta *store.RunMeta, worktreePath, runDir string) []string {
	env := os.Environ()

	agencyEnv := map[string]string{
		"AGENCY_RUN_ID":         meta.RunID,
		"AGENCY_NAME":           meta.Name,
		"AGENCY_REPO_ROOT":      worktreePath,
		"AGENCY_WORKSPACE_ROOT": worktreePath,
		"AGENCY_BRANCH":         meta.Branch,
		"AGENCY_PARENT_BRANCH":  meta.ParentBranch,
		"AGENCY_ORIGIN_NAME":    "origin",
		"AGENCY_ORIGIN_URL":     "",
		"AGENCY_RUNNER":         meta.Runner,
		"AGENCY_PR_URL":         meta.PRURL,
		"AGENCY_PR_NUMBER":      "",
		"AGENCY_DOTAGENCY_DIR":  filepath.Join(worktreePath, ".agency"),
		"AGENCY_OUTPUT_DIR":     filepath.Join(worktreePath, ".agency", "out"),
		"AGENCY_LOG_DIR":        filepath.Join(runDir, "logs"),
		"AGENCY_NONINTERACTIVE": "1",
		"CI":                    "1",
	}

	if meta.PRNumber != 0 {
		agencyEnv["AGENCY_PR_NUMBER"] = fmt.Sprintf("%d", meta.PRNumber)
	}

	for k, v := range agencyEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}

// appendMergeEvent appends an event to events.jsonl.
func appendMergeEvent(eventsPath, repoID, runID, eventName string, data map[string]any) {
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
