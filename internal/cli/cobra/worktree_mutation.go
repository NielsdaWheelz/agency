package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newWorktreeRmCmd() *cobra.Command {
	var repoRef string
	var force bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "<worktree-ref> rm",
		Short: "Remove a worktree",
		Long: `Remove an integration worktree.

By default, fails if the worktree has uncommitted changes.
Use --force to remove regardless.

The worktree record is retained (archived state) but the tree directory is removed.

Examples:
  agency worktree my-feature rm
  agency worktree my-feature rm --repo agency
  agency worktree my-feature rm --force`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 && args[1] == "rm" {
				return nil
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
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
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm removal in non-interactive mode")
	setWorktreeArgCompletion(cmd, "present")
	registerRepoFlagCompletion(cmd)

	return cmd
}

func newWorktreePRSyncCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool
	var allowDirty bool
	var forceWithLease bool

	cmd := &cobra.Command{
		Use:   "<worktree-ref> pr sync",
		Short: "Push branch and create/update pull request",
		Long: `Perform worktree-scoped PR synchronization.

This command pushes the integration branch, then creates or updates the
branch-scoped pull request.

Examples:
  agency worktree my-feature pr sync
  agency worktree my-feature pr sync --repo agency
  agency worktree my-feature pr sync --allow-dirty
  agency worktree my-feature pr sync --force-with-lease
  agency worktree my-feature pr sync --json`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 3 && args[1] == "pr" && args[2] == "sync" {
				return nil
			}
			return cobra.ExactArgs(3)(cmd, args)
		},
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
	setWorktreeArgCompletion(cmd, "present")
	registerRepoFlagCompletion(cmd)

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
		Use:   "<worktree-ref> pr merge",
		Short: "Verify and merge worktree pull request",
		Long: `Perform worktree-scoped PR merge.

This command runs verify, merges the branch-scoped pull request, runs the
archive script, and archives the worktree by removing its tree directory.

Non-interactive executions must pass --yes. Pass at most one merge strategy
flag: --squash, --merge, or --rebase.

Examples:
  agency worktree my-feature pr merge
  agency worktree my-feature pr merge --repo agency
  agency worktree my-feature pr merge --yes --json
  agency worktree my-feature pr merge --merge
  agency worktree my-feature pr merge --rebase --no-delete-branch
  agency worktree my-feature pr merge --agency-config /path/to/agency.json --yes`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 3 && args[1] == "pr" && args[2] == "merge" {
				return nil
			}
			return cobra.ExactArgs(3)(cmd, args)
		},
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
	cmd.MarkFlagsMutuallyExclusive("squash", "merge", "rebase")
	setWorktreeArgCompletion(cmd, "present")
	registerRepoFlagCompletion(cmd)

	return cmd
}

func newWorktreeRebaseCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "<worktree-ref> rebase",
		Short: "Rebase worktree branch onto base branch",
		Long: `Fetch origin and rebase the worktree branch onto origin/<base_branch>.

This command requires a clean worktree and returns a typed conflict error if
the rebase cannot be applied cleanly.

Examples:
  agency worktree my-feature rebase
  agency worktree my-feature rebase --repo agency
  agency worktree my-feature rebase --json`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 && args[1] == "rebase" {
				return nil
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
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
	setWorktreeArgCompletion(cmd, "present")
	registerRepoFlagCompletion(cmd)

	return cmd
}
