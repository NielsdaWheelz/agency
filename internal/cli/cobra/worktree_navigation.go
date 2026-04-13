package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newWorktreePathCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "path <name|id|prefix>",
		Short: "Output worktree path for scripting",
		Long: `Output the tree path of an integration worktree.

Outputs only the path, suitable for scripting:
  cd $(agency worktree path my-feature)

Example:
  agency worktree path my-feature
  agency worktree path --repo abc123 my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreePath(ctx, cr, fsys, cwd, commands.WorktreePathOpts{
				WorktreeRef: args[0],
				RepoFlag:    repoFlag,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	return cmd
}

func newWorktreeOpenCmd() *cobra.Command {
	var repoFlag string
	var editor string

	cmd := &cobra.Command{
		Use:   "open <name|id|prefix>",
		Short: "Open worktree in editor",
		Long: `Open an integration worktree in the configured editor.

Example:
  agency worktree open my-feature
  agency worktree open --repo abc123 my-feature
  agency worktree open my-feature --editor cursor`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreeOpen(ctx, cr, fsys, cwd, commands.WorktreeOpenOpts{
				WorktreeRef: args[0],
				RepoFlag:    repoFlag,
				Editor:      editor,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().StringVar(&editor, "editor", "", "Editor to use (overrides config)")
	return cmd
}

func newWorktreeShellCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "shell <name|id|prefix>",
		Short: "Open shell in worktree",
		Long: `Open a shell in an integration worktree.

Spawns $SHELL (or /bin/sh) with the worktree as the working directory.
Exiting the shell returns control to agency.

Example:
  agency worktree shell my-feature
  agency worktree shell --repo abc123 my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreeShell(ctx, cr, fsys, cwd, commands.WorktreeShellOpts{
				WorktreeRef: args[0],
				RepoFlag:    repoFlag,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	return cmd
}
