package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "repo",
		Aliases: []string{"r"},
		Short:   "Register repos and inspect the repo registry",
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
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				_ = cmd.Help()
				return errors.New(errors.EUsage, "specify 'add', 'ls', or a repo ref")
			}

			if args[0] == "--help" || args[0] == "-h" {
				return cmd.Help()
			}

			switch args[0] {
			case "add":
				return errors.New(errors.EUsage, "use 'agency repo add [path]'")
			case "ls":
				return errors.New(errors.EUsage, "use 'agency repo ls'")
			case "show", "rm":
				return errors.New(errors.EUsage, "unknown command \""+args[0]+"\" for \"agency repo\"")
			}

			switch {
			case len(args) == 1:
				return runNestedCommand(cmd, newRepoShowCmd(), args)
			case strings.HasPrefix(args[1], "-"):
				return runNestedCommand(cmd, newRepoShowCmd(), args)
			case len(args) == 2 && args[1] == "show":
				return runNestedCommand(cmd, newRepoShowCmd(), args)
			case len(args) >= 2 && args[1] == "rm":
				return runNestedCommand(cmd, newRepoRmCmd(), args)
			default:
				return errors.New(errors.EUsage, "use 'agency repo <repo-ref>' or 'agency repo <repo-ref> rm --yes'")
			}
		},
	}

	cmd.AddCommand(
		newRepoAddCmd(),
		newRepoLSCmd(),
	)

	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			candidates := []string{"add\tRegister a repository", "ls\tList registered repositories"}
			repoRefs, directive := completeRepoRefs(cmd, args, toComplete)
			return append(candidates, repoRefs...), directive
		case 1:
			if args[0] == "add" || args[0] == "ls" {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			values := []string{"show", "rm"}
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

func newRepoAddCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "add [path]",
		Short: "Register a repository with the daemon",
		Long: `Register one git repository with the daemon.

Pass a checkout path explicitly, or omit [path] to register the repository that
contains the current directory. The daemon resolves the git toplevel and stores
the stable repo_id and repo_key that later --repo lookups use.`,
		Example: `  agency repo add
  agency repo add /home/user/myrepo
  agency repo add /home/user/myrepo --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, _, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			path := ""
			if len(args) == 1 {
				path = args[0]
			}

			return commands.RepoAdd(ctx, cr, fsys, commands.RepoAddOpts{
				Path: path,
				JSON: jsonOutput,
			}, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "output as JSON")

	return cmd
}

func newRepoLSCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List registered repositories",
		Long: `List every repository currently registered with the daemon.

This is the starting point when you need a repo ref to pass to --repo.`,
		Example: `  agency repo ls
  agency repo ls --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, _, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.RepoLS(ctx, cr, fsys, commands.RepoLSOpts{
				JSON: jsonOutput,
			}, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "output as JSON")

	return cmd
}

func newRepoShowCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "<repo-ref> [show]",
		Short: "Show details of a registered repository",
		Long: `Show the canonical record for one registered repository.

Accepted repo refs include a short name, owner/repo, repo key, repo_id, or a
unique prefix.`,
		Example: `  agency repo agency
  agency repo agency show
  agency repo NielsdaWheelz/agency
  agency repo github:NielsdaWheelz/agency
  agency repo 769749d
  agency repo agency --json`,
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
			ctx, cr, fsys, _, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.RepoShow(ctx, cr, fsys, commands.RepoShowOpts{
				RepoRef: args[0],
				JSON:    jsonOutput,
			}, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "output as JSON")

	return cmd
}

func newRepoRmCmd() *cobra.Command {
	var yes bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "<repo-ref> rm",
		Short: "Remove a registered repository",
		Long: `Remove one repository from the daemon's registry.

This removes the registry entry only. It does not delete any checkout, branch,
or worktree on disk.`,
		Example: `  agency repo agency rm --yes
  agency repo 769749d77af0806f rm --yes --json`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 && args[1] == "rm" {
				return nil
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, _, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.RepoRm(ctx, cr, fsys, commands.RepoRmOpts{
				RepoRef: args[0],
				Yes:     yes,
				JSON:    jsonOutput,
			}, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "confirm removal without prompting")
	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "output as JSON")

	return cmd
}
