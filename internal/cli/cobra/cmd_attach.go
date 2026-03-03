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

func newAttachCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "attach <invocation_ref>",
		Short: "Compatibility alias for 'agent attach'",
		Long: `Compatibility alias for 'agency agent attach'.
Attaches to a headed invocation after daemon-first resolution.

Arguments:
  invocation_ref   invocation id, name, or unique prefix`,
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

			opts := commands.AgentAttachOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
			}

			return commands.AgentAttach(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo id or unique prefix")

	return cmd
}
