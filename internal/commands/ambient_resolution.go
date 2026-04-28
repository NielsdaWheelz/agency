package commands

import (
	"context"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
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
