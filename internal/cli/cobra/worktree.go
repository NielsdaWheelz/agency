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
  rm        Remove a worktree
  pr sync   Push branch and sync pull request
  merge     Verify and merge pull request
  update    Rebase worktree branch onto parent`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency worktree <create|ls|show|path|open|shell|rm|pr|merge|update>")
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
		newWorktreePRCmd(),
		newWorktreeMergeCmd(),
		newWorktreeUpdateCmd(),
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
			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

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
	cmd.Flags().BoolVarP(&open, "open", "o", false, "Open the worktree in editor after creation")
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
  agency worktree ls --json
  agency watch`,
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

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Filter by repo name, key, id, or prefix")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "List across all registered repos")
	cmd.Flags().BoolVar(&all, "all", false, "Include archived worktrees")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

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

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

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

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")

	return cmd
}

func newWorktreeRmCmd() *cobra.Command {
	var repoFlag string
	var force bool
	var yes bool

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
				Yes:         yes,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVar(&force, "force", false, "Force removal even if worktree has uncommitted changes")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm remove in non-interactive mode")

	return cmd
}

func newWorktreePRCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Worktree-scoped pull request operations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency worktree pr <sync>")
		},
	}
	cmd.AddCommand(newWorktreePRSyncCmd())
	return cmd
}

func newWorktreePRSyncCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool
	var allowDirty bool
	var forceWithLease bool

	cmd := &cobra.Command{
		Use:   "sync <worktree_ref>",
		Short: "Push branch and create/update pull request",
		Long: `Perform worktree-scoped PR synchronization.

This command pushes the integration branch, then creates or updates the
branch-scoped pull request.

Example:
  agency worktree pr sync my-feature
  agency worktree pr sync --repo abc123 my-feature
  agency worktree pr sync --allow-dirty my-feature
  agency worktree pr sync --force-with-lease my-feature
  agency worktree pr sync --json my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreePRSync(ctx, cr, fsys, cwd, commands.WorktreePRSyncOpts{
				WorktreeRef:    args[0],
				RepoFlag:       repoFlag,
				AllowDirty:     allowDirty,
				ForceWithLease: forceWithLease,
				JSON:           jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	cmd.Flags().BoolVar(&allowDirty, "allow-dirty", false, "Allow sync with dirty integration worktree")
	cmd.Flags().BoolVar(&forceWithLease, "force-with-lease", false, "Use git push --force-with-lease")

	return cmd
}

func newWorktreeMergeCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool
	var squash bool
	var merge bool
	var rebase bool
	var noDeleteBranch bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "merge <worktree_ref>",
		Short: "Verify and merge pull request",
		Long: `Perform worktree-scoped merge.

This command runs verify, merges the branch-scoped pull request, and persists
merge logs under the worktree record.

Non-interactive executions must pass --yes.

Example:
  agency worktree merge my-feature
  agency worktree merge --repo abc123 my-feature
  agency worktree merge --yes --json my-feature
  agency worktree merge --merge my-feature
  agency worktree merge --rebase --no-delete-branch my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreePRMerge(ctx, cr, fsys, cwd, commands.WorktreePRMergeOpts{
				WorktreeRef:    args[0],
				RepoFlag:       repoFlag,
				Squash:         squash,
				Merge:          merge,
				Rebase:         rebase,
				NoDeleteBranch: noDeleteBranch,
				Yes:            yes,
				JSON:           jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	cmd.Flags().BoolVar(&squash, "squash", false, "Use squash merge strategy (default)")
	cmd.Flags().BoolVar(&merge, "merge", false, "Use regular merge strategy")
	cmd.Flags().BoolVar(&rebase, "rebase", false, "Use rebase merge strategy")
	cmd.Flags().BoolVar(&noDeleteBranch, "no-delete-branch", false, "Preserve remote branch after merge")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm merge in non-interactive mode")

	return cmd
}

func newWorktreeUpdateCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "update <worktree_ref>",
		Short: "Rebase worktree branch onto parent branch",
		Long: `Fetch origin and rebase the worktree branch onto origin/<parent_branch>.

This command requires a clean worktree and returns a typed conflict error if
the rebase cannot be applied cleanly.

Example:
  agency worktree update my-feature
  agency worktree update --repo abc123 my-feature
  agency worktree update --json my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.WorktreeUpdate(ctx, cr, fsys, cwd, commands.WorktreeUpdateOpts{
				WorktreeRef: args[0],
				RepoFlag:    repoFlag,
				JSON:        jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}
