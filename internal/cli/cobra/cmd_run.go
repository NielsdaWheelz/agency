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

func newRunCmd() *cobra.Command {
	var name string
	var repoPath string
	var runner string
	var parent string
	var detached bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Create workspace, setup, and start tmux runner session",
		Long: `Create workspace, run setup, and start tmux runner session.
Defaults to current directory; use --repo to target a different repo.
Requires the target repo to have agency.json.
By default, attaches to the tmux session after creation.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			// --name is required
			if name == "" {
				_ = cmd.Help()
				return errors.New(errors.EUsage, "--name is required")
			}

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get working directory", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			opts := commands.RunOpts{
				Name:     name,
				RepoPath: repoPath,
				Runner:   runner,
				Parent:   parent,
				Attach:   !detached,
			}

			return commands.Run(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "run name (required, 2-40 chars, lowercase alphanumeric with hyphens)")
	cmd.Flags().StringVar(&repoPath, "repo", "", "target a specific repo (default: current directory)")
	cmd.Flags().StringVar(&runner, "runner", "", "runner name: claude or codex (default: user config defaults.runner)")
	cmd.Flags().StringVar(&parent, "parent", "", "parent branch (default: current branch)")
	cmd.Flags().BoolVar(&detached, "detached", false, "do not attach to tmux session after creation")

	return cmd
}
