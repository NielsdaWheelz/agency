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
		Use:   "open <ref>",
		Short: "Open a run or invocation worktree in your editor",
		Long: `Open a run or invocation worktree in your editor.

Resolves the reference as an invocation first (daemon-first), then
falls back to legacy run resolution if no invocation matches.

Arguments:
  ref   run or invocation id, name, or unique prefix`,
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

			// Try invocation resolution first (daemon-first, S8 path).
			agentErr := commands.AgentOpen(ctx, cr, fsys, cwd, commands.AgentOpenOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				Editor:        editor,
			}, stdout, stderr)
			if agentErr == nil {
				return nil
			}

			// Only fall back to legacy run resolution when the ref
			// simply doesn't exist as an invocation. All other errors
			// (daemon failures, ambiguity, sandbox missing, editor
			// errors) are authoritative and must propagate.
			if errors.GetCode(agentErr) != errors.EInvocationNotFound {
				return agentErr
			}

			return commands.Open(ctx, cr, fsys, cwd, commands.OpenOpts{
				RunID:  args[0],
				Editor: editor,
			}, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&editor, "editor", "", "editor name (default: user config defaults.editor)")
	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")

	return cmd
}
