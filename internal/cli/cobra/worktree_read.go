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

By default, only shows non-archived worktrees for the current repo.
Use --repo to specify a repo ref (name, owner/repo, repo key, id, or prefix), or --all-repos to list globally.

Example:
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

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "List across all registered repos")
	cmd.Flags().BoolVar(&all, "all", false, "Include archived worktrees")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}

func newWorktreeShowCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "show <name|id|prefix>",
		Short: "Show details of a worktree",
		Long: `Show details of an integration worktree.

The worktree can be specified by name, id, or unique prefix.

Example:
  agency worktree show my-feature
  agency worktree show --repo agency my-feature
  agency worktree show --json my-feature`,
		Args: cobra.ExactArgs(1),
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

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}
