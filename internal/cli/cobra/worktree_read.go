package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newWorktreeLSCmd() *cobra.Command {
	var repoRef string
	var allRepos bool
	var all bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List integration worktrees",
		Long: `List integration worktrees for the current repository.

By default this lists present worktrees for one repo. Omit --repo only when
cwd already identifies that repo. Use --all-repos to list globally.

Examples:
  agency worktree ls
  agency worktree ls --all
  agency worktree ls --repo agency
  agency worktree ls --all-repos
  agency worktree ls --json
  agency watch`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreeLS(ctx, cr, fsys, cwd, commands.WorktreeLSOpts{
				RepoRef:  repoRef,
				AllRepos: allRepos,
				All:      all,
				JSON:     jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "List across all registered repos")
	cmd.Flags().BoolVar(&all, "all", false, "Include archived worktrees")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	cmd.MarkFlagsMutuallyExclusive("repo", "all-repos")
	registerRepoFlagCompletion(cmd)

	return cmd
}

func newWorktreeShowCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "<worktree-ref> [show]",
		Short: "Show details of a worktree",
		Long: `Show details of an integration worktree.

Pass --repo when cwd does not already identify the repo. The worktree argument
can be the worktree name, full id, or an unambiguous id prefix.

Examples:
  agency worktree my-feature
  agency worktree my-feature show
  agency worktree my-feature --json
  agency worktree my-feature show --repo agency`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return nil
			}
			if len(args) == 2 && args[1] == "show" {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.WorktreeShow(ctx, cr, fsys, cwd, commands.WorktreeShowOpts{
				WorktreeRef: args[0],
				RepoRef:     repoRef,
				JSON:        jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	setWorktreeArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)

	return cmd
}
