// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/events"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// KillOpts holds options for the kill command.
type KillOpts struct {
	// RunID is the run identifier to kill.
	RunID string

	// RepoPath is the optional --repo flag to scope name resolution.
	RepoPath string
}

// Kill kills the tmux session for a run. Workspace remains intact.
// Works from any directory; resolves runs globally.
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

	// Use the resolved run_id
	runID := resolved.RunID
	repoID := resolved.RepoID

	// Create store for later operations
	st := store.NewStore(fsys, rctx.DataDir, nil)

	// Compute session name from run_id (source of truth from tmux.SessionName)
	sessionName := tmux.SessionName(runID)

	// Check if tmux session actually exists
	exists, err := tmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		return errors.Wrap(errors.ETmuxNotInstalled, "failed to check tmux session", err)
	}
	if !exists {
		// Session doesn't exist - no-op, exit 0
		_, _ = fmt.Fprintf(stderr, "no session for %s\n", runID)
		return nil
	}

	// Kill the session
	killErr := tmuxClient.KillSession(ctx, sessionName)
	if killErr != nil {
		return errors.Wrap(errors.ETmuxFailed, "failed to kill tmux session", killErr)
	}

	// Append kill_session event
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventErr := events.AppendEvent(eventsPath, events.Event{
		SchemaVersion: "1.0",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         runID,
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
