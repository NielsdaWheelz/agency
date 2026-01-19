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
	"github.com/NielsdaWheelz/agency/internal/events"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// StopOpts holds options for the stop command.
type StopOpts struct {
	// RunID is the run identifier to stop.
	RunID string
}

// Stop sends C-c to the runner in the tmux session (best-effort interrupt).
// Requires cwd to be inside the target repo.
func Stop(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, opts StopOpts, stdout, stderr io.Writer) error {
	// Create real tmux client
	tmuxClient := tmux.NewExecClient(cr)
	return StopWithTmux(ctx, cr, fsys, tmuxClient, cwd, opts, stdout, stderr)
}

// StopWithTmux stops a run using the provided tmux client.
// This variant is used for testing with a fake tmux client.
func StopWithTmux(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, tmuxClient tmux.Client, cwd string, opts StopOpts, stdout, stderr io.Writer) error {
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

	// Resolve run by name or ID within this repo
	resolved, _, err := resolveRunInRepo(opts.RunID, repoID, dataDir)
	if err != nil {
		return err
	}

	// Use the resolved run_id
	runID := resolved.RunID

	// Create store for later operations
	st := store.NewStore(fsys, dataDir, nil)

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

	// Send C-c to the session
	sendErr := tmuxClient.SendKeys(ctx, sessionName, []tmux.Key{tmux.KeyCtrlC})
	if sendErr != nil {
		// SendKeys failed - return error without mutating meta or events
		return errors.Wrap(errors.ETmuxFailed, "failed to send keys to tmux session", sendErr)
	}

	// Set needs_attention flag in meta.json
	// Note: if this fails, we continue to append the event and return the error afterward
	metaErr := st.UpdateMeta(repoID, runID, func(m *store.RunMeta) {
		if m.Flags == nil {
			m.Flags = &store.RunMetaFlags{}
		}
		m.Flags.NeedsAttention = true
	})

	// Append stop event
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventErr := events.AppendEvent(eventsPath, events.Event{
		SchemaVersion: "1.0",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         runID,
		Event:         "stop",
		Data: map[string]any{
			"session_name": sessionName,
			"keys":         []string{"C-c"},
		},
	})
	if eventErr != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to append stop event", eventErr)
	}

	// Return meta mutation error if it occurred
	if metaErr != nil {
		return metaErr
	}

	return nil
}
