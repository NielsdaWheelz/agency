package cobra

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func newInitCmd() *cobra.Command {
	var repoPath string
	var noGitignore bool
	var force bool
	var local bool
	var repoConfig bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create repo-scoped agency config and stub scripts",
		Long: `Create repo-scoped agency config and stub scripts.
By default, this writes local per-repo config under AGENCY_CONFIG_DIR and nothing is written to the repo.
Use --repo-config to write shareable agency.json, scripts, .gitignore, and CLAUDE.md in the repo.
This command requires a git repo and does not create user config; run "agency config init" first.
Defaults to current directory; use --repo to target a different repo.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if local && repoConfig {
				return errors.New(errors.EUsage, "--local and --repo-config cannot both be set")
			}

			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get working directory", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			opts := commands.InitOpts{
				RepoPath:    repoPath,
				NoGitignore: noGitignore,
				Force:       force,
				RepoConfig:  repoConfig,
			}

			return commands.Init(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", "", "target a specific repo (default: current directory)")
	cmd.Flags().BoolVar(&noGitignore, "no-gitignore", false, "do not modify .gitignore")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing agency.json")
	cmd.Flags().BoolVar(&local, "local", false, "write local per-repo config under AGENCY_CONFIG_DIR (default)")
	cmd.Flags().BoolVar(&repoConfig, "repo-config", false, "write shareable agency.json and scripts in the repo")

	return cmd
}
