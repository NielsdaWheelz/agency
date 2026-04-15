// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
)

// WorktreeCreateOpts holds options for the worktree create command.
type WorktreeCreateOpts struct {
	RepoRef      string
	Name         string
	ParentBranch string
	Open         bool
	Editor       string
}

// WorktreeCreate creates a new integration worktree.
func WorktreeCreate(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts WorktreeCreateOpts, stdout, stderr io.Writer) error {
	repoRef := strings.TrimSpace(opts.RepoRef)
	if repoRef == "" {
		return errors.New(errors.EUsage, "--repo is required")
	}
	if strings.TrimSpace(opts.Name) == "" {
		return errors.New(errors.EUsage, "--name is required")
	}
	parentBranch := strings.TrimSpace(opts.ParentBranch)
	if parentBranch == "" {
		return errors.New(errors.EUsage, "--parent is required")
	}

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repo, err := ns.client.GetRepo(ctx, repoRef)
	if err != nil {
		return err
	}
	if repo.Data.PreferredRoot == "" || !repo.Data.PreferredRootAccessible {
		return errors.NewWithDetails(
			errors.ERepoRootInaccessible,
			"repo preferred_root is not accessible",
			map[string]string{"repo": repoRef, "hint": "run `agency repo add /path/to/repo` from an accessible checkout"},
		)
	}

	// Check parent tree is clean (daemon doesn't check this)
	clean, err := git.IsClean(ctx, cr, repo.Data.PreferredRoot)
	if err != nil {
		return err
	}
	if !clean {
		return errors.New(errors.EParentDirty, "working tree has uncommitted changes; commit or stash before creating a worktree")
	}

	// Generate idempotency key
	idempotencyKey := uuid.New().String()

	// Call daemon to create worktree
	result, err := ns.client.WorktreeCreate(ctx, daemonclient.WorktreeCreateOpts{
		RepoRoot:       repo.Data.PreferredRoot,
		Name:           opts.Name,
		ParentBranch:   parentBranch,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return err
	}

	if !result.OK {
		return errors.NewWithDetails(
			errors.Code(result.ErrorCode),
			result.Message,
			map[string]string{"hint": result.Hint},
		)
	}

	// Output result
	_, _ = fmt.Fprintf(stdout, "Created integration worktree '%s'\n", opts.Name)
	_, _ = fmt.Fprintf(stdout, "  worktree_id: %s\n", result.WorktreeID)
	_, _ = fmt.Fprintf(stdout, "  branch:      %s\n", result.Branch)
	_, _ = fmt.Fprintf(stdout, "  path:        %s\n", result.TreePath)

	// Open in editor if requested
	if opts.Open {
		userCfg, _, err := config.LoadUserConfig(fsys, ns.dirs.ConfigDir)
		if err != nil {
			emitOpenOnCreateStatus(stdout, stderr, err)
			return nil
		}

		editorName := opts.Editor
		if editorName == "" {
			editorName = userCfg.Defaults.Editor
		}
		if editorName == "" {
			editorName = os.Getenv("EDITOR")
		}

		editorCmd, err := config.ResolveEditorCmd(cr, fsys, ns.dirs.ConfigDir, userCfg, editorName)
		if err != nil {
			emitOpenOnCreateStatus(stdout, stderr, err)
			return nil
		}

		runResult, runErr := runAttachedInDir(ctx, editorCmd, []string{result.TreePath}, result.TreePath)
		if runErr != nil {
			emitOpenOnCreateStatus(stdout, stderr, runErr)
			return nil
		}
		if runResult.ExitCode != 0 {
			emitOpenOnCreateStatus(stdout, stderr, fmt.Errorf("editor exited with code %d", runResult.ExitCode))
			return nil
		}

		emitOpenOnCreateStatus(stdout, stderr, nil)
	}

	return nil
}
