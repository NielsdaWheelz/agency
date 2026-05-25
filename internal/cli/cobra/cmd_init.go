package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newInitCmd() *cobra.Command {
	var path string
	var noGitignore bool
	var force bool
	var repoConfig bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create agency config for one git checkout",
		Long: `Create agency config for one existing git repository.

By default, init writes local per-repo config under AGENCY_CONFIG_DIR and does
not modify the repository checkout. Use --repo-config when you want shareable
repo files in the checkout itself: agency.json, scripts, .gitignore, and
CLAUDE.md. Repo-shared writes must target the original repo checkout, not an
agency-managed worktree or sandbox.

This command requires user config from "agency config init". If --path is
omitted, the current directory must be inside the repository you want to
initialize.`,
		Example: `  agency init
  agency init --path /path/to/repo
  agency init --repo-config
  agency init --path /path/to/repo --repo-config --force`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}
			return commands.Init(ctx, cr, fsys, cwd, commands.InitOpts{
				Path:        path,
				NoGitignore: noGitignore,
				Force:       force,
				RepoConfig:  repoConfig,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "target checkout path (defaults to current directory)")
	cmd.Flags().BoolVar(&noGitignore, "no-gitignore", false, "do not modify .gitignore")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing agency.json")
	cmd.Flags().BoolVar(&repoConfig, "repo-config", false, "write shareable agency.json and scripts in the repo")

	return cmd
}
