// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runservice"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// AttachOpts holds options for the attach command.
type AttachOpts struct {
	// RunID is the run identifier to attach to.
	RunID string

	// RepoPath is the optional --repo flag to scope name resolution.
	RepoPath string

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// Attach attaches to an existing tmux session for a run.
// Works from any directory; resolves runs globally.
func Attach(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, opts AttachOpts, stdout, stderr io.Writer) error {
	// Create real tmux client
	tmuxClient := tmux.NewExecClient(cr)
	return AttachWithTmux(ctx, cr, fsys, tmuxClient, cwd, opts, stdout, stderr)
}

// AttachWithTmux attaches to an existing tmux session for a run using the provided tmux client.
// This variant is used for testing with a fake tmux client.
func AttachWithTmux(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, tmuxClient tmux.Client, cwd string, opts AttachOpts, stdout, stderr io.Writer) error {
	// Validate run_id provided
	if opts.RunID == "" {
		return errors.New(errors.EUsage, "run_id is required")
	}

	// Build resolution context using the new global resolver
	rctx, err := ResolveRunContext(ctx, cr, cwd, opts.RepoPath, opts.DataDirOverride)
	if err != nil {
		return err
	}

	// Resolve run globally (works from anywhere)
	resolved, err := ResolveRun(rctx, opts.RunID)
	if err != nil {
		return err
	}

	// Use the resolved run_id for session name
	runID := resolved.RunID

	// Compute session name from run_id (source of truth from tmux.SessionName)
	sessionName := tmux.SessionName(runID)

	// Check if tmux session actually exists using the TmuxClient
	exists, err := tmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		return errors.Wrap(errors.ETmuxNotInstalled, "failed to check tmux session", err)
	}
	if !exists {
		// Session doesn't exist (was killed, system restarted, etc.)
		// Return E_SESSION_NOT_FOUND with suggestion to use resume
		return errors.NewWithDetails(
			errors.ESessionNotFound,
			fmt.Sprintf("tmux session '%s' does not exist", sessionName),
			map[string]string{
				"run_id":     runID,
				"session":    sessionName,
				"suggestion": fmt.Sprintf("try: agency resume %s", runID),
			},
		)
	}

	// Attach to the tmux session
	return attachToTmuxSession(ctx, sessionName)
}

// attachToTmuxSession attaches to a tmux session interactively.
func attachToTmuxSession(ctx context.Context, sessionName string) error {
	result, err := agencyexec.RunAttached(ctx, "tmux", []string{"attach", "-t", sessionName}, agencyexec.AttachedRunOpts{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		return errors.Wrap(errors.ETmuxFailed, "tmux attach failed", err)
	}
	if result.ExitCode != 0 {
		return errors.New(errors.ETmuxFailed, fmt.Sprintf("tmux attach failed with exit code %d", result.ExitCode))
	}
	return nil
}

// TmuxSessionPrefix is the prefix for all agency tmux session names.
const TmuxSessionPrefix = runservice.TmuxSessionPrefix
