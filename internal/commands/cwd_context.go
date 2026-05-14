package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	invocationpkg "github.com/NielsdaWheelz/agency/internal/invocation"
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

	cursor := ""
	for {
		result, err := client.ListWorktrees(ctx, daemonclient.ListWorktreesOpts{State: "present", Limit: 500, Cursor: cursor})
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
			if pathIsAtOrUnder(treePath, cwdPath) {
				return worktree, true, nil
			}
		}
		if result.Data.NextCursor == "" {
			break
		}
		cursor = result.Data.NextCursor
	}

	return daemon.WorktreeDTO{}, false, nil
}

func agencyManagedTreeRepoID(ctx context.Context, client *daemonclient.Client, path string) (string, bool, error) {
	if strings.TrimSpace(path) == "" {
		return "", false, nil
	}

	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	cleanPath, err = filepath.EvalSymlinks(cleanPath)
	if err != nil {
		return "", false, err
	}

	cursor := ""
	for {
		result, err := client.ListWorktrees(ctx, daemonclient.ListWorktreesOpts{State: "all", Limit: 500, Cursor: cursor})
		if err != nil {
			return "", false, err
		}
		for _, worktree := range result.Data.Worktrees {
			if strings.TrimSpace(worktree.TreePath) == "" {
				continue
			}
			treePath, err := filepath.EvalSymlinks(worktree.TreePath)
			if err != nil {
				continue
			}
			if pathIsAtOrUnder(treePath, cleanPath) {
				return worktree.RepoID, true, nil
			}
		}
		if result.Data.NextCursor == "" {
			break
		}
		cursor = result.Data.NextCursor
	}

	cursor = ""
	for {
		result, err := client.ListInvocations(ctx, daemonclient.ListInvocationsOpts{State: "all", Mode: "all", Limit: 500, Cursor: cursor})
		if err != nil {
			return "", false, err
		}
		for _, invocation := range result.Data.Invocations {
			if strings.TrimSpace(invocation.SandboxPath) == "" {
				continue
			}
			sandboxPath, err := filepath.EvalSymlinks(invocation.SandboxPath)
			if err != nil {
				continue
			}
			if pathIsAtOrUnder(sandboxPath, cleanPath) {
				return invocation.RepoID, true, nil
			}
		}
		if result.Data.NextCursor == "" {
			break
		}
		cursor = result.Data.NextCursor
	}

	return "", false, nil
}

func cwdInsideAgencyManagedTree(cwd string) bool {
	if strings.TrimSpace(cwd) == "" {
		return false
	}

	cleanPath, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}
	if resolvedPath, err := filepath.EvalSymlinks(cleanPath); err == nil {
		cleanPath = resolvedPath
	}

	for {
		if _, err := os.Stat(filepath.Join(cleanPath, ".agency", integrationworktree.IntegrationMarkerFileName)); err == nil {
			return true
		}
		if _, err := os.Stat(filepath.Join(cleanPath, ".agency", invocationpkg.SandboxMarkerFileName)); err == nil {
			return true
		}
		parent := filepath.Dir(cleanPath)
		if parent == cleanPath {
			return false
		}
		cleanPath = parent
	}
}

func pathIsAtOrUnder(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)))
}
