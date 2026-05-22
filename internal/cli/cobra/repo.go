package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newRepoCmd() *cobra.Command {
	var jsonOutput bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Register repos and inspect the repo registry",
		Long: `Register repositories so agency commands can target them by --repo.

Once a repo is registered, worktree and agent commands can resolve it from any
directory by repo ref. Repo refs accept a short name, owner/repo, repo key,
repo_id, or a unique prefix.

Use:
  agency repo add [path]   to register a repo
  agency repo ls           to list registered repos
  agency repo <repo-ref>   to show one repo
  agency repo <repo-ref> rm --yes
                           to remove one repo`,
		Example: `  agency repo add /path/to/repo
  agency repo ls
  agency repo agency
  agency repo agency rm --yes`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				_ = cmd.Help()
			}

			if err := validateRepoTargetFlags(cmd, args); err != nil {
				return err
			}
			ctx, cr, fsys, _, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}
			return commands.RepoTarget(ctx, cr, fsys, commands.RepoTargetOpts{
				Args: args,
				JSON: jsonOutput,
				Yes:  yes,
			}, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "output as JSON")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm removal without prompting")

	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			candidates := completeStaticValues([]string{
				commands.RepoActionAdd + "\tRegister a repository",
				commands.RepoActionLS + "\tList registered repositories",
			}, toComplete)
			if len(candidates) > 0 {
				return candidates, cobra.ShellCompDirectiveNoFileComp
			}
			repoRefs, directive := completeRepoRefs(cmd, args, toComplete)
			return repoRefs, directive
		case 1:
			if args[0] == commands.RepoActionAdd || args[0] == commands.RepoActionLS {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeStaticValues(repoTargetActionCompletions(), toComplete), cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	return cmd
}

func repoTargetActionCompletions() []string {
	return []string{
		commands.RepoTargetActionRm,
	}
}

func validateRepoTargetFlags(cmd *cobra.Command, args []string) error {
	targetFlags := []string{"json", "yes"}
	if policy, ok := commands.RepoTargetFlagPolicy(args); ok {
		return validateChangedTargetFlags(cmd, "repo", policy.Action, targetFlags, policy.AllowedFlags...)
	}
	return nil
}
