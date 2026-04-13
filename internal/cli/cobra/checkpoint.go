package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newCheckpointCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Manage sandbox checkpoints",
		Long: `Manage sandbox checkpoints for agent invocations.

Checkpoints are automatic snapshots of sandbox state created during headless
agent execution. They allow rolling back to previous states if something
goes wrong.

Subcommands:
  ls        List checkpoints for an invocation
  apply     Restore sandbox to a checkpoint state`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency checkpoint <ls|apply>")
		},
	}

	cmd.AddCommand(
		newCheckpointLSCmd(),
		newCheckpointApplyCmd(),
	)

	return cmd
}

func newCheckpointLSCmd() *cobra.Command {
	var invocationRef string
	var repoFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "ls --invocation <id|prefix>",
		Short: "List checkpoints for an invocation",
		Long: `List checkpoints for an agent invocation.

Shows checkpoint history with timestamps and diffstats.

Example:
  agency checkpoint ls --invocation 20260201
  agency checkpoint ls --invocation my-inv --repo abc123 --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.CheckpointLS(ctx, cr, fsys, cwd, commands.CheckpointLSOpts{
				InvocationRef: invocationRef,
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&invocationRef, "invocation", "", "Invocation to list checkpoints for (required)")
	cmd.Flags().StringVar(&repoFlag, "repo", "", "Filter by repo name, key, id, or prefix")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func newCheckpointApplyCmd() *cobra.Command {
	var invocationRef string

	cmd := &cobra.Command{
		Use:   "apply --invocation <id|prefix> <checkpoint_id>",
		Short: "Restore sandbox to a checkpoint state",
		Long: `Restore a sandbox to a previous checkpoint state.

This operation is only allowed on stopped or finished invocations.
The sandbox working tree is reset to the exact state at checkpoint time.

After applying a checkpoint, you can start a new invocation on the sandbox.

Example:
  agency checkpoint apply --invocation 20260201 5
  agency checkpoint apply --invocation my-inv 3`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.CheckpointApply(ctx, cr, fsys, cwd, commands.CheckpointApplyOpts{
				InvocationRef: invocationRef,
				CheckpointID:  args[0],
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&invocationRef, "invocation", "", "Invocation to apply checkpoint for (required)")

	return cmd
}
