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

Available subcommands will be added in future releases:
  start     Start a new agent invocation
  ls        List agent invocations
  show      Show details of an invocation
  attach    Attach to a running invocation
  stop      Stop an invocation gracefully
  kill      Kill an invocation forcefully
  diff      Show sandbox changes
  land      Apply sandbox changes to integration
  discard   Discard sandbox changes
  open      Open sandbox in editor
  logs      View invocation logs`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency agent <start|ls|show|attach|stop|kill|diff|land|discard|open|logs>")
		},
	}

	return cmd
}
