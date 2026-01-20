// Package commands implements agency CLI commands.
package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/archive"
	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/events"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// CleanOpts holds options for the clean command.
type CleanOpts struct {
	// RunID is the run identifier to clean (required).
	RunID string

	// RepoPath is the optional --repo flag to scope name resolution.
	RepoPath string

	// AllowDirty allows clean with a dirty worktree.
	AllowDirty bool

	// DeleteBranch deletes the local and remote branch after archiving.
	// Also closes any associated PR.
	DeleteBranch bool
}

// Clean archives a run without merging.
// Works from any directory; resolves runs globally.
// Requires an interactive TTY for confirmation.
func Clean(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, opts CleanOpts, stdin io.Reader, stdout, stderr io.Writer) error {
	// Create real tmux client
	tmuxClient := tmux.NewExecClient(cr)
	return CleanWithTmux(ctx, cr, fsys, tmuxClient, cwd, opts, stdin, stdout, stderr)
}

// CleanWithTmux is the test-friendly version of Clean that accepts a tmux client.
func CleanWithTmux(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, tmuxClient tmux.Client, cwd string, opts CleanOpts, stdin io.Reader, stdout, stderr io.Writer) error {
	// Validate run_id provided
	if opts.RunID == "" {
		return errors.New(errors.EUsage, "run_id is required")
	}

	// Check for interactive TTY (stdin and stderr must be TTYs)
	if !isInteractive() {
		return errors.New(errors.ENotInteractive, "clean requires an interactive terminal; stdin and stderr must be TTYs")
	}

	// Build resolution context using the new global resolver
	rctx, err := ResolveRunContext(ctx, cr, cwd, opts.RepoPath)
	if err != nil {
		return err
	}

	// Resolve run globally (works from anywhere)
	resolved, err := ResolveRun(rctx, opts.RunID)
	if err != nil {
		return err
	}

	// Check if run is broken
	if resolved.Broken || resolved.Record == nil || resolved.Record.Meta == nil {
		return errors.NewWithDetails(
			errors.ERunBroken,
			"run exists but meta.json is unreadable or invalid",
			map[string]string{"run_id": resolved.RunID, "repo_id": resolved.RepoID},
		)
	}

	// Get home directory for path resolution
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}

	// Resolve data directory
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	dataDir := dirs.DataDir

	// Use the resolved run_id
	runID := resolved.RunID
	repoID := resolved.RepoID
	opts.RunID = runID // Update opts so later code uses the resolved ID
	meta := resolved.Record.Meta

	// Derive repo root from meta worktree path (best effort)
	// Clean needs repo root for archive script but worktree is the key context
	repoRoot := ""
	if rctx.CWDRepoRoot != "" && rctx.CWDRepoID == repoID {
		repoRoot = rctx.CWDRepoRoot
	} else if rctx.ExplicitRepoRoot != "" && rctx.ExplicitRepoID == repoID {
		repoRoot = rctx.ExplicitRepoRoot
	}

	// Create store for later operations
	st := store.NewStore(fsys, dataDir, time.Now)

	// Check if already archived (idempotent)
	if meta.Archive != nil && meta.Archive.ArchivedAt != "" {
		_, _ = fmt.Fprintln(stdout, "already archived")
		return nil
	}

	// Check if worktree exists
	worktreePath := meta.WorktreePath
	if worktreePath == "" {
		return errors.NewWithDetails(errors.EWorktreeMissing, "meta.json has empty worktree_path",
			map[string]string{"run_id": opts.RunID, "repo_id": repoID})
	}

	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return errors.NewWithDetails(errors.EWorktreeMissing, "worktree path does not exist",
			map[string]string{"run_id": opts.RunID, "worktree_path": worktreePath})
	}

	// Acquire repo lock
	repoLock := lock.NewRepoLock(dataDir)
	unlock, err := repoLock.Lock(repoID, "clean")
	if err != nil {
		var lockErr *lock.ErrLocked
		if ok := isLockError(err, &lockErr); ok {
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

	// Get events path
	eventsPath := st.EventsPath(repoID, opts.RunID)

	// Dirty worktree gate
	isClean, status, err := getDirtyStatus(ctx, cr, worktreePath)
	if err != nil {
		_ = events.AppendEvent(eventsPath, events.Event{
			SchemaVersion: "1.0",
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			RepoID:        repoID,
			RunID:         opts.RunID,
			Event:         "clean_failed",
			Data: map[string]any{
				"error_code": string(errors.GetCode(err)),
				"step":       "dirty_check",
			},
		})
		return err
	}
	if !isClean {
		if !opts.AllowDirty {
			_ = events.AppendEvent(eventsPath, events.Event{
				SchemaVersion: "1.0",
				Timestamp:     time.Now().UTC().Format(time.RFC3339),
				RepoID:        repoID,
				RunID:         opts.RunID,
				Event:         "clean_failed",
				Data: map[string]any{
					"error_code": string(errors.EDirtyWorktree),
					"step":       "dirty_check",
				},
			})
			return dirtyErrorWithContext(status)
		}
		_ = events.AppendEvent(eventsPath, events.Event{
			SchemaVersion: "1.0",
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			RepoID:        repoID,
			RunID:         opts.RunID,
			Event:         "dirty_allowed",
			Data: map[string]any{
				"cmd":    "clean",
				"status": status,
			},
		})
		printDirtyWarning(stderr, status)
	}

	// Print lock acquisition message (per spec)
	_, _ = fmt.Fprintln(stderr, "lock: acquired repo lock (held during clean/archive)")

	// Prompt for confirmation
	_, _ = fmt.Fprint(stderr, "confirm: type 'clean' to proceed: ")
	reader := bufio.NewReader(stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return errors.Wrap(errors.EAborted, "failed to read confirmation", err)
	}

	if strings.TrimSpace(input) != "clean" {
		return errors.New(errors.EAborted, "confirmation failed; expected 'clean'")
	}

	// Append clean_started event
	now := time.Now().UTC()
	_ = events.AppendEvent(eventsPath, events.Event{
		SchemaVersion: "1.0",
		Timestamp:     now.Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         opts.RunID,
		Event:         "clean_started",
		Data:          events.CleanStartedData(opts.RunID),
	})

	// Append archive_started event
	_ = events.AppendEvent(eventsPath, events.Event{
		SchemaVersion: "1.0",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         opts.RunID,
		Event:         "archive_started",
		Data:          events.ArchiveStartedData(opts.RunID),
	})

	// Load agency.json to get archive script
	agencyJSON, err := config.LoadAgencyConfig(fsys, worktreePath)
	if err != nil {
		// If config can't be loaded, we still try to archive
		// but with an empty script path (script step will fail)
		agencyJSON = config.AgencyConfig{}
	}

	// Run archive pipeline
	archiveCfg := archive.Config{
		Meta:          meta,
		RepoRoot:      repoRoot,
		DataDir:       dataDir,
		ArchiveScript: agencyJSON.Scripts.Archive,
		Timeout:       archive.DefaultArchiveTimeout,
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
		_ = events.AppendEvent(eventsPath, events.Event{
			SchemaVersion: "1.0",
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			RepoID:        repoID,
			RunID:         opts.RunID,
			Event:         "archive_finished",
			Data:          events.ArchiveFinishedData(true),
		})
	} else {
		_ = events.AppendEvent(eventsPath, events.Event{
			SchemaVersion: "1.0",
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			RepoID:        repoID,
			RunID:         opts.RunID,
			Event:         "archive_failed",
			Data:          events.ArchiveFailedData(result.ScriptOK, result.TmuxOK, result.DeleteOK, result.ScriptReason, result.TmuxReason, result.DeleteReason),
		})
	}

	// Update meta on success
	if result.Success() {
		updateErr := st.UpdateMeta(repoID, opts.RunID, func(m *store.RunMeta) {
			if m.Flags == nil {
				m.Flags = &store.RunMetaFlags{}
			}
			m.Flags.Abandoned = true
			if m.Archive == nil {
				m.Archive = &store.RunMetaArchive{}
			}
			m.Archive.ArchivedAt = time.Now().UTC().Format(time.RFC3339)
		})
		if updateErr != nil {
			// Non-fatal; log and continue (diagnostic output)
			_, _ = fmt.Fprintf(stderr, "warning: failed to update meta.json: %v\n", updateErr)
		}
	}

	// Handle --delete-branch after successful archive
	var branchResult *branchDeletionResult
	if opts.DeleteBranch && result.Success() {
		branchResult = deleteBranchAndClosePR(ctx, cr, meta, repoRoot, eventsPath, repoID, stderr)
	}

	// Append clean_finished event
	cleanFinishedData := events.CleanFinishedData(result.Success())
	if branchResult != nil {
		cleanFinishedData["delete_branch"] = true
		cleanFinishedData["local_branch_deleted"] = branchResult.LocalDeleted
		cleanFinishedData["remote_branch_deleted"] = branchResult.RemoteDeleted
		cleanFinishedData["pr_closed"] = branchResult.PRClosed
	}
	_ = events.AppendEvent(eventsPath, events.Event{
		SchemaVersion: "1.0",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         opts.RunID,
		Event:         "clean_finished",
		Data:          cleanFinishedData,
	})

	// Return error if archive failed
	if !result.Success() {
		return result.ToError()
	}

	// Print success message (informational output to user)
	_, _ = fmt.Fprintf(stdout, "cleaned: %s\n", opts.RunID)
	if result.LogPath != "" {
		_, _ = fmt.Fprintf(stdout, "log: %s\n", result.LogPath)
	}
	if branchResult != nil {
		if branchResult.LocalDeleted {
			_, _ = fmt.Fprintf(stdout, "local_branch: deleted %s\n", meta.Branch)
		}
		if branchResult.RemoteDeleted {
			_, _ = fmt.Fprintf(stdout, "remote_branch: deleted origin/%s\n", meta.Branch)
		}
		if branchResult.PRClosed {
			_, _ = fmt.Fprintf(stdout, "pr: closed #%d\n", meta.PRNumber)
		}
	}

	return nil
}

// branchDeletionResult holds the results of branch deletion operations.
type branchDeletionResult struct {
	LocalDeleted  bool
	RemoteDeleted bool
	PRClosed      bool
}

// deleteBranchAndClosePR deletes the local and remote branch and closes the PR.
// All operations are best-effort; failures are logged but don't fail the clean.
func deleteBranchAndClosePR(
	ctx context.Context,
	cr agencyexec.CommandRunner,
	meta *store.RunMeta,
	repoRoot string,
	eventsPath string,
	repoID string,
	stderr io.Writer,
) *branchDeletionResult {
	result := &branchDeletionResult{}
	branch := meta.Branch

	// 1. Delete local branch
	// We need to run this from the main repo root, not the worktree (which is deleted)
	if repoRoot != "" && branch != "" {
		localResult, err := cr.Run(ctx, "git", []string{
			"-C", repoRoot,
			"branch", "-D", branch,
		}, agencyexec.RunOpts{})

		if err == nil && localResult.ExitCode == 0 {
			result.LocalDeleted = true
			_ = events.AppendEvent(eventsPath, events.Event{
				SchemaVersion: "1.0",
				Timestamp:     time.Now().UTC().Format(time.RFC3339),
				RepoID:        repoID,
				RunID:         meta.RunID,
				Event:         "branch_deleted",
				Data: map[string]any{
					"branch": branch,
					"type":   "local",
				},
			})
		} else {
			// Log warning but continue
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			} else {
				errMsg = strings.TrimSpace(localResult.Stderr)
			}
			_, _ = fmt.Fprintf(stderr, "warning: failed to delete local branch: %s\n", errMsg)
		}
	}

	// 2. Try to get origin URL and delete remote branch
	if repoRoot != "" && branch != "" {
		originResult, err := cr.Run(ctx, "git", []string{
			"-C", repoRoot,
			"config", "--get", "remote.origin.url",
		}, agencyexec.RunOpts{})

		if err == nil && originResult.ExitCode == 0 {
			originURL := strings.TrimSpace(originResult.Stdout)
			originHost := parseOriginHost(originURL)

			// Only attempt remote operations for github.com
			if originHost == "github.com" {
				// Delete remote branch
				remoteResult, err := cr.Run(ctx, "git", []string{
					"-C", repoRoot,
					"push", "origin", "--delete", branch,
				}, agencyexec.RunOpts{
					Env: nonInteractiveEnv(),
				})

				if err == nil && remoteResult.ExitCode == 0 {
					result.RemoteDeleted = true
					_ = events.AppendEvent(eventsPath, events.Event{
						SchemaVersion: "1.0",
						Timestamp:     time.Now().UTC().Format(time.RFC3339),
						RepoID:        repoID,
						RunID:         meta.RunID,
						Event:         "branch_deleted",
						Data: map[string]any{
							"branch": branch,
							"type":   "remote",
						},
					})
				} else {
					// Log warning - remote branch may not exist
					errMsg := ""
					if err != nil {
						errMsg = err.Error()
					} else {
						errMsg = strings.TrimSpace(remoteResult.Stderr)
					}
					// Only warn if it's not a "branch doesn't exist" error
					if !strings.Contains(errMsg, "remote ref does not exist") {
						_, _ = fmt.Fprintf(stderr, "warning: failed to delete remote branch: %s\n", errMsg)
					}
				}

				// 3. Close PR if it exists
				if meta.PRNumber != 0 {
					owner, repo, ok := identity.ParseGitHubOwnerRepo(originURL)
					if ok {
						ghRepo := fmt.Sprintf("%s/%s", owner, repo)
						prResult, err := cr.Run(ctx, "gh", []string{
							"pr", "close",
							fmt.Sprintf("%d", meta.PRNumber),
							"-R", ghRepo,
							"--comment", "Closed via `agency clean --delete-branch`",
						}, agencyexec.RunOpts{
							Dir: repoRoot,
							Env: nonInteractiveEnv(),
						})

						if err == nil && prResult.ExitCode == 0 {
							result.PRClosed = true
							_ = events.AppendEvent(eventsPath, events.Event{
								SchemaVersion: "1.0",
								Timestamp:     time.Now().UTC().Format(time.RFC3339),
								RepoID:        repoID,
								RunID:         meta.RunID,
								Event:         "pr_closed",
								Data: map[string]any{
									"pr_number": meta.PRNumber,
									"pr_url":    meta.PRURL,
								},
							})
						} else {
							// Log warning - PR may already be closed or merged
							errMsg := ""
							if err != nil {
								errMsg = err.Error()
							} else {
								errMsg = strings.TrimSpace(prResult.Stderr)
							}
							// Only warn if not already closed/merged
							if !strings.Contains(errMsg, "already closed") &&
								!strings.Contains(errMsg, "already merged") &&
								!strings.Contains(errMsg, "Pull request #") {
								_, _ = fmt.Fprintf(stderr, "warning: failed to close PR #%d: %s\n", meta.PRNumber, errMsg)
							}
						}
					}
				}
			}
		}
	}

	return result
}

// isLockError checks if err is a lock.ErrLocked and assigns it to target.
func isLockError(err error, target **lock.ErrLocked) bool {
	if e, ok := err.(*lock.ErrLocked); ok {
		*target = e
		return true
	}
	return false
}
