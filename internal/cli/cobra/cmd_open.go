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

	cmd := &cobra.Command{
		Use:   "open <run>",
		Short: "Open run worktree in editor",
		Long: `Open a run worktree in your editor.
Resolves globally (works from anywhere, not just inside a repo).

Arguments:
  run    run name, run_id, or unique run_id prefix`,
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

			opts := commands.OpenOpts{
				RunID:  args[0],
				Editor: editor,
			}

			return commands.Open(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&editor, "editor", "", "editor name (default: user config defaults.editor)")

	return cmd
}
