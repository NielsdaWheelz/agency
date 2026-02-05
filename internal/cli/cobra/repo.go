package cobra

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
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
			if path == "" {
				var err error
				path, err = os.Getwd()
				if err != nil {
					return errors.Wrap(errors.EInternal, "failed to get cwd", err)
				}
			}

			cwd, _ := os.Getwd()
			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()

			return commands.RepoAdd(cmd.Context(), cr, fsys, cwd, commands.RepoAddOpts{
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
			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()

			return commands.RepoLS(cmd.Context(), cr, fsys, commands.RepoLSOpts{
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
		Use:   "show <repo-id>",
		Short: "Show details of a registered repository",
		Long: `Show details of a registered repository.

The argument can be a full repo_id or a unique prefix.

Example:
  agency repo show abc123
  agency repo show --json abc123`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()

			return commands.RepoShow(cmd.Context(), cr, fsys, commands.RepoShowOpts{
				RepoID: args[0],
				JSON:   jsonOutput,
			}, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}
