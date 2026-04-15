package cobra

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newAgentCheckpointCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Manage invocation checkpoints",
		Long: `Manage checkpoints for agent invocations.

Checkpoints are automatic snapshots of sandbox state created during headless
agent execution. They allow rolling back to previous states if something
goes wrong.

Subcommands:
  ls        List checkpoints for an invocation
  apply     Restore sandbox to a checkpoint state`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency agent checkpoint <ls|apply>")
		},
	}

	cmd.AddCommand(
		newAgentCheckpointLSCmd(),
		newAgentCheckpointApplyCmd(),
	)

	return cmd
}

func newAgentCheckpointLSCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "ls <invocation_ref>",
		Short: "List checkpoints for an invocation",
		Long: `List checkpoints for an agent invocation.

Shows checkpoint history with timestamps and diffstats.

Example:
  agency agent checkpoint ls 20260201
  agency agent checkpoint ls my-inv --repo abc123 --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.CheckpointLS(ctx, cr, fsys, cwd, commands.CheckpointLSOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Filter by repo name, key, id, or prefix")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func newAgentCheckpointApplyCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "apply <invocation_ref> <checkpoint_id>",
		Short: "Restore sandbox to a checkpoint state",
		Long: `Restore a sandbox to a previous checkpoint state.

This operation is only allowed on stopped or finished invocations.
The sandbox working tree is reset to the exact state at checkpoint time.

After applying a checkpoint, you can start a new invocation on the sandbox.

Example:
  agency agent checkpoint apply 20260201 5
  agency agent checkpoint apply my-inv 3
  agency agent checkpoint apply my-inv 3 --repo abc123`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			checkpointID, err := strconv.Atoi(args[1])
			if err != nil || checkpointID <= 0 {
				return errors.New(errors.EUsage, "checkpoint_id must be a positive integer")
			}

			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.CheckpointApply(ctx, cr, fsys, cwd, commands.CheckpointApplyOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				CheckpointID:  checkpointID,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Filter by repo name, key, id, or prefix")

	return cmd
}
