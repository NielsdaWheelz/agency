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
		fmt.Fprintf(stderr, "no session for %s\n", opts.RunID)
		return nil
	}

	// Send C-c to the session
	sendErr := tmuxClient.SendKeys(ctx, sessionName, []tmux.Key{tmux.KeyCtrlC})
	if sendErr != nil {
		// SendKeys failed - return error without mutating meta or events
		return errors.Wrap(errors.ETmuxFailed, "failed to send keys to tmux session", sendErr)
	}

	// Set needs_attention flag in meta.json
	err = st.UpdateMeta(repoID, opts.RunID, func(m *store.RunMeta) {
		if m.Flags == nil {
			m.Flags = &store.RunMetaFlags{}
		}
		m.Flags.NeedsAttention = true
	})
	if err != nil {
		// Meta mutation failed - continue to append event anyway
		// but return the error at the end
	}

	// Append stop event
	eventsPath := filepath.Join(st.RunDir(repoID, opts.RunID), "events.jsonl")
	eventErr := events.AppendEvent(eventsPath, events.Event{
		SchemaVersion: "1.0",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		RepoID:        repoID,
		RunID:         opts.RunID,
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
	if err != nil {
		return err
	}

	return nil
}
