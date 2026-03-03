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

func newOpenCmd() *cobra.Command {
	var editor string
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "open <invocation_ref>",
		Short: "Compatibility alias for 'agent open'",
		Long: `Compatibility alias for 'agency agent open'.
Opens daemon-resolved sandbox path in your editor.

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

			opts := commands.AgentOpenOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				Editor:        editor,
			}

			return commands.AgentOpen(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&editor, "editor", "", "editor name (default: user config defaults.editor)")
	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo id or unique prefix")

	return cmd
}
