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

func newShowCmd() *cobra.Command {
	var jsonOutput bool
	var pathOutput bool
	var capture bool

	cmd := &cobra.Command{
		Use:   "show <run>",
		Short: "Show details of a run",
		Long: `Show details for a single run.
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

			opts := commands.ShowOpts{
				RunID:   args[0],
				JSON:    jsonOutput,
				Path:    pathOutput,
				Capture: capture,
				Args:    args,
			}

			return commands.Show(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON (stable format)")
	cmd.Flags().BoolVar(&pathOutput, "path", false, "output only resolved filesystem paths")
	cmd.Flags().BoolVar(&capture, "capture", false, "capture tmux scrollback to transcript files (mutating mode)")

	return cmd
}
