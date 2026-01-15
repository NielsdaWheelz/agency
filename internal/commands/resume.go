// Package commands implements agency CLI commands.
package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// ResumeOpts holds options for the resume command.
type ResumeOpts struct {
	// RunID is the run identifier to resume.
	RunID string

	// Detached means do not attach; return after ensuring session exists.
	Detached bool

	// Restart means kill existing session (if any) and recreate.
	Restart bool

	// Yes skips confirmation prompt for --restart when session exists.
	Yes bool
}

// isInteractive is a package-level var for testing override.
var isInteractive = tty.IsInteractive

// Resume ensures a tmux session exists for the run and optionally attaches.
// Requires cwd to be inside the target repo.
func Resume(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, opts ResumeOpts, stdin io.Reader, stdout, stderr io.Writer) error {
	// Create real tmux client
	tmuxClient := tmux.NewExecClient(cr)
	return ResumeWithTmux(ctx, cr, fsys, tmuxClient, cwd, opts, stdin, stdout, stderr)
}

// ResumeWithTmux resumes a run using the provided tmux client.
// This variant is used for testing with a fake tmux client.
func ResumeWithTmux(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, tmuxClient tmux.Client, cwd string, opts ResumeOpts, stdin io.Reader, stdout, stderr io.Writer) error {
	// Validate run_id provided
	if opts.RunID == "" {
		return errors.New(errors.EUsage, "run_id is required")
	}

	// Find repo root
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return err
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
	st := store.NewStore(fsys, dataDir, nil)
	meta, err := st.ReadMeta(repoID, opts.RunID)
	if err != nil {
		// E_RUN_NOT_FOUND is already the right error code from ReadMeta
		return err
	}

	// Validate worktree path exists (before any tmux actions)
	sessionName := tmux.SessionName(opts.RunID)
	eventsPath := filepath.Join(st.RunDir(repoID, opts.RunID), "events.jsonl")

	worktreeInfo, statErr := fsys.Stat(meta.WorktreePath)
	worktreeExists := statErr == nil && worktreeInfo.IsDir()
	if !worktreeExists {
		// Determine reason: archived vs corrupted/missing
		reason := "missing"
		msg := "worktree missing; run is corrupted"
		if meta.Archive != nil && meta.Archive.ArchivedAt != "" {
			reason = "archived"
			msg = "run is archived; cannot resume"
		}

		// Append resume_failed event
		_ = events.AppendEvent(eventsPath, events.Event{
			SchemaVersion: "1.0",
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			RepoID:        repoID,
			RunID:         opts.RunID,
			Event:         "resume_failed",
			Data:          events.ResumeFailedData(sessionName, reason),
		})

		return errors.NewWithDetails(
			errors.EWorktreeMissing,
			msg,
			map[string]string{
				"run_id":        opts.RunID,
				"worktree_path": meta.WorktreePath,
				"reason":        reason,
			},
		)
	}

	// Check session existence (no lock yet)
	sessionExists, err := tmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		return errors.Wrap(errors.ETmuxNotInstalled, "failed to check tmux session", err)
	}

	// Load agency.json for runner resolution
	cfg, err := config.LoadAgencyConfig(fsys, repoRoot.Path)
	if err != nil {
		return err
	}

	// Resolve runner command using shared helper
	runnerCmd, err := config.ResolveRunnerCmd(&cfg, meta.Runner)
	if err != nil {
		return err
	}

	// Handle restart path
	if opts.Restart {
		return handleRestart(ctx, cr, fsys, tmuxClient, st, repoID, opts, meta, sessionName, sessionExists, runnerCmd, stdin, stdout, stderr, eventsPath, dataDir)
	}

	// Handle attach or create path
	if sessionExists {
		// Append resume_attach event
		_ = events.AppendEvent(eventsPath, events.Event{
			SchemaVersion: "1.0",
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			RepoID:        repoID,
			RunID:         opts.RunID,
			Event:         "resume_attach",
			Data:          events.ResumeData(sessionName, meta.Runner, opts.Detached, false),
		})

		if opts.Detached {
			fmt.Fprintf(stdout, "ok: session %s ready\n", sessionName)
			return nil
		}
		return attachToTmuxSession(sessionName, stdout, stderr)
	}

	// Session missing - need to create (requires lock)
	return handleCreateSession(ctx, cr, fsys, tmuxClient, st, repoID, opts, meta, sessionName, runnerCmd, stdout, stderr, eventsPath, dataDir)
}

