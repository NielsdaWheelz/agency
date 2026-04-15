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

Agent invocations are executions of runners (Claude, Codex, etc.) inside
sandbox worktrees. Each invocation is isolated and produces logs,
checkpoints, and outcomes.

Subcommands:
  start     Start a new agent invocation
  ls        List agent invocations
  show      Show details of an invocation
  enter     Attach to a running headed invocation (canonical)
  stop      Stop an invocation gracefully (Ctrl-C)
  kill      Kill an invocation forcefully
  diff      Show sandbox changes vs integration
  land      Apply sandbox changes to integration
  discard   Discard sandbox changes
  open      Open sandbox in editor
  path      Print sandbox path
  shell     Open shell in sandbox
  chat      Send follow-up prompt to a headless invocation
  history   Show unified invocation timeline
  checkpoint Manage invocation checkpoints
  restart   Restart invocation from checkpoint
  logs      View invocation logs
  review    Show review/readiness surface`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency agent <start|ls|show|...>")
		},
	}

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
		newAgentCheckpointCmd(),
		newAgentRestartCmd(),
		newAgentHistoryCmd(),
		newAgentLogsCmd(),
		newAgentReviewCmd(),
	)

	return cmd
}
