package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "agent",
		Aliases: []string{"ag"},
		Short:   "Manage agent invocations",
		Long: `Manage agent invocations.

An agent invocation runs a configured runner inside an isolated sandbox cloned
from an integration worktree. Invocations are the execution layer: they run the
model, stream logs, create checkpoints, and eventually land or discard work.

Use:
  agency agent start       to create a new sandbox from the active context
  agency agent start --worktree <worktree-ref>
                           to create a new sandbox from one worktree
  agency agent ls         to list invocations
  agency agent <invocation-ref>
                           to show one invocation
  agency agent <invocation-ref> history
                           to inspect one invocation
  agency agent <invocation-ref> clients
                           to inspect headed tmux clients
  agency agent <invocation-ref> land
                           to apply sandbox changes back to integration`,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				_ = cmd.Help()
				return errors.New(errors.EUsage, "specify 'start', 'ls', or an invocation ref")
			}

			if args[0] == "--help" || args[0] == "-h" {
				return cmd.Help()
			}

			switch args[0] {
			case "start":
				return errors.New(errors.EUsage, "use 'agency agent start' or 'agency agent start --worktree <worktree-ref>'")
			case "ls":
				return errors.New(errors.EUsage, "use 'agency agent ls'")
			case "show", "check", "diff", "history", "open", "path", "shell", "attach", "clients", "stop", "kill", "land", "discard", "followup", "recreate", "restore", "logs", "checkpoint", "restart":
				return errors.New(errors.EUsage, "unknown command \""+args[0]+"\" for \"agency agent\"")
			}

			switch {
			case len(args) == 1:
				return runNestedCommand(cmd, newAgentShowCmd(), args)
			case strings.HasPrefix(args[1], "-"):
				return runNestedCommand(cmd, newAgentShowCmd(), args)
			case args[1] == "show":
				return runNestedCommand(cmd, newAgentShowCmd(), args)
			case args[1] == "check":
				return runNestedCommand(cmd, newAgentCheckCmd(), args)
			case args[1] == "diff":
				return runNestedCommand(cmd, newAgentDiffCmd(), args)
			case args[1] == "history" && len(args) >= 3 && args[2] == "logs":
				return runNestedCommand(cmd, newAgentHistoryLogsCmd(), args)
			case args[1] == "history":
				return runNestedCommand(cmd, newAgentHistoryCmd(), args)
			case args[1] == "open":
				return runNestedCommand(cmd, newAgentOpenCmd(), args)
			case args[1] == "path":
				return runNestedCommand(cmd, newAgentPathCmd(), args)
			case args[1] == "shell":
				return runNestedCommand(cmd, newAgentShellCmd(), args)
			case args[1] == "attach":
				return runNestedCommand(cmd, newAgentAttachCmd(), args)
			case args[1] == "clients":
				return runNestedCommand(cmd, newAgentClientsCmd(), args)
			case args[1] == "stop":
				return runNestedCommand(cmd, newAgentStopCmd(), args)
			case args[1] == "kill":
				return runNestedCommand(cmd, newAgentKillCmd(), args)
			case args[1] == "land":
				return runNestedCommand(cmd, newAgentLandCmd(), args)
			case args[1] == "discard":
				return runNestedCommand(cmd, newAgentDiscardCmd(), args)
			case args[1] == "followup":
				return runNestedCommand(cmd, newAgentFollowupCmd(), args)
			case args[1] == "recreate":
				return runNestedCommand(cmd, newAgentRecreateCmd(), args)
			case args[1] == "restore":
				return runNestedCommand(cmd, newAgentRestoreCmd(), args)
			default:
				return errors.New(errors.EUsage, "use 'agency agent <invocation-ref>' or 'agency agent <invocation-ref> <show|check|diff|history|open|path|shell|attach|clients|stop|kill|land|discard|followup|recreate|restore>'")
			}
		},
	}

	cmd.AddGroup(
		&cobra.Group{ID: "run", Title: "Run"},
		&cobra.Group{ID: "inspect", Title: "Inspect"},
	)

	cmd.AddCommand(
		newAgentStartCmd(),
		newAgentLSCmd(),
	)

	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		for i, arg := range args {
			switch arg {
			case "--repo":
				if i == len(args)-1 {
					return completeRepoRefs(cmd, args, toComplete)
				}
				return nil, cobra.ShellCompDirectiveNoFileComp
			case "--kind":
				if i == len(args)-1 {
					return completeLogKinds(cmd, args, toComplete)
				}
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		}

		switch len(args) {
		case 0:
			candidates := []string{"start\tStart a new agent invocation", "ls\tList agent invocations"}
			invocations, directive := completeInvocationRefsForState(cmd, toComplete, "all")
			return append(candidates, invocations...), directive
		case 1:
			if args[0] == "start" || args[0] == "ls" {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			values := []string{"show", "check", "diff", "history", "open", "path", "shell", "attach", "clients", "stop", "kill", "land", "discard", "followup", "recreate", "restore"}
			candidates := make([]string, 0, len(values))
			for _, value := range values {
				if toComplete != "" && !strings.HasPrefix(value, toComplete) {
					continue
				}
				candidates = append(candidates, value)
			}
			return candidates, cobra.ShellCompDirectiveNoFileComp
		case 2:
			if args[1] != "history" {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			if toComplete != "" && !strings.HasPrefix("logs", toComplete) {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return []string{"logs"}, cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	return cmd
}
