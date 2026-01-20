// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/render"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// ResolveOpts holds options for the resolve command.
type ResolveOpts struct {
	// RunID is the run identifier (name, run_id, or prefix).
	RunID string
}

// Resolve executes the agency resolve command.
// Prints conflict resolution guidance to stdout (worktree present) or stderr (worktree missing).
//
// Per spec:
// - Read-only: does not require repo lock, no meta mutations, no events
// - Makes no git changes
// - If worktree present: prints action card to stdout, exits 0
// - If worktree missing: prints partial guidance to stderr, exits with E_WORKTREE_MISSING
func Resolve(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts ResolveOpts, stdout, stderr io.Writer) error {
	// Validate run_id provided
	if opts.RunID == "" {
		return errors.New(errors.EUsage, "run_id is required")
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

	// Resolve run by name or ID globally (read-only, no lock needed)
	runRef, record, err := resolveRunGlobal(opts.RunID, dataDir)
	if err != nil {
		return err
	}

	// Check if broken
	if runRef.Broken || record == nil || record.Meta == nil {
		return errors.NewWithDetails(
			errors.ERunBroken,
			"run exists but meta.json is unreadable or invalid",
			map[string]string{"run_id": runRef.RunID, "repo_id": runRef.RepoID},
		)
	}

	meta := record.Meta
	_ = st // silence unused warning; store used only for resolution

	// Build action card inputs using the ref the user invoked
	// Per spec: use the same ref the user invoked in printed commands
	ref := opts.RunID
	if meta.Name != "" && meta.Name == opts.RunID {
		ref = meta.Name
	} else if meta.RunID == opts.RunID {
		ref = meta.RunID
	}
	// For prefix matches, fall back to run_id which is always unambiguous
	if ref != meta.Name && ref != meta.RunID {
		ref = meta.RunID
	}

	inputs := render.ConflictCardInputs{
		Ref:          ref,
		PRURL:        meta.PRURL,
		PRNumber:     meta.PRNumber,
		Base:         meta.ParentBranch,
		Branch:       meta.Branch,
		WorktreePath: meta.WorktreePath,
	}

	// Check if worktree exists on disk
	worktreeExists := false
	if meta.WorktreePath != "" {
		if _, err := os.Stat(meta.WorktreePath); err == nil {
			worktreeExists = true
		}
	}

	if worktreeExists {
		// Worktree present: print full action card to stdout, exit 0
		render.WriteConflictCard(stdout, inputs)
		return nil
	}

	// Worktree missing: print partial guidance to stderr, exit with E_WORKTREE_MISSING
	// Per spec: error_code line first, then message, then partial card
	_, _ = io.WriteString(stderr, "error_code: E_WORKTREE_MISSING\n")
	_, _ = io.WriteString(stderr, "worktree archived or missing\n")
	_, _ = io.WriteString(stderr, "\n")
	render.WritePartialConflictCard(stderr, inputs)

	return errors.New(errors.EWorktreeMissing, "worktree archived or missing")
}
