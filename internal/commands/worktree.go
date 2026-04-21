// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
)

// WorktreeCreateOpts holds options for the worktree create command.
type WorktreeCreateOpts struct {
	RepoRef    string
	Name       string
	BaseBranch string
	Open       bool
	Editor     string
}

// WorktreeCreate creates a new integration worktree.
func WorktreeCreate(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeCreateOpts, stdout, stderr io.Writer) error {
	repoRef := strings.TrimSpace(opts.RepoRef)
	if strings.TrimSpace(opts.Name) == "" {
		return errors.New(errors.EUsage, "pass a worktree name: 'agency worktree create <worktree-name>'")
	}
	baseBranch := strings.TrimSpace(opts.BaseBranch)
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
	baseRoot := ""
	if repoRef != "" {
		repo, err := ns.client.GetRepo(ctx, repoRef)
		if err != nil {
			return err
		}
		if repo.Data.PreferredRoot == "" || !repo.Data.PreferredRootAccessible {
			return errors.NewWithDetails(
				errors.ERepoRootInaccessible,
				"repo preferred_root is not accessible",
				map[string]string{
					"repo": repoRef,
					"hint": "re-register this repo from an accessible checkout, then re-run the command",
				},
			)
		}
		repoRoot = repo.Data.PreferredRoot
		baseRoot = repoRoot
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
					map[string]string{
						"repo": worktree.RepoID,
						"hint": "re-register this repo from an accessible checkout, then re-run the command",
					},
				)
			}
			currentRoot, err := git.GetRepoRoot(ctx, cr, cwd)
			if err != nil {
				return err
			}
			repoRoot = repo.Data.PreferredRoot
			baseRoot = currentRoot.Path
		} else {
			if cwdInsideAgencyManagedTree(cwd, ns.dirs.DataDir) {
				return errors.NewWithDetails(
					errors.EUnsafeRepoRoot,
					"current directory is inside an agency-managed tree but not a present integration worktree",
					map[string]string{
						"hint": "re-run from the original repo checkout, or pass --repo <repo_ref> explicitly",
					},
				)
			}
			currentRoot, err := git.GetRepoRoot(ctx, cr, cwd)
			if err != nil {
				return errors.NewWithDetails(
					errors.ENoRepoContext,
					"cannot resolve worktree create without a repo context",
					map[string]string{
						"hint": "run from a git checkout, or pass --repo <repo_ref>",
					},
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
					map[string]string{
						"repo": reg.Data.RepoID,
						"hint": "re-register this repo from an accessible checkout, then re-run the command",
					},
				)
			}
			repoRoot = reg.Data.PreferredRoot
			baseRoot = currentRoot.Path
		}
	}

	if baseBranch == "" {
		result, err := cr.Run(ctx, "git", []string{"branch", "--show-current"}, exec.RunOpts{Dir: baseRoot})
		if err != nil {
			return errors.Wrap(errors.EBaseBranchNotFound, "failed to determine the current branch; pass --base explicitly", err)
		}
		baseBranch = strings.TrimSpace(result.Stdout)
		if result.ExitCode != 0 || baseBranch == "" {
			return errors.NewWithDetails(
				errors.EBaseBranchNotFound,
				"failed to determine the current branch; pass --base explicitly",
				map[string]string{"repo_root": baseRoot},
			)
		}
	}

	hasCommits, err := git.HasCommits(ctx, cr, baseRoot)
	if err != nil {
		return err
	}
	if !hasCommits {
		return errors.New(errors.EEmptyRepo, "repository has no commits; create an initial commit first")
	}

	clean, err := git.IsClean(ctx, cr, baseRoot)
	if err != nil {
		return err
	}
	if !clean {
		return errors.New(errors.EBaseDirty, "the checkout used to resolve --base is dirty; commit or stash changes first")
	}

	branchExists, err := git.BranchExists(ctx, cr, baseRoot, baseBranch)
	if err != nil {
		return err
	}
	if !branchExists {
		return errors.NewWithDetails(
			errors.EBaseBranchNotFound,
			"local base branch '"+baseBranch+"' was not found",
			map[string]string{"branch": baseBranch},
		)
	}

	// Generate idempotency key
	idempotencyKey := uuid.New().String()

	// Call daemon to create worktree
	result, err := ns.client.WorktreeCreate(ctx, daemonclient.WorktreeCreateOpts{
		RepoRoot:       repoRoot,
		Name:           opts.Name,
		BaseBranch:     baseBranch,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return err
	}

	// Output result
	_, _ = fmt.Fprintf(stdout, "Created integration worktree '%s'\n", opts.Name)
	_, _ = fmt.Fprintf(stdout, "  worktree_id: %s\n", result.WorktreeID)
	_, _ = fmt.Fprintf(stdout, "  branch:      %s\n", result.Branch)
	_, _ = fmt.Fprintf(stdout, "  path:        %s\n", result.TreePath)

	// Open in editor if requested
	if opts.Open {
		editorCmd, err := resolveEditorCmdWithOptionalOverride(cr, fsys, ns.dirs.ConfigDir, opts.Editor)
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
