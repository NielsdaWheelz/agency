package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
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
			switch {
			case len(args) == 0:
				_ = cmd.Help()
				return errors.New(errors.EUsage, "specify 'add', 'ls', or a repo ref")
			case args[0] == "add":
				if err := validateRepoTargetFlags(cmd, "add", "json"); err != nil {
					return err
				}
				if len(args) > 2 {
					return errors.New(errors.EUsage, "too many arguments for \"agency repo add\"")
				}
				path := ""
				if len(args) == 2 {
					path = args[1]
				}
				return runRepoAdd(cmd, path, jsonOutput)
			case args[0] == "ls":
				if err := validateRepoTargetFlags(cmd, "ls", "json"); err != nil {
					return err
				}
				if len(args) > 1 {
					return errors.New(errors.EUsage, "too many arguments for \"agency repo ls\"")
				}
				return runRepoLS(cmd, jsonOutput)
			default:
				repoRef := args[0]
				if len(args) == 1 {
					if err := validateRepoTargetFlags(cmd, "<repo-ref>", "json"); err != nil {
						return err
					}
					return runRepoShow(cmd, repoRef, jsonOutput)
				}
				if len(args) == 2 && args[1] == "rm" {
					if err := validateRepoTargetFlags(cmd, "rm", "json", "yes"); err != nil {
						return err
					}
					return runRepoRm(cmd, repoRef, yes, jsonOutput)
				}
				return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency repo\"")
			}
		},
	}

	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "output as JSON")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm removal without prompting")

	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			candidates := []string{}
			if toComplete == "" || strings.HasPrefix("add", toComplete) {
				candidates = append(candidates, "add\tRegister a repository")
			}
			if toComplete == "" || strings.HasPrefix("ls", toComplete) {
				candidates = append(candidates, "ls\tList registered repositories")
			}
			if len(candidates) > 0 {
				return candidates, cobra.ShellCompDirectiveNoFileComp
			}
			repoRefs, directive := completeRepoRefs(cmd, args, toComplete)
			return repoRefs, directive
		case 1:
			if args[0] == "add" || args[0] == "ls" {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			values := []string{"rm"}
			candidates := make([]string, 0, len(values))
			for _, value := range values {
				if toComplete != "" && !strings.HasPrefix(value, toComplete) {
					continue
				}
				candidates = append(candidates, value)
			}
			return candidates, cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	return cmd
}

func validateRepoTargetFlags(cmd *cobra.Command, action string, allowed ...string) error {
	allowedFlags := make(map[string]bool, len(allowed))
	for _, flag := range allowed {
		allowedFlags[flag] = true
	}
	for _, flag := range []string{"json", "yes"} {
		if cmd.Flags().Changed(flag) && !allowedFlags[flag] {
			return errors.New(errors.EUsage, "--"+flag+" is not valid for agency repo "+action)
		}
	}
	return nil
}

func runRepoAdd(cmd *cobra.Command, path string, jsonOutput bool) error {
	ctx, cr, fsys, _, err := realCommandDeps(cmd.Context())
	if err != nil {
		return err
	}

	return commands.RepoAdd(ctx, cr, fsys, commands.RepoAddOpts{
		Path: path,
		JSON: jsonOutput,
	}, cmd.OutOrStdout(), cmd.OutOrStderr())
}

func runRepoLS(cmd *cobra.Command, jsonOutput bool) error {
	ctx, cr, fsys, _, err := realCommandDeps(cmd.Context())
	if err != nil {
		return err
	}

	return commands.RepoLS(ctx, cr, fsys, commands.RepoLSOpts{
		JSON: jsonOutput,
	}, cmd.OutOrStdout(), cmd.OutOrStderr())
}

func runRepoShow(cmd *cobra.Command, repoRef string, jsonOutput bool) error {
	ctx, cr, fsys, _, err := realCommandDeps(cmd.Context())
	if err != nil {
		return err
	}

	return commands.RepoShow(ctx, cr, fsys, commands.RepoShowOpts{
		RepoRef: repoRef,
		JSON:    jsonOutput,
	}, cmd.OutOrStdout(), cmd.OutOrStderr())
}

func runRepoRm(cmd *cobra.Command, repoRef string, yes bool, jsonOutput bool) error {
	ctx, cr, fsys, _, err := realCommandDeps(cmd.Context())
	if err != nil {
		return err
	}

	return commands.RepoRm(ctx, cr, fsys, commands.RepoRmOpts{
		RepoRef: repoRef,
		Yes:     yes,
		JSON:    jsonOutput,
	}, cmd.OutOrStdout(), cmd.OutOrStderr())
}
