package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agent invocations",
		Long: `Manage agent invocations.

An agent invocation runs a configured runner inside an isolated sandbox cloned
from an integration worktree. Invocations are the execution layer: they run the
model, stream logs, create checkpoints, and eventually land or discard work.

Use:
  agency agent start      to create a new sandbox and run a runner
  agency agent ls/show    to inspect invocations
  agency agent history    to read the timeline or open the history UI
  agency agent land       to apply sandbox changes back to integration`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency agent <command>")
		},
	}

	cmd.AddGroup(
		&cobra.Group{ID: "run", Title: "Run"},
		&cobra.Group{ID: "inspect", Title: "Inspect"},
		&cobra.Group{ID: "navigate", Title: "Navigate"},
		&cobra.Group{ID: "recover", Title: "Recover"},
		&cobra.Group{ID: "finish", Title: "Finish"},
	)

	cmd.AddCommand(
		newAgentStartCmd(),
		newAgentLSCmd(),
		newAgentShowCmd(),
		newAgentStopCmd(),
		newAgentKillCmd(),
		newAgentDiffCmd(),
		newAgentLandCmd(),
		newAgentDiscardCmd(),
		newAgentOpenCmd(),
		newAgentPathCmd(),
		newAgentShellCmd(),
		newAgentAttachCmd(),
		newAgentFollowupCmd(),
		newAgentRecreateCmd(),
		newAgentHistoryCmd(),
		newAgentRestoreCmd(),
		newAgentCheckCmd(),
	)

	return cmd
}
