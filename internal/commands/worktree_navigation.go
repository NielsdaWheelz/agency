package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// WorktreePathOpts holds options for the worktree path command.
type WorktreePathOpts struct {
	WorktreeRef string
	RepoFlag    string
}

// WorktreePath outputs the path to an integration worktree.
func WorktreePath(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreePathOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	deps := ns.buildWorktreeNavDeps(cr, cwd, opts.RepoFlag, "worktree path")
	result, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetWorktree,
			Ref:        opts.WorktreeRef,
		},
	}, deps)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(stdout, result.ResolvedPath)
	return nil
}

// WorktreeOpenOpts holds options for the worktree open command.
type WorktreeOpenOpts struct {
	WorktreeRef string
	RepoFlag    string
	Editor      string
}

// WorktreeOpen opens an integration worktree in the configured editor.
func WorktreeOpen(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeOpenOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	deps := ns.buildWorktreeNavDeps(cr, cwd, opts.RepoFlag, "worktree open")
	result, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetWorktree,
			Ref:        opts.WorktreeRef,
		},
	}, deps)
	if err != nil {
		return err
	}

	treePath := result.ResolvedPath

	userCfg, found, _ := config.LoadUserConfig(fsys, ns.dirs.ConfigDir)
	editorName := opts.Editor
	if found && editorName == "" {
		editorName = userCfg.Defaults.Editor
	}

	editorCmd, err := config.ResolveEditorCmd(cr, fsys, ns.dirs.ConfigDir, userCfg, editorName)
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
	RepoFlag    string
}

// WorktreeShell opens a shell in an integration worktree.
func WorktreeShell(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeShellOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	deps := ns.buildWorktreeNavDeps(cr, cwd, opts.RepoFlag, "worktree shell")
	result, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetWorktree,
			Ref:        opts.WorktreeRef,
		},
	}, deps)
	if err != nil {
		return err
	}

	treePath := result.ResolvedPath

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

// Shared navigation kernel setup for worktree path/open/shell.
func (ns *daemonNavSetup) buildWorktreeNavDeps(cr exec.CommandRunner, cwd, repoFlag, cmdName string) NavigationDeps {
	return NavigationDeps{
		ResolveRepo: func(ctx context.Context) (*RepoContextResult, error) {
			return ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
				RepoFlag:      repoFlag,
				AllowAllRepos: false,
				CmdName:       cmdName,
			})
		},
		GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			result, err := ns.client.GetWorktree(ctx, ref, repoID)
			if err != nil {
				return nil, err
			}
			return &NavigationResult{
				TargetKind:     TargetWorktree,
				ResolvedRepoID: result.Data.RepoID,
				ResolvedID:     result.Data.WorktreeID,
				ResolvedPath:   result.Data.TreePath,
			}, nil
		},
	}
}
