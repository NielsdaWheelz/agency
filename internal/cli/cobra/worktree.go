package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "worktree",
		Aliases: []string{"wt"},
		Short:   "Manage integration worktrees",
		Long: `Manage integration worktrees.

Integration worktrees are the long-lived branches you intend to merge, push,
rebase, and open pull requests from. They are separate from agent sandboxes.

Use:
  agency worktree create <worktree-name>
                           to make a new integration worktree
  agency worktree ls       to list worktrees
  agency worktree <worktree-ref>
                           to show one worktree
  agency worktree <worktree-ref> open
                           to open one worktree
  agency worktree <worktree-ref> pr sync
                           to push and sync one pull request`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				switch args[0] {
				case "create":
					return errors.New(errors.EUsage, "use 'agency worktree create <worktree-name>'")
				case "ls":
					return errors.New(errors.EUsage, "use 'agency worktree ls'")
				case "show", "path", "open", "shell", "rm", "pr", "rebase", "merge":
					return errors.New(errors.EUsage, "unknown command \""+args[0]+"\" for \"agency worktree\"")
				}
			}
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify 'create', 'ls', or a worktree ref")
		},
	}

	showCmd := newWorktreeShowCmd()
	showCmd.Hidden = true
	pathCmd := newWorktreePathCmd()
	pathCmd.Hidden = true
	openCmd := newWorktreeOpenCmd()
	openCmd.Hidden = true
	shellCmd := newWorktreeShellCmd()
	shellCmd.Hidden = true
	rmCmd := newWorktreeRmCmd()
	rmCmd.Hidden = true
	rebaseCmd := newWorktreeRebaseCmd()
	rebaseCmd.Hidden = true
	prSyncCmd := newWorktreePRSyncCmd()
	prSyncCmd.Hidden = true
	prMergeCmd := newWorktreePRMergeCmd()
	prMergeCmd.Hidden = true

	cmd.AddCommand(
		newWorktreeCreateCmd(),
		newWorktreeLSCmd(),
		showCmd,
		pathCmd,
		openCmd,
		shellCmd,
		rmCmd,
		rebaseCmd,
		prSyncCmd,
		prMergeCmd,
	)

	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		for i, arg := range args {
			if arg != "--repo" {
				continue
			}
			if i == len(args)-1 {
				return completeRepoRefs(cmd, args, toComplete)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		switch len(args) {
		case 0:
			candidates := []string{"create\tCreate a new integration worktree", "ls\tList integration worktrees"}
			worktrees, directive := completeWorktreeRefsForState(cmd, toComplete, "all")
			return append(candidates, worktrees...), directive
		case 1:
			if args[0] == "create" || args[0] == "ls" {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			values := []string{"path", "open", "shell", "rm", "rebase", "pr"}
			candidates := make([]string, 0, len(values))
			for _, value := range values {
				if toComplete != "" && !strings.HasPrefix(value, toComplete) {
					continue
				}
				candidates = append(candidates, value)
			}
			return candidates, cobra.ShellCompDirectiveNoFileComp
		case 2:
			if args[1] != "pr" {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			values := []string{"sync", "merge"}
			candidates := make([]string, 0, len(values))
			for _, value := range values {
				if toComplete != "" && !strings.HasPrefix(value, toComplete) {
					continue
				}
				candidates = append(candidates, value)
			}
			return candidates, cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	return cmd
}
