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

func newPathCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "path <invocation_ref>",
		Short: "Compatibility alias for 'agent path'",
		Long: `Compatibility alias for 'agency agent path'.
Prints daemon-resolved sandbox path as a single line for scripting.

Arguments:
  invocation_ref   invocation id, name, or unique prefix

Shell integration:
  # add to your .bashrc or .zshrc:
  acd() { cd "$(agency path "$1")" || return 1; }

  # then use:
  acd my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get working directory", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			opts := commands.AgentPathOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
			}

			return commands.AgentPath(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo id or unique prefix")

	return cmd
}
