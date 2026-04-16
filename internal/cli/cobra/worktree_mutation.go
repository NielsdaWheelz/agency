package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newWorktreeRmCmd() *cobra.Command {
	var repoRef string
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
  agency worktree rm --repo agency my-feature
  agency worktree rm my-feature --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreeRm(ctx, cr, fsys, cwd, commands.WorktreeRmOpts{
				WorktreeRef: args[0],
				RepoRef:     repoRef,
				Force:       force,
				Yes:         yes,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
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
			return errors.New(errors.EUsage, "specify a subcommand: agency worktree pr <sync|merge>")
		},
	}
	cmd.AddCommand(newWorktreePRSyncCmd(), newWorktreePRMergeCmd())
	return cmd
}

func newWorktreePRSyncCmd() *cobra.Command {
	var repoRef string
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
  agency worktree pr sync --repo agency my-feature
  agency worktree pr sync --allow-dirty my-feature
  agency worktree pr sync --force-with-lease my-feature
  agency worktree pr sync --json my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreePRSync(ctx, cr, fsys, cwd, commands.WorktreePRSyncOpts{
				WorktreeRef:    args[0],
				RepoRef:        repoRef,
				AllowDirty:     allowDirty,
				ForceWithLease: forceWithLease,
				JSON:           jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	cmd.Flags().BoolVar(&allowDirty, "allow-dirty", false, "Allow sync with dirty integration worktree")
	cmd.Flags().BoolVar(&forceWithLease, "force-with-lease", false, "Use git push --force-with-lease")

	return cmd
}

func newWorktreePRMergeCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool
	var squash bool
	var merge bool
	var rebase bool
	var noDeleteBranch bool
	var yes bool
	var agencyConfigPath string

	cmd := &cobra.Command{
		Use:   "merge <worktree_ref>",
		Short: "Verify and merge worktree pull request",
		Long: `Perform worktree-scoped PR merge.

This command runs verify, merges the branch-scoped pull request, runs the
archive script, and archives the worktree by removing its tree directory.

Non-interactive executions must pass --yes.

Example:
  agency worktree pr merge my-feature
  agency worktree pr merge --repo agency my-feature
  agency worktree pr merge --yes --json my-feature
  agency worktree pr merge --merge my-feature
  agency worktree pr merge --rebase --no-delete-branch my-feature
  agency worktree pr merge --agency-config /path/to/agency.json --yes my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreePRMerge(ctx, cr, fsys, cwd, commands.WorktreePRMergeOpts{
				WorktreeRef:      args[0],
				RepoRef:          repoRef,
				Squash:           squash,
				Merge:            merge,
				Rebase:           rebase,
				NoDeleteBranch:   noDeleteBranch,
				Yes:              yes,
				JSON:             jsonOut,
				AgencyConfigPath: agencyConfigPath,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	cmd.Flags().BoolVar(&squash, "squash", false, "Use squash merge strategy (default)")
	cmd.Flags().BoolVar(&merge, "merge", false, "Use regular merge strategy")
	cmd.Flags().BoolVar(&rebase, "rebase", false, "Use rebase merge strategy")
	cmd.Flags().BoolVar(&noDeleteBranch, "no-delete-branch", false, "Preserve remote branch after merge")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm merge in non-interactive mode")
	cmd.Flags().StringVar(&agencyConfigPath, "agency-config", "", "load agency config from this file")

	return cmd
}

func newWorktreeRebaseCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "rebase <worktree_ref>",
		Short: "Rebase worktree branch onto base branch",
		Long: `Fetch origin and rebase the worktree branch onto origin/<base_branch>.

This command requires a clean worktree and returns a typed conflict error if
the rebase cannot be applied cleanly.

Example:
  agency worktree rebase my-feature
  agency worktree rebase --repo agency my-feature
  agency worktree rebase --json my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreeRebase(ctx, cr, fsys, cwd, commands.WorktreeRebaseOpts{
				WorktreeRef: args[0],
				RepoRef:     repoRef,
				JSON:        jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}
