package cobra

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func newAttachCmd() *cobra.Command {
	var repoPath string

	cmd := &cobra.Command{
		Use:   "attach <run>",
		Short: "Attach to a tmux session for an existing run",
		Long: `Attach to the tmux session for an existing run.
Works from any directory; resolves runs globally.

Arguments:
  run    run name, run_id, or unique run_id prefix

Resolution:
  - run_id and prefix resolution is always global
  - name resolution prefers current repo (if inside one)
  - use --repo to disambiguate when names conflict across repos`,
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

			opts := commands.AttachOpts{
				RunID:    args[0],
				RepoPath: repoPath,
			}

			err = commands.Attach(ctx, cr, fsys, cwd, opts, stdout, stderr)
			if err != nil {
				// Print helpful details for E_SESSION_NOT_FOUND
				if ae, ok := errors.AsAgencyError(err); ok && ae.Code == errors.ESessionNotFound {
					if ae.Details != nil {
						if suggestion := ae.Details["suggestion"]; suggestion != "" {
							_, _ = fmt.Fprintf(stderr, "\n%s\n", suggestion)
						}
					}
				}
			}
			return err
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", "", "scope name resolution to a specific repo")

	return cmd
}
