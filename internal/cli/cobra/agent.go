package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
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
			switch {
			case len(args) == 0:
				_ = cmd.Help()
				return errors.New(errors.EUsage, "specify 'start', 'ls', or an invocation ref")
			default:
				invocationRef := args[0]
				switch {
				case len(args) == 1:
					if err := validateAgentTargetFlags(cmd, "<invocation-ref>", "json"); err != nil {
						return err
					}
					return runAgentShow(cmd, invocationRef, repoRef, jsonOut)
				case len(args) == 2:
					switch args[1] {
					case "check":
						if err := validateAgentTargetFlags(cmd, "check"); err != nil {
							return err
						}
						return runAgentCheck(cmd, invocationRef, repoRef)
					case "diff":
						if err := validateAgentTargetFlags(cmd, "diff", "json", "turn", "turn-range"); err != nil {
							return err
						}
						return runAgentDiff(cmd, invocationRef, repoRef, jsonOut, turnID, turnRange)
					case "history":
						if err := validateAgentTargetFlags(cmd, "history", "json", "last", "limit", "cursor"); err != nil {
							return err
						}
						return runAgentHistory(cmd, invocationRef, repoRef, jsonOut, last, limit, cursor)
					case "open":
						if err := validateAgentTargetFlags(cmd, "open"); err != nil {
							return err
						}
						return runAgentOpen(cmd, invocationRef, repoRef)
					case "path":
						if err := validateAgentTargetFlags(cmd, "path"); err != nil {
							return err
						}
						return runAgentPath(cmd, invocationRef, repoRef)
					case "shell":
						if err := validateAgentTargetFlags(cmd, "shell"); err != nil {
							return err
						}
						return runAgentShell(cmd, invocationRef, repoRef)
					case "attach":
						if err := validateAgentTargetFlags(cmd, "attach"); err != nil {
							return err
						}
						return runAgentAttach(cmd, invocationRef, repoRef)
					case "clients":
						if err := validateAgentTargetFlags(cmd, "clients"); err != nil {
							return err
						}
						return runAgentClients(cmd, invocationRef, repoRef)
					case "stop":
						if err := validateAgentTargetFlags(cmd, "stop", "json"); err != nil {
							return err
						}
						return runAgentStop(cmd, invocationRef, repoRef, jsonOut)
					case "kill":
						if err := validateAgentTargetFlags(cmd, "kill", "json"); err != nil {
							return err
						}
						return runAgentKill(cmd, invocationRef, repoRef, jsonOut)
					case "land":
						if err := validateAgentTargetFlags(cmd, "land", "json", "apply", "require-base"); err != nil {
							return err
						}
						return runAgentLand(cmd, invocationRef, repoRef, apply, requireBase, jsonOut)
					case "discard":
						if err := validateAgentTargetFlags(cmd, "discard", "json"); err != nil {
							return err
						}
						return runAgentDiscard(cmd, invocationRef, repoRef, jsonOut)
					case "followup":
						if err := validateAgentTargetFlags(cmd, "followup", "json", "prompt", "prompt-file"); err != nil {
							return err
						}
						return runAgentFollowup(cmd, invocationRef, repoRef, prompt, promptFile, jsonOut)
					case "recreate":
						if err := validateAgentTargetFlags(cmd, "recreate", "json", "detached"); err != nil {
							return err
						}
						return runAgentRecreate(cmd, invocationRef, repoRef, detached, jsonOut)
					case "restore":
						if err := validateAgentTargetFlags(cmd, "restore", "json", "checkpoint", "turn"); err != nil {
							return err
						}
						return runAgentRestore(cmd, invocationRef, repoRef, checkpointID, turnID, jsonOut)
					default:
						return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency agent\"")
					}
				case len(args) == 3 && args[1] == "history":
					if args[2] != "logs" {
						return errors.New(errors.EUsage, "unknown command \""+args[2]+"\" for \"agency agent "+invocationRef+" history\"")
					}
					if err := validateAgentTargetFlags(cmd, "history logs", "kind", "follow", "offset"); err != nil {
						return err
					}
					return runAgentHistoryLogs(cmd, invocationRef, repoRef, kind, follow, offset)
				default:
					return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency agent\"")
				}
			}
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
	cmd.Flags().StringVar(&kind, "kind", "", "Log kind (raw, stderr, stream, hooks, terminal)")
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

