package commands

import (
	"context"
	"fmt"
	"io"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// resolvePresentWorktree resolves the daemon nav, repo context, and integration
// worktree for cmdName, and rejects archived worktrees with EWorktreeNotFound.
func resolvePresentWorktree(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd, worktreeRef, repoRef, cmdName string) (*daemonNavSetup, *daemon.Result[daemon.WorktreeDTO], error) {
	ns, repoCtx, err := setupDaemonNavAndRepo(ctx, cr, fsys, cwd, "", ResolveRepoContextOpts{
		RepoRef: repoRef,
		CmdName: cmdName,
	})
	if err != nil {
		return nil, nil, err
	}

	worktree, err := ns.client.GetWorktree(ctx, worktreeRef, repoCtx.RepoID)
	if err != nil {
		return nil, nil, translateNavigationError(err, "worktree")
	}
	if worktree.Data.State != "present" {
		return nil, nil, errors.NewWithDetails(
			errors.EWorktreeNotFound,
			"integration worktree is archived",
			map[string]string{"hint": cmdName + " requires a present integration worktree"},
		)
	}
	return ns, worktree, nil
}

// WorktreePathOpts holds options for the worktree path command.
type WorktreePathOpts struct {
	WorktreeRef string
	RepoRef     string
}

// WorktreePath outputs the path to an integration worktree.
func WorktreePath(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreePathOpts, stdout, stderr io.Writer) error {
	_, worktree, err := resolvePresentWorktree(ctx, cr, fsys, cwd, opts.WorktreeRef, opts.RepoRef, "worktree path")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stdout, worktree.Data.TreePath)
	return nil
}

// WorktreeOpenOpts holds options for the worktree open command.
type WorktreeOpenOpts struct {
	WorktreeRef string
	RepoRef     string
	Editor      string
}

// WorktreeOpen opens an integration worktree in the configured editor.
func WorktreeOpen(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeOpenOpts, stdout, stderr io.Writer) error {
	ns, worktree, err := resolvePresentWorktree(ctx, cr, fsys, cwd, opts.WorktreeRef, opts.RepoRef, "worktree open")
	if err != nil {
		return err
	}
	return runEditorAt(ctx, cr, fsys, ns.dirs.ConfigDir, opts.Editor, worktree.Data.TreePath)
}

// WorktreeShellOpts holds options for the worktree shell command.
type WorktreeShellOpts struct {
	WorktreeRef string
	RepoRef     string
}

// WorktreeShell opens a shell in an integration worktree.
func WorktreeShell(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeShellOpts, stdout, stderr io.Writer) error {
	_, worktree, err := resolvePresentWorktree(ctx, cr, fsys, cwd, opts.WorktreeRef, opts.RepoRef, "worktree shell")
	if err != nil {
		return err
	}
	return runShellAt(ctx, worktree.Data.TreePath)
}
