package cobra

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
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
  attach    Attach to a running headed invocation
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
  restart   Restart invocation from checkpoint
  history   Show unified invocation timeline
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
		newAgentAttachCmd(),
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
		newAgentRestartCmd(),
		newAgentHistoryCmd(),
		newAgentLogsCmd(),
		newAgentReviewCmd(),
	)

	return cmd
}

func newAgentStartCmd() *cobra.Command {
	var worktree string
	var runner string
	var headless bool
	var name string
	var detached bool
	var prompt string
	var promptFile string
	var runnerArgs []string
	var model string
	var effort string
	var noIncludeUntracked bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new agent invocation",
		Long: `Start a new agent invocation in a sandbox.

An agent invocation runs a runner (Claude, Codex, etc.) inside an isolated
sandbox worktree derived from the integration branch.

For headed mode (default): creates sandbox, launches tmux session, and attaches.
Use --detached to start without attaching.

For headless mode: creates sandbox and runs the runner via the daemon.
Headless mode requires a prompt (--prompt or --prompt-file).

Checkpoints are automatically created during headless execution. Use
--no-include-untracked to exclude untracked files from checkpoint snapshots.

Example:
  agency agent start --worktree my-feature
  agency agent start --worktree my-feature --runner claude-code
  agency agent start --worktree my-feature --detached
  agency agent start --worktree my-feature --name arch-agent
  agency agent start --worktree my-feature --headless --prompt "Fix the bug"
  agency agent start --worktree my-feature --headless --prompt-file task.md
  agency agent start --worktree my-feature --headless --no-include-untracked`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if worktree == "" {
				return errors.New(errors.EUsage, "--worktree is required")
			}

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentStart(ctx, cr, fsys, cwd, commands.AgentStartOpts{
				WorktreeRef:        worktree,
				Runner:             runner,
				Headless:           headless,
				InvocationName:     name,
				Detached:           detached,
				Prompt:             prompt,
				PromptFile:         promptFile,
				RunnerArgs:         runnerArgs,
				Model:              model,
				Effort:             effort,
				JSON:               jsonOut,
				NoIncludeUntracked: noIncludeUntracked,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&worktree, "worktree", "", "Integration worktree to run against (required)")
	cmd.Flags().StringVar(&runner, "runner", "", "Runner to use (defaults to config defaults.runner)")
	cmd.Flags().BoolVar(&headless, "headless", false, "Run in headless mode (non-interactive)")
	cmd.Flags().StringVar(&name, "name", "", "Optional name for the invocation")
	cmd.Flags().BoolVar(&detached, "detached", false, "Start but do not attach (headed mode only)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt string for headless mode")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Path to file containing prompt for headless mode")
	cmd.Flags().StringArrayVar(&runnerArgs, "runner-arg", nil, "Additional argument to pass to the runner (repeatable)")
	cmd.Flags().StringVar(&model, "model", "", "Model override (supported for runners claude-code, codex, cursor)")
	cmd.Flags().StringVar(&effort, "effort", "", "Effort override (claude-code: --effort, codex: model_reasoning_effort; cursor: choose thinking model via --model)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&noIncludeUntracked, "no-include-untracked", false, "Exclude untracked files from checkpoint snapshots")

	return cmd
}

func newAgentLSCmd() *cobra.Command {
	var repoFlag string
	var allRepos bool
	var worktree string
	var all bool
	var jsonOut bool
	var watch bool
	var intervalStr string

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List agent invocations",
		Long: `List agent invocations for the current repository.

By default, shows active invocations (not yet landed/discarded).
Use --repo to specify a repo, or --all-repos to list globally.

Use --watch to continuously redraw the list at a configurable interval.
--watch is incompatible with --json.

Example:
  agency agent ls
  agency agent ls --repo abc123
  agency agent ls --all-repos
  agency agent ls --worktree my-feature
  agency agent ls --all --json
  agency agent ls --watch
  agency agent ls --watch --interval 1s`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if watch && jsonOut {
				return errors.New(errors.EUsage, "--watch and --json cannot be used together")
			}

			var interval time.Duration
			if watch && intervalStr != "" {
				d, parseErr := parseWatchInterval(intervalStr)
				if parseErr != nil {
					return errors.New(errors.EInvalidArgument, parseErr.Error())
				}
				interval = d
			}

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentLS(ctx, cr, fsys, cwd, commands.AgentLSOpts{
				RepoFlag:    repoFlag,
				AllRepos:    allRepos,
				WorktreeRef: worktree,
				All:         all,
				JSON:        jsonOut,
				Watch:       watch,
				Interval:    interval,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Filter by repo name, key, id, or prefix")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "List across all registered repos")
	cmd.Flags().StringVar(&worktree, "worktree", "", "Filter by integration worktree")
	cmd.Flags().BoolVar(&all, "all", false, "Include finished (landed/discarded) invocations")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&watch, "watch", false, "Continuously redraw the list")
	cmd.Flags().StringVar(&intervalStr, "interval", "500ms", "Watch redraw interval (e.g. 500ms, 1s)")

	return cmd
}

func newAgentShowCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "show <invocation_id|prefix>",
		Short: "Show details of an invocation",
		Long: `Show details of an agent invocation.

The invocation can be specified by full ID or unique prefix.

Example:
  agency agent show 20260131
  agency agent show --repo abc123 20260131
  agency agent show --json 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentShow(ctx, cr, fsys, cwd, commands.AgentShowOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}

func newAgentAttachCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "attach <invocation_id|prefix>",
		Short: "Compatibility alias for 'agent enter'",
		Long: `Attach to a running headed invocation's tmux session.

This command is a compatibility alias for 'agency agent enter'.
Prefer 'agency agent enter' for canonical invocation navigation.

This is only supported for headed (interactive) invocations.
Detach from the session with Ctrl+b, d.

Example:
  agency agent attach 20260131
  agency agent attach --repo abc123 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentAttach(ctx, cr, fsys, cwd, commands.AgentAttachOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")

	return cmd
}

func newAgentStopCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "stop <invocation_id|prefix>",
		Short: "Stop an invocation gracefully",
		Long: `Send a graceful stop signal (Ctrl-C) to a running invocation.

For headed invocations, this sends C-c via tmux send-keys.
The runner may ignore the signal; use 'kill' for forceful termination.

Example:
  agency agent stop 20260131
  agency agent stop --repo abc123 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentStop(ctx, cr, fsys, cwd, commands.AgentStopOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}

func newAgentKillCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "kill <invocation_id|prefix>",
		Short: "Kill an invocation forcefully",
		Long: `Forcefully terminate a running invocation.

For headed invocations, this kills the tmux session.
The invocation is marked as failed with exit_reason="killed".
The sandbox is preserved for inspection.

Example:
  agency agent kill 20260131
  agency agent kill --repo abc123 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentKill(ctx, cr, fsys, cwd, commands.AgentKillOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}

func newAgentDiffCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool
	var turnID string
	var turnRange string

	cmd := &cobra.Command{
		Use:   "diff <invocation_ref>",
		Short: "Show sandbox changes vs integration",
		Long: `Show the diff between sandbox and the integration branch.

Displays:
- Commits in the sandbox (since base_commit)
- File changes between base_commit and sandbox tip
- Uncommitted changes in sandbox (if any)

Optionally anchor diff context to timeline turn selectors:
- --turn <entry_id> for a single turn
- --turn-range <start_entry_id>..<end_entry_id> for an inclusive turn range

Example:
  agency agent diff 20260131
  agency agent diff --repo abc123 my-invocation
  agency agent diff --turn inv_event:2:agency.followup_prompt 20260131
  agency agent diff --turn-range stream:4..stream:9 --json 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if turnID != "" && turnRange != "" {
				return errors.New(errors.EUsage, "use either --turn or --turn-range, not both")
			}

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentDiff(ctx, cr, fsys, cwd, commands.AgentDiffOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
				TurnID:        turnID,
				TurnRange:     turnRange,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	cmd.Flags().StringVar(&turnID, "turn", "", "Timeline entry id to anchor diff context")
	cmd.Flags().StringVar(&turnRange, "turn-range", "", "Inclusive turn range (<start_entry_id>..<end_entry_id>)")

	return cmd
}

func newAgentLandCmd() *cobra.Command {
	var repoFlag string
	var apply bool
	var requireBase bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "land <invocation_ref>",
		Short: "Apply sandbox changes to integration",
		Long: `Land sandbox changes into the integration worktree.

By default, cherry-picks sandbox commits onto the integration branch HEAD.
If the sandbox has no commits but has uncommitted changes, use --apply.

Example:
  agency agent land 20260131
  agency agent land --repo abc123 my-invocation --apply
  agency agent land 20260131 --require-base`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentLand(ctx, cr, fsys, cwd, commands.AgentLandOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				Apply:         apply,
				RequireBase:   requireBase,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply uncommitted changes (when no commits exist)")
	cmd.Flags().BoolVar(&requireBase, "require-base", false, "Fail if integration has diverged from base_commit")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func newAgentDiscardCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "discard <invocation_ref>",
		Short: "Discard sandbox changes",
		Long: `Discard a sandbox without landing its changes.

If the invocation is still running, it will be stopped first (gracefully,
then forcefully killed after 5 seconds).

Example:
  agency agent discard 20260131
  agency agent discard --repo abc123 my-invocation`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentDiscard(ctx, cr, fsys, cwd, commands.AgentDiscardOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}

func newAgentOpenCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "open <invocation_ref>",
		Short: "Open sandbox in editor",
		Long: `Open the sandbox directory in your configured editor.

Example:
  agency agent open 20260131
  agency agent open --repo abc123 my-invocation`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentOpen(ctx, cr, fsys, cwd, commands.AgentOpenOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")

	return cmd
}

func newAgentPathCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "path <invocation_ref>",
		Short: "Print sandbox path",
		Long: `Print the sandbox path for an agent invocation.

The path is resolved via the daemon and printed to stdout.

Example:
  agency agent path 20260131
  agency agent path --repo abc123 my-invocation
  cd $(agency agent path 20260131)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentPath(ctx, cr, fsys, cwd, commands.AgentPathOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")

	return cmd
}

func newAgentShellCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "shell <invocation_ref>",
		Short: "Open shell in sandbox",
		Long: `Open a login shell with the working directory set to the sandbox path.

The sandbox path is resolved via the daemon before shell dispatch.

Example:
  agency agent shell 20260131
  agency agent shell --repo abc123 my-invocation`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentShell(ctx, cr, fsys, cwd, commands.AgentShellOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo name, key, id, or prefix")

	return cmd
}

func newAgentEnterCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "enter <invocation_ref>",
		Short: "Attach to a running headed invocation",
		Long: `Attach to a running headed invocation's tmux session.

This is the canonical interactive navigation command for headed invocations.
Headless invocations are rejected. Detach from the session with Ctrl+b, d.

Example:
  agency agent enter 20260131
  agency agent enter --repo abc123 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentEnter(ctx, cr, fsys, cwd, commands.AgentEnterOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")

	return cmd
}

// watchIntervalMin/Max define valid bounds for --interval.
const (
	watchIntervalMin = 250 * time.Millisecond
	watchIntervalMax = 5 * time.Second
)

// parseWatchInterval parses and validates a --interval flag value.
func parseWatchInterval(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("--interval: %q is not a valid duration (use e.g. 500ms, 1s, 2.5s)", s)
	}
	if d < watchIntervalMin {
		return 0, fmt.Errorf("--interval must be between 250ms and 5s")
	}
	if d > watchIntervalMax {
		return 0, fmt.Errorf("--interval must be between 250ms and 5s")
	}
	return d, nil
}

func newAgentChatCmd() *cobra.Command {
	var repoFlag string
	var prompt string
	var promptFile string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "chat <invocation_ref>",
		Short: "Send follow-up prompt to a headless invocation",
		Long: `Send a follow-up prompt to an existing headless invocation.

Use either --prompt or --prompt-file. The request is idempotent at the
daemon control plane using client_request_id semantics.

Example:
  agency agent chat 20260131 --prompt "continue with test fixes"
  agency agent chat --repo abc123 my-invocation --prompt-file followup.md
  agency agent chat --json 20260131 --prompt "next step"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentChat(ctx, cr, fsys, cwd, commands.AgentChatOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				Prompt:        prompt,
				PromptFile:    promptFile,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo name, key, id, or prefix")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Follow-up prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Path to file containing follow-up prompt")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func newAgentRestartCmd() *cobra.Command {
	var repoFlag string
	var checkpointID int
	var historySelector bool
	var runnerArgs []string
	var model string
	var effort string
	var envAssignments []string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "restart <invocation_ref>",
		Short: "Restart headless invocation from checkpoint/history",
		Long: `Restart a headless invocation in one flow.

This command performs checkpoint restore and runner restart as a single
invocation-scoped operation.

Use either:
  - --checkpoint <id> for explicit/scripted restart
  - --history for interactive arrow-key selection over timeline history

Deterministic mapping rule for --history:
  the selected timeline entry resolves to the latest checkpoint_event at or before
  that entry. If no valid checkpoint mapping exists, the command fails with a
  deterministic error and guidance.

Example:
  agency agent restart 20260131 --checkpoint 3
  agency agent restart 20260131 --history
  agency agent restart --repo abc123 my-invocation --checkpoint 7
  agency agent restart 20260131 --checkpoint 3 --runner-arg "--model=sonnet"
  agency agent restart 20260131 --checkpoint 3 --env FAKE_RUNNER_MODE=sleep
  agency agent restart --json 20260131 --checkpoint 3`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			writeJSONValidationFailure := func(err error) error {
				if err == nil || !jsonOut {
					return err
				}
				return commands.WriteAgentMutationJSONError(cmd.OutOrStdout(), err)
			}

			checkpointProvided := cmd.Flags().Changed("checkpoint")
			if historySelector && checkpointProvided {
				return writeJSONValidationFailure(errors.New(errors.EUsage, "use either --checkpoint or --history, not both"))
			}
			if !historySelector && !checkpointProvided {
				return writeJSONValidationFailure(errors.New(errors.EUsage, "either --checkpoint <id> or --history is required"))
			}
			if checkpointProvided && checkpointID <= 0 {
				return writeJSONValidationFailure(errors.New(errors.EUsage, "--checkpoint must be a positive integer"))
			}

			envMap, err := parseEnvAssignments(envAssignments)
			if err != nil {
				return writeJSONValidationFailure(errors.New(errors.EUsage, err.Error()))
			}

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentRestart(ctx, cr, fsys, cwd, commands.AgentRestartOpts{
				InvocationRef:      args[0],
				RepoFlag:           repoFlag,
				CheckpointID:       checkpointID,
				InteractiveHistory: historySelector,
				RunnerArgs:         runnerArgs,
				Model:              model,
				Effort:             effort,
				Env:                envMap,
				JSON:               jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo name, key, id, or prefix")
	cmd.Flags().IntVar(&checkpointID, "checkpoint", 0, "Checkpoint ID to restore")
	cmd.Flags().BoolVar(&historySelector, "history", false, "Select timeline history interactively (arrow keys)")
	cmd.Flags().StringArrayVar(&runnerArgs, "runner-arg", nil, "Additional argument to pass to restarted runner (repeatable)")
	cmd.Flags().StringVar(&model, "model", "", "Model override for restart (supported for runners claude-code, codex, cursor)")
	cmd.Flags().StringVar(&effort, "effort", "", "Effort override for restart (claude-code: --effort, codex: model_reasoning_effort; cursor: choose thinking model via --model)")
	cmd.Flags().StringArrayVar(&envAssignments, "env", nil, "Environment override KEY=VALUE for restart (repeatable)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func parseEnvAssignments(assignments []string) (map[string]string, error) {
	if len(assignments) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(assignments))
	for _, assignment := range assignments {
		key, value, ok := strings.Cut(assignment, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid --env value %q (expected KEY=VALUE)", assignment)
		}
		env[key] = value
	}
	return env, nil
}

func newAgentHistoryCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool
	var last bool
	var limit int
	var cursor string

	cmd := &cobra.Command{
		Use:   "history <invocation_ref>",
		Short: "Show unified invocation timeline",
		Long: `Show a rich transcript of the agent's conversation for an invocation.

By default, renders a human-readable transcript with styled output showing
assistant messages, tool use, prompts, and results. For runners with semantic
adapters (Claude, Codex, Cursor), the transcript includes full content blocks with
tool inputs and results. For other runners, falls back to a sparse timeline.

Use --last to show only the most recent timeline entry.
Use --json for machine-readable output with full content_blocks data.
Use --limit and --cursor for stable paginated reads.

Example:
  agency agent history 20260131
  agency agent history --last 20260131
  agency agent history --repo abc123 my-invocation
  agency agent history --limit 50 --cursor <opaque>
  agency agent history --json 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentHistory(ctx, cr, fsys, cwd, commands.AgentHistoryOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
				Last:          last,
				Limit:         limit,
				Cursor:        cursor,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&last, "last", false, "Show only the most recent timeline entry")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum entries to return (1-500)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from previous response")

	return cmd
}

func newAgentLogsCmd() *cobra.Command {
	var repoFlag string
	var kind string
	var follow bool
	var offset int64

	cmd := &cobra.Command{
		Use:   "logs <invocation_ref>",
		Short: "View invocation logs",
		Long: `View logs for an agent invocation.

Streams log content from the daemon using offset-based reads.
By default, reads all available log data and exits.

With --follow, continues polling for new data after reaching EOF
(like tail -f). Exit with Ctrl-C.

Use --kind to select log stream:
  raw     Verbatim runner stdout (default)
  stderr  Runner stderr
  stream  Normalized events (if available)

Example:
  agency agent logs 20260131
  agency agent logs --follow my-invocation
  agency agent logs --kind stderr 20260131
  agency agent logs --offset 1024 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentLogs(ctx, cr, fsys, cwd, commands.AgentLogsOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				Kind:          kind,
				Follow:        follow,
				Offset:        offset,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo name, key, id, or prefix")
	cmd.Flags().StringVar(&kind, "kind", "raw", "Log kind: raw, stderr, stream")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow log output (poll for new data)")
	cmd.Flags().Int64Var(&offset, "offset", 0, "Byte offset to start reading from")

	return cmd
}

func newAgentReviewCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "review <invocation_ref>",
		Short: "Show review/readiness for progression",
		Long: `Show the canonical review surface for an invocation.

This command reports one deterministic review verdict, typed blocking reasons,
PR sync eligibility, and navigation context back to history/diff.

Example:
  agency agent review 20260131
  agency agent review --repo abc123 my-invocation
  agency agent review --json 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.AgentReview(ctx, cr, fsys, cwd, commands.AgentReviewOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}
