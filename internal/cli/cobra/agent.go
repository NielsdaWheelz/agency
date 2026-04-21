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
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				switch args[0] {
				case "start":
					return errors.New(errors.EUsage, "use 'agency agent start' or 'agency agent start --worktree <worktree-ref>'")
				case "ls":
					return errors.New(errors.EUsage, "use 'agency agent ls'")
				case "show", "check", "diff", "history", "open", "path", "shell", "attach", "clients", "stop", "kill", "land", "discard", "followup", "recreate", "restore", "logs", "checkpoint", "restart":
					return errors.New(errors.EUsage, "unknown command \""+args[0]+"\" for \"agency agent\"")
				}
			}
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify 'start', 'ls', or an invocation ref")
		},
	}

	cmd.AddGroup(
		&cobra.Group{ID: "run", Title: "Run"},
		&cobra.Group{ID: "inspect", Title: "Inspect"},
		&cobra.Group{ID: "navigate", Title: "Navigate"},
		&cobra.Group{ID: "finish", Title: "Finish"},
		&cobra.Group{ID: "recover", Title: "Recover"},
	)

	showCmd := newAgentShowCmd()
	showCmd.Hidden = true
	checkCmd := newAgentCheckCmd()
	checkCmd.Hidden = true
	diffCmd := newAgentDiffCmd()
	diffCmd.Hidden = true
	historyCmd := newAgentHistoryCmd()
	historyCmd.Hidden = true
	historyLogsCmd := newAgentHistoryLogsCmd()
	historyLogsCmd.Hidden = true
	openCmd := newAgentOpenCmd()
	openCmd.Hidden = true
	pathCmd := newAgentPathCmd()
	pathCmd.Hidden = true
	shellCmd := newAgentShellCmd()
	shellCmd.Hidden = true
	attachCmd := newAgentAttachCmd()
	attachCmd.Hidden = true
	clientsCmd := newAgentClientsCmd()
	clientsCmd.Hidden = true
	stopCmd := newAgentStopCmd()
	stopCmd.Hidden = true
	killCmd := newAgentKillCmd()
	killCmd.Hidden = true
	landCmd := newAgentLandCmd()
	landCmd.Hidden = true
	discardCmd := newAgentDiscardCmd()
	discardCmd.Hidden = true
	followupCmd := newAgentFollowupCmd()
	followupCmd.Hidden = true
	recreateCmd := newAgentRecreateCmd()
	recreateCmd.Hidden = true
	restoreCmd := newAgentRestoreCmd()
	restoreCmd.Hidden = true

	cmd.AddCommand(
		newAgentStartCmd(),
		newAgentLSCmd(),
		showCmd,
		checkCmd,
		diffCmd,
		historyCmd,
		historyLogsCmd,
		openCmd,
		pathCmd,
		shellCmd,
		attachCmd,
		clientsCmd,
		stopCmd,
		killCmd,
		landCmd,
		discardCmd,
		followupCmd,
		recreateCmd,
		restoreCmd,
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
			values := []string{"check", "diff", "history", "open", "path", "shell", "attach", "clients", "stop", "kill", "land", "discard", "followup", "recreate", "restore"}
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
