// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/paths"
)

// PathOpts holds options for the path command.
type PathOpts struct {
	// RunRef is the run reference (name, run_id, or unique prefix).
	RunRef string
}

// Path outputs the worktree path for a run.
// Resolves run references globally and does not require repo cwd.
// This is a read-only command with no side effects.
// Outputs a single line (the worktree path) to stdout on success.
func Path(ctx context.Context, opts PathOpts, stdout, stderr io.Writer) error {
	if opts.RunRef == "" {
		return errors.New(errors.EUsage, "run reference is required")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	_, record, err := resolveRunGlobal(opts.RunRef, dirs.DataDir)
	if err != nil {
		return err
	}

	if record.Broken || record.Meta == nil {
		return errors.NewWithDetails(
			errors.ERunBroken,
			"run exists but meta.json is unreadable or invalid",
			map[string]string{"run_ref": opts.RunRef},
		)
	}

	worktreePath := record.Meta.WorktreePath
	if worktreePath == "" {
		return errors.New(errors.EWorktreeMissing, "worktree path missing in meta.json")
	}

	// Verify worktree exists on disk
	if _, err := os.Stat(worktreePath); err != nil {
		if os.IsNotExist(err) {
			return errors.NewWithDetails(
				errors.EWorktreeMissing,
				"worktree path missing on disk (run may be archived)",
				map[string]string{"worktree_path": worktreePath},
			)
		}
		return errors.Wrap(errors.EInternal, "failed to stat worktree path", err)
	}

	// Output single line: just the path, no decoration
	_, _ = fmt.Fprintln(stdout, worktreePath)

	_ = ctx
	_ = stderr
	return nil
}
