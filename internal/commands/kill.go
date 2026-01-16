// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/events"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// KillOpts holds options for the kill command.
type KillOpts struct {
	// RunID is the run identifier to kill.
	RunID string
}

// Kill kills the tmux session for a run. Workspace remains intact.
// Requires cwd to be inside the target repo.
func Kill(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, opts KillOpts, stdout, stderr io.Writer) error {
	// Create real tmux client
	tmuxClient := tmux.NewExecClient(cr)
	return KillWithTmux(ctx, cr, fsys, tmuxClient, cwd, opts, stdout, stderr)
}

// KillWithTmux kills a run's tmux session using the provided tmux client.
// This variant is used for testing with a fake tmux client.
func KillWithTmux(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, tmuxClient tmux.Client, cwd string, opts KillOpts, stdout, stderr io.Writer) error {
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
	_, err = st.ReadMeta(repoID, opts.RunID)
	if err != nil {
		// E_RUN_NOT_FOUND is already the right error code from ReadMeta
		return err
	}

	// Compute session name from run_id (source of truth from tmux.SessionName)
	sessionName := tmux.SessionName(opts.RunID)

	// Check if tmux session actually exists
	exists, err := tmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		return errors.Wrap(errors.ETmuxNotInstalled, "failed to check tmux session", err)
	}
	if !exists {
		// Session doesn't exist - no-op, exit 0
		_, _ = fmt.Fprintf(stderr, "no session for %s\n", opts.RunID)
		return nil
	}

	// Kill the session
	killErr := tmuxClient.KillSession(ctx, sessionName)
	if killErr != nil {
		return errors.Wrap(errors.ETmuxFailed, "failed to kill tmux session", killErr)
	}

	// Append kill_session event
	eventsPath := filepath.Join(st.RunDir(repoID, opts.RunID), "events.jsonl")
	eventErr := events.AppendEvent(eventsPath, events.Event{
		SchemaVersion: "1.0",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         opts.RunID,
		Event:         "kill_session",
		Data: map[string]any{
			"session_name": sessionName,
		},
	})
	if eventErr != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to append kill_session event", eventErr)
	}

	return nil
}
