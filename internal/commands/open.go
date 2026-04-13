// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/paths"
)

// OpenOpts holds options for the open command.
type OpenOpts struct {
	// RunID is the run identifier to open.
	RunID string

	// Editor overrides the default editor name.
	Editor string

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string

	// ConfigDirOverride, if set, is used instead of resolving from environment.
	ConfigDirOverride string
}

// Open opens the run worktree in the configured editor.
// Resolves run IDs globally and does not require repo cwd.
func Open(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, opts OpenOpts) error {
	if opts.RunID == "" {
		return errors.New(errors.EUsage, "run_id is required")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	dataDir := dirs.DataDir
	configDir := dirs.ConfigDir
	if opts.DataDirOverride != "" {
		dataDir = opts.DataDirOverride
	}
	if opts.ConfigDirOverride != "" {
		configDir = opts.ConfigDirOverride
	}

	userCfg, _, err := config.LoadUserConfig(fsys, configDir)
	if err != nil {
		return err
	}

	runRef, record, err := resolveRunGlobal(opts.RunID, dataDir)
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
	editorCmd, err := config.ResolveEditorCmd(cr, fsys, configDir, userCfg, editorName)
	if err != nil {
		return err
	}

	result, err := agencyexec.RunAttached(ctx, editorCmd, []string{worktreePath}, agencyexec.AttachedRunOpts{
		Dir:    worktreePath,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to run editor command", err)
	}
	if result.ExitCode != 0 {
		return errors.WithExitCode(
			errors.New(errors.EInternal, fmt.Sprintf("editor exited with code %d", result.ExitCode)),
			result.ExitCode,
		)
	}
	return nil
}
