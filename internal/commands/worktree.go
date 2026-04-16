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
func WorktreeCreate(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeCreateOpts, stdout, stderr io.Writer) error {
	repoRef := strings.TrimSpace(opts.RepoRef)
	if strings.TrimSpace(opts.Name) == "" {
		return errors.New(errors.EUsage, "--name is required")
	}
	parentBranch := strings.TrimSpace(opts.ParentBranch)
	if cr == nil {
		cr = exec.NewRealRunner()
	}
	if fsys == nil {
		fsys = fs.NewRealFS()
	}

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoRoot := ""
	parentRoot := ""
	if repoRef != "" {
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
		repoRoot = repo.Data.PreferredRoot
		parentRoot = repoRoot
	} else {
		worktree, ok, err := findPresentWorktreeContainingCWD(ctx, ns.client, cwd)
		if err != nil {
			return err
		}
		if ok {
			repo, err := ns.client.GetRepo(ctx, worktree.RepoID)
			if err != nil {
				return err
			}
			if repo.Data.PreferredRoot == "" || !repo.Data.PreferredRootAccessible {
				return errors.NewWithDetails(
					errors.ERepoRootInaccessible,
					"repo preferred_root is not accessible",
					map[string]string{"repo": worktree.RepoID, "hint": "run `agency repo add /path/to/repo` from an accessible checkout"},
				)
			}
			currentRoot, err := git.GetRepoRoot(ctx, cr, cwd)
			if err != nil {
				return err
			}
			repoRoot = repo.Data.PreferredRoot
			parentRoot = currentRoot.Path
		} else {
			if cwdInsideAgencyManagedTree(cwd, ns.dirs.DataDir) {
				return errors.NewWithDetails(
					errors.EUnsafeRepoRoot,
					"current directory is inside an agency-managed tree but not a present integration worktree",
					map[string]string{"hint": "re-run from the original repo or pass --repo and --parent explicitly"},
				)
			}
			currentRoot, err := git.GetRepoRoot(ctx, cr, cwd)
			if err != nil {
				return errors.NewWithDetails(
					errors.ENoRepoContext,
					"cannot resolve worktree create without a repo context",
					map[string]string{"hint": "run from a git checkout or pass --repo <repo_ref>"},
				)
			}
			reg, err := ns.client.RegisterRepo(ctx, currentRoot.Path)
			if err != nil {
				return err
			}
			if reg.Data.PreferredRoot == "" || !reg.Data.PreferredRootAccessible {
				return errors.NewWithDetails(
					errors.ERepoRootInaccessible,
					"repo preferred_root is not accessible",
					map[string]string{"repo": reg.Data.RepoID, "hint": "run `agency repo add /path/to/repo` from an accessible checkout"},
				)
			}
			repoRoot = reg.Data.PreferredRoot
			parentRoot = currentRoot.Path
		}
	}

	if parentBranch == "" {
		result, err := cr.Run(ctx, "git", []string{"branch", "--show-current"}, exec.RunOpts{Dir: parentRoot})
		if err != nil {
			return errors.Wrap(errors.EParentBranchNotFound, "failed to determine current branch; pass --parent", err)
		}
		parentBranch = strings.TrimSpace(result.Stdout)
		if result.ExitCode != 0 || parentBranch == "" {
			return errors.NewWithDetails(
				errors.EParentBranchNotFound,
				"failed to determine current branch; pass --parent",
				map[string]string{"repo_root": parentRoot},
			)
		}
	}

	// Check parent tree is clean (daemon doesn't check this)
	clean, err := git.IsClean(ctx, cr, parentRoot)
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
		RepoRoot:       repoRoot,
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
