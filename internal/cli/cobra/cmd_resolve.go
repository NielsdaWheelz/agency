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

func newResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve <run>",
		Short: "Show conflict resolution guidance for a run",
		Long: `Show conflict resolution guidance for a run.
Provides step-by-step instructions to resolve merge conflicts via rebase.
Read-only: makes no git changes, does not require repo lock.

Arguments:
  run    run name, run_id, or unique run_id prefix

Behavior:
  - if worktree present: prints action card to stdout, exits 0
  - if worktree missing: prints partial guidance to stderr, exits with E_WORKTREE_MISSING

Output includes:
  - PR URL, base branch, run branch, worktree path
  - numbered steps: open, fetch, rebase, resolve, push --force-with-lease, merge
  - fallback cd command`,
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

			opts := commands.ResolveOpts{
				RunID: args[0],
			}

			return commands.Resolve(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	return cmd
}
