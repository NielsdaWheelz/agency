package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Register repos and inspect the repo registry",
		Long: `Register repositories so agency commands can target them by --repo.

Once a repo is registered, worktree and agent commands can resolve it from any
directory by repo ref. Repo refs accept a short name, owner/repo, repo key,
repo_id, or a unique prefix.`,
		Example: `  agency repo add /path/to/repo
  agency repo ls
  agency repo show agency
  agency repo rm agency --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand")
		},
	}

	cmd.AddCommand(
		newRepoAddCmd(),
		newRepoLSCmd(),
		newRepoRmCmd(),
		newRepoShowCmd(),
	)

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
		Use:   "show <repo-ref>",
		Short: "Show details of a registered repository",
		Long: `Show the canonical record for one registered repository.

Accepted repo refs include a short name, owner/repo, repo key, repo_id, or a
unique prefix.`,
		Example: `  agency repo show agency
  agency repo show NielsdaWheelz/agency
  agency repo show github:NielsdaWheelz/agency
  agency repo show 769749d
  agency repo show agency --json`,
		Args: cobra.ExactArgs(1),
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
		Use:   "rm <repo-ref>",
		Short: "Remove a registered repository",
		Long: `Remove one repository from the daemon's registry.

This removes the registry entry only. It does not delete any checkout, branch,
or worktree on disk.`,
		Example: `  agency repo rm agency --yes
  agency repo rm 769749d77af0806f --yes --json`,
		Args: cobra.ExactArgs(1),
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