func validateAgentTargetFlags(cmd *cobra.Command, action string, allowed ...string) error {
	allowedFlags := make(map[string]bool, len(allowed))
	for _, flag := range allowed {
		allowedFlags[flag] = true
	}
	for _, flag := range []string{"json", "detached", "prompt", "prompt-file", "turn", "turn-range", "last", "limit", "cursor", "kind", "follow", "offset", "checkpoint", "apply", "require-base"} {
		if cmd.Flags().Changed(flag) && !allowedFlags[flag] {
			return errors.New(errors.EUsage, "--"+flag+" is not valid for agency agent "+action)
		}
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

func runAgentShow(cmd *cobra.Command, invocationRef string, repoRef string, jsonOut bool) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentShow(ctx, cr, fsys, cwd, commands.AgentShowOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
		JSON:          jsonOut,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentCheck(cmd *cobra.Command, invocationRef string, repoRef string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentCheck(ctx, cr, fsys, cwd, commands.AgentCheckOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentDiff(cmd *cobra.Command, invocationRef string, repoRef string, jsonOut bool, turnID string, turnRange string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentDiff(ctx, cr, fsys, cwd, commands.AgentDiffOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
		JSON:          jsonOut,
		TurnID:        turnID,
		TurnRange:     turnRange,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentHistory(cmd *cobra.Command, invocationRef string, repoRef string, jsonOut bool, last bool, limit int, cursor string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentHistory(ctx, cr, fsys, cwd, commands.AgentHistoryOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
		JSON:          jsonOut,
		Last:          last,
		Limit:         limit,
		Cursor:        cursor,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentHistoryLogs(cmd *cobra.Command, invocationRef string, repoRef string, kind string, follow bool, offset int64) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentHistoryLogs(ctx, cr, fsys, cwd, commands.AgentHistoryLogsOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
		Kind:          kind,
		Follow:        follow,
		Offset:        offset,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentOpen(cmd *cobra.Command, invocationRef string, repoRef string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentOpen(ctx, cr, fsys, cwd, commands.AgentOpenOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentPath(cmd *cobra.Command, invocationRef string, repoRef string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentPath(ctx, cr, fsys, cwd, commands.AgentPathOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentShell(cmd *cobra.Command, invocationRef string, repoRef string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentShell(ctx, cr, fsys, cwd, commands.AgentShellOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentAttach(cmd *cobra.Command, invocationRef string, repoRef string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentAttach(ctx, cr, fsys, cwd, commands.AgentAttachOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentClients(cmd *cobra.Command, invocationRef string, repoRef string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentClients(ctx, cr, fsys, cwd, commands.AgentClientsOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentStop(cmd *cobra.Command, invocationRef string, repoRef string, jsonOut bool) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentStop(ctx, cr, fsys, cwd, commands.AgentStopOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
		JSON:          jsonOut,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentKill(cmd *cobra.Command, invocationRef string, repoRef string, jsonOut bool) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentKill(ctx, cr, fsys, cwd, commands.AgentKillOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
		JSON:          jsonOut,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentLand(cmd *cobra.Command, invocationRef string, repoRef string, apply bool, requireBase bool, jsonOut bool) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentLand(ctx, cr, fsys, cwd, commands.AgentLandOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
		Apply:         apply,
		RequireBase:   requireBase,
		JSON:          jsonOut,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentDiscard(cmd *cobra.Command, invocationRef string, repoRef string, jsonOut bool) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentDiscard(ctx, cr, fsys, cwd, commands.AgentDiscardOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
		JSON:          jsonOut,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentFollowup(cmd *cobra.Command, invocationRef string, repoRef string, prompt string, promptFile string, jsonOut bool) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentFollowup(ctx, cr, fsys, cwd, commands.AgentFollowupOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
		Prompt:        prompt,
		PromptFile:    promptFile,
		JSON:          jsonOut,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentRecreate(cmd *cobra.Command, invocationRef string, repoRef string, detached bool, jsonOut bool) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.AgentRecreate(ctx, cr, fsys, cwd, commands.AgentRecreateOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
		Detached:      detached,
		JSON:          jsonOut,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runAgentRestore(cmd *cobra.Command, invocationRef string, repoRef string, checkpointID int, turnID string, jsonOut bool) error {
	ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
	if err != nil {
		return err
	}

	return commands.AgentRestore(ctx, cr, fsys, cwd, commands.AgentRestoreOpts{
		InvocationRef: invocationRef,
		RepoRef:       repoRef,
		CheckpointID:  checkpointID,
		TurnID:        turnID,
		JSON:          jsonOut,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}
