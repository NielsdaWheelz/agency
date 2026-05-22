package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newAgentCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool
	var prompt string
	var promptFile string
	var detached bool
	var turnID string
	var turnRange string
	var last bool
	var limit int
	var cursor string
	var kind string
	var follow bool
	var offset int64
	var checkpointID int
	var apply bool
	var requireBase bool
	startCmd := newAgentStartCmd()
	lsCmd := newAgentLSCmd()

	cmd := &cobra.Command{
		Use:   "agent [start|ls|<invocation-ref> [action]]",
		Short: "Manage agent invocations",
		Long: `Manage agent invocations.

An agent invocation runs a configured runner inside an isolated sandbox cloned
from an integration worktree. Invocations are the execution layer: they run the
model, stream logs, create checkpoints, and eventually land or discard work.

Use:
  agency agent start       to create a new sandbox from the current integration worktree
  agency agent start --worktree <worktree-ref>
                           to create a new sandbox from one worktree
  agency agent ls         to list invocations
  agency agent <invocation-ref>
                           to show one invocation
  agency agent <invocation-ref> history
                           to inspect one invocation timeline
  agency agent <invocation-ref> history logs
                           to inspect one invocation's raw logs
  agency agent <invocation-ref> clients
                           to inspect headed tmux clients
  agency agent <invocation-ref> land
                           to apply sandbox changes back to integration

Target action flags:
  history uses --json, --last, --limit, and --cursor.
  history logs uses --kind, --follow, and --offset.
  land uses --apply and --require-base.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				_ = cmd.Help()
			}

			if err := validateAgentTargetFlags(cmd, args); err != nil {
				return err
			}

			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentTarget(ctx, cr, fsys, cwd, commands.AgentTargetOpts{
				Args:         args,
				RepoRef:      repoRef,
				JSON:         jsonOut,
				Prompt:       prompt,
				PromptFile:   promptFile,
				Detached:     detached,
				TurnID:       turnID,
				TurnRange:    turnRange,
				Last:         last,
				Limit:        limit,
				Cursor:       cursor,
				Kind:         kind,
				Follow:       follow,
				Offset:       offset,
				CheckpointID: checkpointID,
				Apply:        apply,
				RequireBase:  requireBase,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.AddGroup(
		&cobra.Group{ID: "run", Title: "Run"},
		&cobra.Group{ID: "inspect", Title: "Inspect"},
		&cobra.Group{ID: "navigate", Title: "Navigate"},
		&cobra.Group{ID: "finish", Title: "Finish"},
		&cobra.Group{ID: "recover", Title: "Recover"},
	)

	cmd.AddCommand(startCmd, lsCmd)

	cmd.PersistentFlags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	cmd.Flags().BoolVar(&detached, "detached", false, "Create or recreate the tmux session but do not attach")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Inline prompt text for headless start or followup")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Read prompt text for headless start or followup from this file")
	cmd.Flags().StringVar(&turnID, "turn", "", "Timeline entry id to anchor diff context")
	cmd.Flags().StringVar(&turnRange, "turn-range", "", "Inclusive turn range (<start_entry_id>..<end_entry_id>)")
	cmd.Flags().BoolVar(&last, "last", false, "Show only the last timeline entry")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum entries to show")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Cursor for pagination")
	cmd.Flags().StringVar(&kind, "kind", "", "Log kind ("+strings.Join(daemon.InvocationLogKinds(), ", ")+")")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow log output")
	cmd.Flags().Int64Var(&offset, "offset", 0, "Starting byte offset")
	cmd.Flags().IntVar(&checkpointID, "checkpoint", 0, "Checkpoint ID to restore")
	cmd.Flags().BoolVar(&apply, "apply", false, "Land uncommitted sandbox changes by applying a patch")
	cmd.Flags().BoolVar(&requireBase, "require-base", false, "Fail if the integration worktree moved since the invocation base")
	cmd.MarkFlagsMutuallyExclusive("prompt", "prompt-file")
	cmd.MarkFlagsMutuallyExclusive("last", "cursor")
	lsCmd.MarkFlagsMutuallyExclusive("repo", "all-repos")
	registerRepoFlagCompletion(cmd)
	registerLogKindFlagCompletion(cmd)
	registerWorktreeFlagCompletion(startCmd, "present")
	registerWorktreeFlagCompletion(lsCmd, "present")
	registerRunnerFlagCompletion(startCmd)

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
			return completeInvocationRefsForState(cmd, toComplete, "all")
		case 1:
			return completeStaticValues(agentTargetActionCompletions(), toComplete), cobra.ShellCompDirectiveNoFileComp
		case 2:
			if args[1] != commands.AgentTargetActionHistory {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			candidates := completeStaticValues(agentTargetHistoryActionCompletions(), toComplete)
			if len(candidates) == 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return candidates, cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	return cmd
}

func agentTargetActionCompletions() []string {
	return []string{
		commands.AgentTargetActionCheck,
		commands.AgentTargetActionDiff,
		commands.AgentTargetActionHistory,
		commands.AgentTargetActionOpen,
		commands.AgentTargetActionPath,
		commands.AgentTargetActionShell,
		commands.AgentTargetActionAttach,
		commands.AgentTargetActionClients,
		commands.AgentTargetActionStop,
		commands.AgentTargetActionKill,
		commands.AgentTargetActionLand,
		commands.AgentTargetActionDiscard,
		commands.AgentTargetActionFollowup,
		commands.AgentTargetActionRecreate,
		commands.AgentTargetActionRestore,
	}
}

func agentTargetHistoryActionCompletions() []string {
	return []string{
		commands.AgentTargetHistoryActionLogs,
	}
}

func validateAgentTargetFlags(cmd *cobra.Command, args []string) error {
	targetFlags := []string{"json", "detached", "prompt", "prompt-file", "turn", "turn-range", "last", "limit", "cursor", "kind", "follow", "offset", "checkpoint", "apply", "require-base"}
	if policy, ok := commands.AgentTargetFlagPolicy(args); ok {
		return validateChangedTargetFlags(cmd, "agent", policy.Action, targetFlags, policy.AllowedFlags...)
	}
	return nil
}

func newAgentStartCmd() *cobra.Command {
	var worktreeRef string
	var jsonOut bool
	var runner string
	var mode string
	var name string
	var detached bool
	var prompt string
	var promptFile string
	var agencyConfigPath string
	var executionProfile string
	var runnerArgs []string
	var model string
	var effort string
	var permissionMode string
	var noIncludeUntracked bool

	cmd := &cobra.Command{
		Use:     "start",
		Short:   "Start a new agent invocation",
		GroupID: "run",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return errors.New(errors.EUsage, "too many arguments for \"agency agent start\"")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRef, err := cmd.Flags().GetString("repo")
			if err != nil {
				return err
			}

			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentStart(ctx, cr, fsys, cwd, commands.AgentStartOpts{
				RepoRef:            repoRef,
				WorktreeRef:        worktreeRef,
				Runner:             runner,
				Mode:               strings.TrimSpace(mode),
				InvocationName:     name,
				Detached:           detached,
				Prompt:             prompt,
				PromptFile:         promptFile,
				AgencyConfigPath:   agencyConfigPath,
				ExecutionProfile:   executionProfile,
				RunnerArgs:         runnerArgs,
				Model:              model,
				Effort:             effort,
				PermissionMode:     permissionMode,
				JSON:               jsonOut,
				NoIncludeUntracked: noIncludeUntracked,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
	}

	cmd.Flags().StringVar(&worktreeRef, "worktree", "", "Use this integration worktree instead of local/default resolution")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	cmd.Flags().StringVar(&runner, "runner", "", "Runner id to use")
	cmd.Flags().StringVar(&mode, "mode", "headed", "Agent mode (headed or headless)")
	cmd.Flags().StringVar(&name, "name", "", "Optional invocation name")
	cmd.Flags().BoolVar(&detached, "detached", false, "Create the tmux session but do not attach")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Inline headless prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Read the headless prompt from this file")
	cmd.Flags().StringVar(&agencyConfigPath, "agency-config", "", "Load agency config from this file")
	cmd.Flags().StringVar(&executionProfile, "execution-profile", "", "Execution profile override")
	cmd.Flags().StringArrayVar(&runnerArgs, "runner-arg", nil, "Additional runner argument (repeatable)")
	cmd.Flags().StringVar(&model, "model", "", "Runner model override")
	cmd.Flags().StringVar(&effort, "effort", "", "Runner effort override")
	cmd.Flags().StringVar(&permissionMode, "permission-mode", "", "Claude permission mode override")
	cmd.Flags().BoolVar(&noIncludeUntracked, "no-include-untracked", false, "Exclude untracked files from headless checkpoint snapshots")
	cmd.MarkFlagsMutuallyExclusive("prompt", "prompt-file")
	_ = cmd.RegisterFlagCompletionFunc("mode", completeRunnerModes)

	return cmd
}

func newAgentLSCmd() *cobra.Command {
	var allRepos bool
	var worktreeRef string
	var all bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:     "ls",
		Short:   "List agent invocations",
		GroupID: "inspect",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return errors.New(errors.EUsage, "too many arguments for \"agency agent ls\"")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRef, err := cmd.Flags().GetString("repo")
			if err != nil {
				return err
			}

			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentLS(ctx, cr, fsys, cwd, commands.AgentLSOpts{
				RepoRef:     repoRef,
				AllRepos:    allRepos,
				WorktreeRef: worktreeRef,
				All:         all,
				JSON:        jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
	}

	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "List across all registered repos")
	cmd.Flags().StringVar(&worktreeRef, "worktree", "", "Only show invocations for this integration worktree")
	cmd.Flags().BoolVar(&all, "all", false, "Include all invocations")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")

	return cmd
}
