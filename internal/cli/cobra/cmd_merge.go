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

func newMergeCmd() *cobra.Command {
	var squash bool
	var merge bool
	var rebase bool
	var noDeleteBranch bool
	var allowDirty bool
	var force bool

	cmd := &cobra.Command{
		Use:   "merge <run>",
		Short: "Verify, confirm, merge PR, and archive workspace",
		Long: `Verify, confirm, merge PR, and archive workspace.
Requires cwd to be inside the target repo.
Requires an interactive terminal for confirmation.

Arguments:
  run    run name, run_id, or unique run_id prefix

Behavior:
  1. runs prechecks (origin, gh auth, PR exists, mergeable, etc.)
  2. runs scripts.verify (timeout: 30m)
  3. if verify fails: prompts to continue (unless --force)
  4. prompts for typed confirmation (must type 'merge')
  5. merges PR via gh pr merge --delete-branch (unless --no-delete-branch)
  6. archives workspace (runs archive script, kills tmux, deletes worktree)

Notes:
  - PR must already exist (run 'agency push' first)
  - --force does NOT bypass: missing PR, non-mergeable PR, gh auth failure
  - at most one of --squash/--merge/--rebase may be set
  - by default, the remote branch is deleted after merge`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			// Validate merge strategy flags (at most one)
			strategyCount := 0
			var strategy commands.MergeStrategy
			if squash {
				strategyCount++
				strategy = commands.MergeStrategySquash
			}
			if merge {
				strategyCount++
				strategy = commands.MergeStrategyMerge
			}
			if rebase {
				strategyCount++
				strategy = commands.MergeStrategyRebase
			}

			if strategyCount > 1 {
				return errors.New(errors.EUsage, "at most one of --squash, --merge, --rebase may be specified")
			}
			if strategyCount == 0 {
				strategy = commands.MergeStrategySquash // default
			}

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.ENoRepo, "failed to get working directory", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			opts := commands.MergeOpts{
				RunID:          args[0],
				Strategy:       strategy,
				Force:          force,
				AllowDirty:     allowDirty,
				NoDeleteBranch: noDeleteBranch,
			}

			return commands.Merge(ctx, cr, fsys, cwd, opts, os.Stdin, stdout, stderr)
		},
	}

	cmd.Flags().BoolVar(&squash, "squash", false, "use squash merge strategy (default)")
	cmd.Flags().BoolVar(&merge, "merge", false, "use regular merge strategy")
	cmd.Flags().BoolVar(&rebase, "rebase", false, "use rebase merge strategy")
	cmd.Flags().BoolVar(&noDeleteBranch, "no-delete-branch", false, "preserve remote branch after merge")
	cmd.Flags().BoolVar(&allowDirty, "allow-dirty", false, "allow merge even if worktree has uncommitted changes")
	cmd.Flags().BoolVar(&force, "force", false, "bypass verify-failed prompt (still runs verify)")

	return cmd
}