// handleRestart handles the --restart path.
func handleRestart(
	ctx context.Context,
	cr agencyexec.CommandRunner,
	fsys fs.FS,
	tmuxClient tmux.Client,
	st *store.Store,
	repoID string,
	opts ResumeOpts,
	meta *store.RunMeta,
	sessionName string,
	sessionExists bool,
	runnerCmd string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	eventsPath, dataDir string,
) error {
	// If session exists and not --yes, need confirmation
	if sessionExists && !opts.Yes {
		interactive := isInteractive()
		if !interactive {
			return errors.New(errors.EConfirmationRequired,
				"refusing to restart without confirmation in non-interactive mode; pass --yes")
		}

		// Prompt for confirmation
		fmt.Fprintf(stderr, "restart session? in-tool history will be lost (git state unchanged) [y/N]: ")
		scanner := bufio.NewScanner(stdin)
		if !scanner.Scan() {
			// No input - treat as cancel
			fmt.Fprintln(stderr, "canceled")
			return nil
		}
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(stderr, "canceled")
			return nil
		}
	}

	// Acquire repo lock for restart
	rl := lock.NewRepoLock(dataDir)
	unlock, err := rl.Lock(repoID, "resume --restart")
	if err != nil {
		if _, ok := err.(*lock.ErrLocked); ok {
			return errors.New(errors.ERepoLocked, err.Error())
		}
		return errors.Wrap(errors.EInternal, "failed to acquire repo lock", err)
	}
	defer unlock()

	// Re-check session existence under lock
	sessionExists, err = tmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		return errors.Wrap(errors.ETmuxNotInstalled, "failed to check tmux session", err)
	}

	// Kill if exists
	if sessionExists {
		if err := tmuxClient.KillSession(ctx, sessionName); err != nil {
			return errors.Wrap(errors.ETmuxFailed, "failed to kill existing session", err)
		}
	}

	// Create new session
	argv := []string{runnerCmd}
	if err := tmuxClient.NewSession(ctx, sessionName, meta.WorktreePath, argv); err != nil {
		return errors.Wrap(errors.ETmuxFailed, "failed to create tmux session", err)
	}

	// Append resume_restart event
	_ = events.AppendEvent(eventsPath, events.Event{
		SchemaVersion: "1.0",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         opts.RunID,
		Event:         "resume_restart",
		Data:          events.ResumeData(sessionName, meta.Runner, opts.Detached, true),
	})

	if opts.Detached {
		fmt.Fprintf(stdout, "ok: session %s ready\n", sessionName)
		return nil
	}
	return attachToTmuxSession(sessionName, stdout, stderr)
}

// handleCreateSession handles the create-missing-session path.
func handleCreateSession(
	ctx context.Context,
	cr agencyexec.CommandRunner,
	fsys fs.FS,
	tmuxClient tmux.Client,
	st *store.Store,
	repoID string,
	opts ResumeOpts,
	meta *store.RunMeta,
	sessionName string,
	runnerCmd string,
	stdout, stderr io.Writer,
	eventsPath, dataDir string,
) error {
	// Acquire repo lock for create
	rl := lock.NewRepoLock(dataDir)
	unlock, err := rl.Lock(repoID, "resume")
	if err != nil {
		if _, ok := err.(*lock.ErrLocked); ok {
			return errors.New(errors.ERepoLocked, err.Error())
		}
		return errors.Wrap(errors.EInternal, "failed to acquire repo lock", err)
	}
	defer unlock()

	// Re-check session existence under lock (double-check pattern)
	sessionExists, err := tmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		return errors.Wrap(errors.ETmuxNotInstalled, "failed to check tmux session", err)
	}

	if sessionExists {
		// Race: session was created by another process
		// Treat as attach path
		_ = events.AppendEvent(eventsPath, events.Event{
			SchemaVersion: "1.0",
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			RepoID:        repoID,
			RunID:         opts.RunID,
			Event:         "resume_attach",
			Data:          events.ResumeData(sessionName, meta.Runner, opts.Detached, false),
		})

		if opts.Detached {
			fmt.Fprintf(stdout, "ok: session %s ready\n", sessionName)
			return nil
		}
		return attachToTmuxSession(sessionName, stdout, stderr)
	}

	// Create new session
	argv := []string{runnerCmd}
	if err := tmuxClient.NewSession(ctx, sessionName, meta.WorktreePath, argv); err != nil {
		return errors.Wrap(errors.ETmuxFailed, "failed to create tmux session", err)
	}

	// Append resume_create event
	_ = events.AppendEvent(eventsPath, events.Event{
		SchemaVersion: "1.0",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         opts.RunID,
		Event:         "resume_create",
		Data:          events.ResumeData(sessionName, meta.Runner, opts.Detached, false),
	})

	if opts.Detached {
		fmt.Fprintf(stdout, "ok: session %s ready\n", sessionName)
		return nil
	}
	return attachToTmuxSession(sessionName, stdout, stderr)
}
