package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newWorktreePathCmd() *cobra.Command {
	var repoRef string

	cmd := &cobra.Command{
		Use:     "<worktree-ref> path",
		Aliases: []string{"_path"},
		Short:   "Output worktree path for scripting",
		Long: `Output the tree path of an integration worktree.

This prints only the path, so it is suitable for scripting:
  cd $(agency worktree my-feature path)

Examples:
  agency worktree my-feature path
  agency worktree my-feature path --repo agency`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreePath(ctx, cr, fsys, cwd, commands.WorktreePathOpts{
				WorktreeRef: args[0],
				RepoRef:     repoRef,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	setWorktreeArgCompletion(cmd, "present")
	registerRepoFlagCompletion(cmd)
	return cmd
}

func newWorktreeOpenCmd() *cobra.Command {
	var repoRef string
	var editor string

	cmd := &cobra.Command{
		Use:     "<worktree-ref> open",
		Aliases: []string{"_open"},
		Short:   "Open worktree in editor",
		Long: `Open an integration worktree in the configured editor.

Examples:
  agency worktree my-feature open
  agency worktree my-feature open --repo agency
  agency worktree my-feature open --editor cursor`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreeOpen(ctx, cr, fsys, cwd, commands.WorktreeOpenOpts{
				WorktreeRef: args[0],
				RepoRef:     repoRef,
				Editor:      editor,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().StringVar(&editor, "editor", "", "Editor to use (overrides config)")
	setWorktreeArgCompletion(cmd, "present")
	registerRepoFlagCompletion(cmd)
	return cmd
}

func newWorktreeShellCmd() *cobra.Command {
	var repoRef string

	cmd := &cobra.Command{
		Use:     "<worktree-ref> shell",
		Aliases: []string{"_shell"},
		Short:   "Open shell in worktree",
		Long: `Open a shell in an integration worktree.

Spawns $SHELL (or /bin/sh) with the worktree as the working directory.
Exiting the shell returns control to agency.

Examples:
  agency worktree my-feature shell
  agency worktree my-feature shell --repo agency`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreeShell(ctx, cr, fsys, cwd, commands.WorktreeShellOpts{
				WorktreeRef: args[0],
				RepoRef:     repoRef,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	setWorktreeArgCompletion(cmd, "present")
	registerRepoFlagCompletion(cmd)
	return cmd
}
