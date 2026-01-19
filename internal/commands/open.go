// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	osexec "os/exec"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// OpenOpts holds options for the open command.
type OpenOpts struct {
	// RunID is the run identifier to open.
	RunID string

	// Editor overrides the default editor name.
	Editor string
}

// Open opens the run worktree in the configured editor.
// Resolves run IDs globally and does not require repo cwd.
func Open(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts OpenOpts, stdout, stderr io.Writer) error {
	if opts.RunID == "" {
		return errors.New(errors.EUsage, "run_id is required")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	userCfg, _, err := config.LoadUserConfig(fsys, dirs.ConfigDir)
	if err != nil {
		return err
	}

	runRef, record, err := resolveRunForOpen(dirs.DataDir, opts.RunID)
	if err != nil {
		return err
	}
	if runRef.Broken || record == nil || record.Meta == nil {
		return errors.NewWithDetails(
			errors.ERunBroken,
			"run exists but meta.json is unreadable or invalid",
			map[string]string{"run_id": runRef.RunID, "repo_id": runRef.RepoID},
		)
	}

	worktreePath := record.Meta.WorktreePath
	if worktreePath == "" {
		return errors.New(errors.EWorktreeMissing, "worktree path missing in meta.json")
	}
	if _, err := os.Stat(worktreePath); err != nil {
		if os.IsNotExist(err) {
			return errors.NewWithDetails(
				errors.EWorktreeMissing,
				"worktree path missing on disk",
				map[string]string{"run_id": runRef.RunID, "repo_id": runRef.RepoID, "worktree_path": worktreePath},
			)
		}
		return errors.Wrap(errors.EInternal, "failed to stat worktree path", err)
	}

	editorName := opts.Editor
	if editorName == "" {
		editorName = userCfg.Defaults.Editor
	}
	editorCmd, err := config.ResolveEditorCmd(cr, fsys, dirs.ConfigDir, userCfg, editorName)
	if err != nil {
		return err
	}

	cmd := osexec.Command(editorCmd, worktreePath)
	cmd.Dir = worktreePath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*osexec.ExitError); ok {
			return errors.WithExitCode(
				errors.New(errors.EInternal, fmt.Sprintf("editor exited with code %d", exitErr.ExitCode())),
				exitErr.ExitCode(),
			)
		}
		return errors.Wrap(errors.EInternal, "failed to run editor command", err)
	}

	_ = cwd
	_ = stdout
	_ = stderr
	return nil
}

func resolveRunForOpen(dataDir, runID string) (ids.RunRef, *store.RunRecord, error) {
	// Use the shared name-aware resolution helper
	return resolveRunGlobal(runID, dataDir)
}
