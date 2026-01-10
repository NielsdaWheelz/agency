// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/runservice"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// AttachOpts holds options for the attach command.
type AttachOpts struct {
	// RunID is the run identifier to attach to.
	RunID string
}

// Attach attaches to an existing tmux session for a run.
// Requires cwd to be inside the target repo.
func Attach(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, opts AttachOpts, stdout, stderr io.Writer) error {
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

	// Verify tmux_session_name is set
	if meta.TmuxSessionName == "" {
		// Run exists but no tmux session was ever started (setup failed or tmux failed)
		return errors.NewWithDetails(
			errors.ETmuxSessionMissing,
			"tmux session not found for this run",
			map[string]string{
				"run_id":        opts.RunID,
				"worktree_path": meta.WorktreePath,
				"runner_cmd":    meta.RunnerCmd,
				"hint":          fmt.Sprintf("cd %q && %s", meta.WorktreePath, meta.RunnerCmd),
			},
		)
	}

	// Check if tmux session actually exists
	hasSessionResult, err := cr.Run(ctx, "tmux", []string{"has-session", "-t", meta.TmuxSessionName}, agencyexec.RunOpts{})
	if err != nil {
		return errors.Wrap(errors.ETmuxNotInstalled, "failed to check tmux session", err)
	}
	if hasSessionResult.ExitCode != 0 {
		// Session doesn't exist (was killed, system restarted, etc.)
		return errors.NewWithDetails(
			errors.ETmuxSessionMissing,
			"tmux session '"+meta.TmuxSessionName+"' does not exist",
			map[string]string{
				"run_id":        opts.RunID,
				"session":       meta.TmuxSessionName,
				"worktree_path": meta.WorktreePath,
				"runner_cmd":    meta.RunnerCmd,
				"hint":          fmt.Sprintf("cd %q && %s", meta.WorktreePath, meta.RunnerCmd),
			},
		)
	}

	// Attach to the tmux session
	// We need to use exec.Command directly for interactive attach
	return attachToTmuxSession(meta.TmuxSessionName, stdout, stderr)
}

// attachToTmuxSession attaches to a tmux session interactively.
// This replaces the current process with tmux attach.
func attachToTmuxSession(sessionName string, stdout, stderr io.Writer) error {
	// For interactive attach, we need to run tmux attach with proper terminal handling
	cmd := exec.Command("tmux", "attach", "-t", sessionName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Non-zero exit from tmux is fine (user detached)
			if exitErr.ExitCode() == 0 {
				return nil
			}
		}
		return errors.Wrap(errors.ETmuxFailed, "tmux attach failed", err)
	}
	return nil
}

// TmuxSessionPrefix is the prefix for all agency tmux session names.
const TmuxSessionPrefix = runservice.TmuxSessionPrefix
