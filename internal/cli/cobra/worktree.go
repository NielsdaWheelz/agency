package cobra

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Manage integration worktrees",
		Long: `Manage integration worktrees.

Integration worktrees are stable branches you intend to merge, push, or PR.
They are independent of any agent invocation.

Subcommands:
  create    Create a new integration worktree
  ls        List integration worktrees
  show      Show details of a worktree
  path      Output worktree path for scripting
  open      Open worktree in editor
  shell     Open shell in worktree
  rm        Remove a worktree`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency worktree <create|ls|show|path|open|shell|rm>")
		},
	}

	cmd.AddCommand(
		newWorktreeCreateCmd(),
		newWorktreeLSCmd(),
		newWorktreeShowCmd(),
		newWorktreePathCmd(),
		newWorktreeOpenCmd(),
		newWorktreeShellCmd(),
		newWorktreeRmCmd(),
	)

	return cmd
}

func newWorktreeCreateCmd() *cobra.Command {
	var name string
	var parent string
	var open bool
	var editor string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new integration worktree",
		Long: `Create a new integration worktree.

An integration worktree is a stable branch you intend to merge, push, or PR.
It is independent of any agent invocation.

Example:
  agency worktree create --name my-feature
  agency worktree create --name bugfix --parent develop --open`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return errors.New(errors.EUsage, "--name is required")
			}

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreeCreate(ctx, cr, fsys, cwd, commands.WorktreeCreateOpts{
				Name:         name,
				ParentBranch: parent,
				Open:         open,
				Editor:       editor,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Name for the integration worktree (required)")
	cmd.Flags().StringVar(&parent, "parent", "", "Parent branch to branch from (default: current branch)")
	cmd.Flags().BoolVar(&open, "open", false, "Open the worktree in editor after creation")
	cmd.Flags().StringVar(&editor, "editor", "", "Editor to use (overrides config)")

	return cmd
}

func newWorktreeLSCmd() *cobra.Command {
	var repoPath string
	var all bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List integration worktrees",
		Long: `List integration worktrees for the current repository.

By default, only shows non-archived worktrees.

Example:
  agency worktree ls
  agency worktree ls --all
  agency worktree ls --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreeLS(ctx, cr, fsys, cwd, commands.WorktreeLSOpts{
				RepoPath: repoPath,
				All:      all,
				JSON:     jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", "", "Path to git repository")
	cmd.Flags().BoolVar(&all, "all", false, "Include archived worktrees")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func newWorktreeShowCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "show <name|id|prefix>",
		Short: "Show details of a worktree",
		Long: `Show details of an integration worktree.

The worktree can be specified by name, id, or unique prefix.

Example:
  agency worktree show my-feature
  agency worktree show 20260131
  agency worktree show --json my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreeShow(ctx, cr, fsys, cwd, commands.WorktreeShowOpts{
				WorktreeRef: args[0],
				JSON:        jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func newWorktreePathCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "path <name|id|prefix>",
		Short: "Output worktree path for scripting",
		Long: `Output the tree path of an integration worktree.

Outputs only the path, suitable for scripting:
  cd $(agency worktree path my-feature)

Example:
  agency worktree path my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreePath(ctx, cr, fsys, cwd, commands.WorktreePathOpts{
				WorktreeRef: args[0],
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	return cmd
}

func newWorktreeOpenCmd() *cobra.Command {
	var editor string

	cmd := &cobra.Command{
		Use:   "open <name|id|prefix>",
		Short: "Open worktree in editor",
		Long: `Open an integration worktree in the configured editor.

Example:
  agency worktree open my-feature
  agency worktree open my-feature --editor cursor`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreeOpen(ctx, cr, fsys, cwd, commands.WorktreeOpenOpts{
				WorktreeRef: args[0],
				Editor:      editor,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&editor, "editor", "", "Editor to use (overrides config)")

	return cmd
}

func newWorktreeShellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell <name|id|prefix>",
		Short: "Open shell in worktree",
		Long: `Open a shell in an integration worktree.

Spawns $SHELL (or /bin/sh) with the worktree as the working directory.
Exiting the shell returns control to agency.

Example:
  agency worktree shell my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreeShell(ctx, cr, fsys, cwd, commands.WorktreeShellOpts{
				WorktreeRef: args[0],
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	return cmd
}

func newWorktreeRmCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "rm <name|id|prefix>",
		Short: "Remove a worktree",
		Long: `Remove an integration worktree.

By default, fails if the worktree has uncommitted changes.
Use --force to remove regardless.

The worktree record is retained (archived state) but the tree directory is removed.

Example:
  agency worktree rm my-feature
  agency worktree rm my-feature --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreeRm(ctx, cr, fsys, cwd, commands.WorktreeRmOpts{
				WorktreeRef: args[0],
				Force:       force,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force removal even if worktree has uncommitted changes")

	return cmd
}
