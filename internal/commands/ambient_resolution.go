package commands

import (
	"context"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
)

type cwdTargetSelection struct {
	Repo                    daemon.RepoDTO
	RepoRoot                string
	Worktree                daemon.WorktreeDTO
	HasRepo                 bool
	HasWorktree             bool
	InsideAgencyManagedTree bool
}

func inspectCWDAmbientSelection(ctx context.Context, cr exec.CommandRunner, ns *daemonNavSetup, cwd string) (cwdTargetSelection, error) {
	cwdWorktree, hasWorktree, err := findPresentWorktreeContainingCWD(ctx, ns.client, cwd)
	if err != nil {
		return cwdTargetSelection{}, err
	}
	if hasWorktree {
		repo, err := resolveAccessibleRepo(ctx, ns.client, cwdWorktree.RepoID)
		if err != nil {
			return cwdTargetSelection{}, err
		}
		return cwdTargetSelection{
			Repo:        repo,
			RepoRoot:    cwdWorktree.TreePath,
			Worktree:    cwdWorktree,
			HasRepo:     true,
			HasWorktree: true,
		}, nil
	}

	if cwdInsideAgencyManagedTree(cwd, ns.dirs.DataDir) {
		return cwdTargetSelection{InsideAgencyManagedTree: true}, nil
	}

	currentRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		if errors.GetCode(err) == errors.ENoRepo {
			return cwdTargetSelection{}, nil
		}
		return cwdTargetSelection{}, err
	}

	repo, err := resolveAccessibleRegisteredRepo(ctx, ns.client, currentRoot.Path)
	if err != nil {
		return cwdTargetSelection{}, err
	}

	return cwdTargetSelection{
		Repo:     repo,
		RepoRoot: currentRoot.Path,
		HasRepo:  true,
	}, nil
}

func loadActiveContextFallback(ctx context.Context, client *daemonclient.Client, fsys fs.FS, configDir string, strict bool) (*validatedCurrentContext, bool, error) {
	current, err := loadValidatedCurrentContext(ctx, client, fsys, configDir)
	if err != nil {
		switch errors.GetCode(err) {
		case errors.ENoContext:
			return nil, false, nil
		case errors.EInvalidContext:
			if !strict {
				return nil, false, nil
			}
		}
		return nil, false, err
	}
	return current, true, nil
}
