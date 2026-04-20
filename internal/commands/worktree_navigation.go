package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func resolveEditorCmdWithOptionalOverride(cr exec.CommandRunner, fsys fs.FS, configDir string, editorOverride string) (string, error) {
	editorName := strings.TrimSpace(editorOverride)
	if editorName != "" {
		return config.ResolveEditorCmd(cr, fsys, configDir, config.UserConfig{}, editorName)
	}

	userCfg, err := config.LoadUserConfig(fsys, configDir)
	if err != nil {
		return "", err
	}

	return config.ResolveEditorCmd(cr, fsys, configDir, userCfg, userCfg.Defaults.Editor)
}

// WorktreePathOpts holds options for the worktree path command.
type WorktreePathOpts struct {
	WorktreeRef string
	RepoRef     string
}

// WorktreePath outputs the path to an integration worktree.
func WorktreePath(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreePathOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "worktree path",
	})
	if err != nil {
		return err
	}

	worktree, err := ns.client.GetWorktree(ctx, opts.WorktreeRef, repoCtx.RepoID)
	if err != nil {
		return translateNavigationError(err, "worktree")
	}
	if worktree.Data.State != "present" {
		return errors.NewWithDetails(
			errors.EWorktreeNotFound,
			"integration worktree is archived",
			map[string]string{"hint": "worktree path requires a present integration worktree"},
		)
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
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "worktree open",
	})
	if err != nil {
		return err
	}

	worktree, err := ns.client.GetWorktree(ctx, opts.WorktreeRef, repoCtx.RepoID)
	if err != nil {
		return translateNavigationError(err, "worktree")
	}
	if worktree.Data.State != "present" {
		return errors.NewWithDetails(
			errors.EWorktreeNotFound,
			"integration worktree is archived",
			map[string]string{"hint": "worktree open requires a present integration worktree"},
		)
	}
	treePath := worktree.Data.TreePath

	editorCmd, err := resolveEditorCmdWithOptionalOverride(cr, fsys, ns.dirs.ConfigDir, opts.Editor)
	if err != nil {
		return err
	}

	runResult, runErr := runAttachedInDir(ctx, editorCmd, []string{treePath}, treePath)
	if runErr != nil {
		return errors.Wrap(errors.EInternal, "failed to run editor command", runErr)
	}
	if runResult.ExitCode != 0 {
		return errors.WithExitCode(
			errors.New(errors.EInternal, fmt.Sprintf("editor exited with code %d", runResult.ExitCode)),
			runResult.ExitCode,
		)
	}

	return nil
}

// WorktreeShellOpts holds options for the worktree shell command.
type WorktreeShellOpts struct {
	WorktreeRef string
	RepoRef     string
}

// WorktreeShell opens a shell in an integration worktree.
func WorktreeShell(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeShellOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "worktree shell",
	})
	if err != nil {
		return err
	}

	worktree, err := ns.client.GetWorktree(ctx, opts.WorktreeRef, repoCtx.RepoID)
	if err != nil {
		return translateNavigationError(err, "worktree")
	}
	if worktree.Data.State != "present" {
		return errors.NewWithDetails(
			errors.EWorktreeNotFound,
			"integration worktree is archived",
			map[string]string{"hint": "worktree shell requires a present integration worktree"},
		)
	}
	treePath := worktree.Data.TreePath

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	runResult, runErr := runAttachedInDir(ctx, shell, []string{"-l"}, treePath)
	if runErr != nil {
		return errors.Wrap(errors.EInternal, "failed to run shell", runErr)
	}
	if runResult.ExitCode != 0 {
		return errors.WithExitCode(
			errors.New(errors.EInternal, fmt.Sprintf("shell exited with code %d", runResult.ExitCode)),
			runResult.ExitCode,
		)
	}

	return nil
}
