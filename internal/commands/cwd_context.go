package commands

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
)

func findPresentWorktreeContainingCWD(ctx context.Context, client *daemonclient.Client, cwd string) (daemon.WorktreeDTO, bool, error) {
	if strings.TrimSpace(cwd) == "" {
		return daemon.WorktreeDTO{}, false, nil
	}

	cwdPath, err := filepath.Abs(cwd)
	if err != nil {
		return daemon.WorktreeDTO{}, false, err
	}
	cwdPath, err = filepath.EvalSymlinks(cwdPath)
	if err != nil {
		return daemon.WorktreeDTO{}, false, err
	}

	result, err := client.ListWorktrees(ctx, daemonclient.ListWorktreesOpts{State: "present"})
	if err != nil {
		return daemon.WorktreeDTO{}, false, err
	}

	for _, worktree := range result.Data.Worktrees {
		if strings.TrimSpace(worktree.TreePath) == "" {
			continue
		}
		treePath, err := filepath.EvalSymlinks(worktree.TreePath)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(treePath, cwdPath)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)) {
			return worktree, true, nil
		}
	}

	return daemon.WorktreeDTO{}, false, nil
}

func cwdInsideAgencyManagedTree(cwd, dataDir string) bool {
	if strings.TrimSpace(cwd) == "" || strings.TrimSpace(dataDir) == "" {
		return false
	}

	cwdPath, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}
	cwdPath, err = filepath.EvalSymlinks(cwdPath)
	if err != nil {
		return false
	}

	dataPath, err := filepath.Abs(dataDir)
	if err != nil {
		return false
	}
	dataPath, err = filepath.EvalSymlinks(dataPath)
	if err != nil {
		return false
	}

	reposDir := filepath.Join(dataPath, "repos")
	rel, err := filepath.Rel(reposDir, cwdPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}

	parts := strings.Split(rel, string(filepath.Separator))
	return len(parts) >= 4 &&
		(parts[1] == "integration_worktrees" || parts[1] == "sandboxes") &&
		parts[3] == "tree"
}
