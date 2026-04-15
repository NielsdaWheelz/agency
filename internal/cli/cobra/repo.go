package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage registered repositories",
		Long: `Manage the daemon's repository registry.

The daemon maintains a registry of known repositories. Registering a repo
allows CWD-less operation: you can run agency commands from any directory
by specifying --repo.

Subcommands:
  add     Register a repository path
  ls      List registered repositories
  show    Show details of a registered repository`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency repo <add|ls|show>")
		},
	}

	cmd.AddCommand(
		newRepoAddCmd(),
		newRepoLSCmd(),
		newRepoShowCmd(),
	)

	return cmd
}

func newRepoAddCmd() *cobra.Command {
	var path string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Register a repository with the daemon",
		Long: `Register a repository root with the daemon.

If --path is omitted, the current working directory is used.
The daemon resolves the git toplevel and assigns a stable repo_id.

Example:
  agency repo add
  agency repo add --path /home/user/myrepo
  agency repo add --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, _, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.RepoAdd(ctx, cr, fsys, commands.RepoAddOpts{
				Path: path,
				JSON: jsonOutput,
			}, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "path to repository (defaults to cwd)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}

func newRepoLSCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List registered repositories",
		Long: `List all repositories registered with the daemon.

Example:
  agency repo ls
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

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}

func newRepoShowCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "show <repo_ref>",
		Short: "Show details of a registered repository",
		Long: `Show details of a registered repository.

Accepted repo ref forms: name, owner/repo, repo key, id, or unique prefix.

Example:
  agency repo show agency
  agency repo show NielsdaWheelz/agency
  agency repo show github:NielsdaWheelz/agency
  agency repo show 769749d
  agency repo show --json agency`,
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

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}
