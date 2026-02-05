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
	var repoFlag string
	var allRepos bool
	var all bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List integration worktrees",
		Long: `List integration worktrees for the current repository.

By default, only shows non-archived worktrees for the current repo.
Use --repo to specify a repo by id/prefix, or --all-repos to list globally.

Example:
  agency worktree ls
  agency worktree ls --all
  agency worktree ls --repo abc123
  agency worktree ls --all-repos
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
				RepoFlag: repoFlag,
				AllRepos: allRepos,
				All:      all,
				JSON:     jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Filter by repo id or unique prefix")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "List across all registered repos")
	cmd.Flags().BoolVar(&all, "all", false, "Include archived worktrees")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func newWorktreeShowCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "show <name|id|prefix>",
		Short: "Show details of a worktree",
		Long: `Show details of an integration worktree.

The worktree can be specified by name, id, or unique prefix.

Example:
  agency worktree show my-feature
  agency worktree show --repo abc123 my-feature
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
				RepoFlag:    repoFlag,
				JSON:        jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

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
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreePath(ctx, cr, fsys, cwd, commands.WorktreePathOpts{
				WorktreeRef: args[0],
				RepoFlag:    repoFlag,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")

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
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreeOpen(ctx, cr, fsys, cwd, commands.WorktreeOpenOpts{
				WorktreeRef: args[0],
				RepoFlag:    repoFlag,
				Editor:      editor,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")
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
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreeShell(ctx, cr, fsys, cwd, commands.WorktreeShellOpts{
				WorktreeRef: args[0],
				RepoFlag:    repoFlag,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")

	return cmd
}

func newWorktreeRmCmd() *cobra.Command {
	var repoFlag string
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
  agency worktree rm --repo abc123 my-feature
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
				RepoFlag:    repoFlag,
				Force:       force,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")
	cmd.Flags().BoolVar(&force, "force", false, "Force removal even if worktree has uncommitted changes")

	return cmd
}
