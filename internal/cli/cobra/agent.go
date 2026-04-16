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

Agent invocations run a runner inside an isolated sandbox worktree and
produce logs, checkpoints, and lifecycle outcomes.

Use subcommands to run, inspect, navigate, recover, and finish invocations.`,
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

	checkpointCmd := newAgentCheckpointCmd()
	checkpointCmd.GroupID = "recover"

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
		newAgentEnterCmd(),
		newAgentChatCmd(),
		newAgentRecreateCmd(),
		checkpointCmd,
		newAgentRestartCmd(),
		newAgentHistoryCmd(),
		newAgentLogsCmd(),
		newAgentReviewCmd(),
	)

	return cmd
}
