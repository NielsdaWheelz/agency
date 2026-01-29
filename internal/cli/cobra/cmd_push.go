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

func newPushCmd() *cobra.Command {
	var allowDirty bool
	var force bool
	var forceWithLease bool

	cmd := &cobra.Command{
		Use:   "push <run>",
		Short: "Push branch to origin and create/update GitHub PR",
		Long: `Push the run branch to origin.
Creates/updates GitHub PR.

Arguments:
  run    run name, run_id, or unique run_id prefix

Notes:
  - requires origin to be a github.com remote
  - requires gh to be authenticated
  - does NOT bypass E_EMPTY_DIFF (at least one commit required)
  - fails if worktree has uncommitted changes unless --allow-dirty
  - uses report as PR body when complete; otherwise auto-generates a PR body
  - use --force-with-lease after rebasing to update an existing branch safely`,
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

			opts := commands.PushOpts{
				RunID:          args[0],
				Force:          force,
				AllowDirty:     allowDirty,
				ForceWithLease: forceWithLease,
			}

			return commands.Push(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().BoolVar(&allowDirty, "allow-dirty", false, "allow push even if worktree has uncommitted changes")
	cmd.Flags().BoolVar(&force, "force", false, "retained for compatibility (no-op for report checks)")
	cmd.Flags().BoolVar(&forceWithLease, "force-with-lease", false, "use git push --force-with-lease (required after rebase)")

	return cmd
}
