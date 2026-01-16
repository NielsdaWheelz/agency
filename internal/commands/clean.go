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
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/NielsdaWheelz/agency/internal/tty"
)

// CleanOpts holds options for the clean command.
type CleanOpts struct {
	// RunID is the run identifier to clean (required).
	RunID string
}

// Clean archives a run without merging.
// Requires cwd to be inside the target repo.
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
	if !tty.IsInteractive() {
		return errors.New(errors.ENotInteractive, "clean requires an interactive terminal; stdin and stderr must be TTYs")
	}

	// Find repo root - clean requires being inside a repo
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.NewWithDetails(errors.ENoRepo, "not inside a git repository; cd into repo root and retry",
			map[string]string{"hint": "cd into repo root and retry"})
	}

	// Get origin info for repo identity
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)

	// Get home directory for path resolution
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}

	// Resolve data directory
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	dataDir := dirs.DataDir

	// Compute repo identity
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)
	repoID := repoIdentity.RepoID

	// Create store and look up the run
	st := store.NewStore(fsys, dataDir, time.Now)
	meta, err := st.ReadMeta(repoID, opts.RunID)
	if err != nil {
		return err
	}

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

	// Get events path
	eventsPath := st.EventsPath(repoID, opts.RunID)

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
		RepoRoot:      repoRoot.Path,
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

	// Append clean_finished event
	_ = events.AppendEvent(eventsPath, events.Event{
		SchemaVersion: "1.0",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         opts.RunID,
		Event:         "clean_finished",
		Data:          events.CleanFinishedData(result.Success()),
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

	return nil
}

// isLockError checks if err is a lock.ErrLocked and assigns it to target.
func isLockError(err error, target **lock.ErrLocked) bool {
	if e, ok := err.(*lock.ErrLocked); ok {
		*target = e
		return true
	}
	return false
}
