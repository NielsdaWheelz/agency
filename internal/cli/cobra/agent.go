package cobra

import (
	"context"
	"os"

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
  stop      Stop an invocation gracefully (Ctrl-C)
  kill      Kill an invocation forcefully
  diff      Show sandbox changes vs integration
  land      Apply sandbox changes to integration
  discard   Discard sandbox changes
  open      Open sandbox in editor
  logs      View invocation logs (future)`,
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
	var noIncludeUntracked bool

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
  agency agent start --worktree my-feature --runner claude
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
				NoIncludeUntracked: noIncludeUntracked,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&worktree, "worktree", "", "Integration worktree to run against (required)")
	cmd.Flags().StringVar(&runner, "runner", "claude", "Runner to use (claude, codex)")
	cmd.Flags().BoolVar(&headless, "headless", false, "Run in headless mode (non-interactive)")
	cmd.Flags().StringVar(&name, "name", "", "Optional name for the invocation")
	cmd.Flags().BoolVar(&detached, "detached", false, "Start but do not attach (headed mode only)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt string for headless mode")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Path to file containing prompt for headless mode")
	cmd.Flags().StringArrayVar(&runnerArgs, "runner-arg", nil, "Additional argument to pass to the runner (repeatable)")
	cmd.Flags().BoolVar(&noIncludeUntracked, "no-include-untracked", false, "Exclude untracked files from checkpoint snapshots")

	return cmd
}

func newAgentLSCmd() *cobra.Command {
	var repoFlag string
	var allRepos bool
	var worktree string
	var all bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List agent invocations",
		Long: `List agent invocations for the current repository.

By default, shows active invocations (not yet landed/discarded).
Use --repo to specify a repo, or --all-repos to list globally.

Example:
  agency agent ls
  agency agent ls --repo abc123
  agency agent ls --all-repos
  agency agent ls --worktree my-feature
  agency agent ls --all --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Filter by repo id or unique prefix")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "List across all registered repos")
	cmd.Flags().StringVar(&worktree, "worktree", "", "Filter by integration worktree")
	cmd.Flags().BoolVar(&all, "all", false, "Include finished (landed/discarded) invocations")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

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

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func newAgentAttachCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "attach <invocation_id|prefix>",
		Short: "Attach to a running headed invocation",
		Long: `Attach to a running headed invocation's tmux session.

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

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")

	return cmd
}

func newAgentStopCmd() *cobra.Command {
	var repoFlag string

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
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")

	return cmd
}

func newAgentKillCmd() *cobra.Command {
	var repoFlag string

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
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")

	return cmd
}

func newAgentDiffCmd() *cobra.Command {
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "diff <invocation_ref>",
		Short: "Show sandbox changes vs integration",
		Long: `Show the diff between sandbox and the integration branch.

Displays:
- Commits in the sandbox (since base_commit)
- File changes between base_commit and sandbox tip
- Uncommitted changes in sandbox (if any)

Example:
  agency agent diff 20260131
  agency agent diff --repo abc123 my-invocation`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")

	return cmd
}

func newAgentLandCmd() *cobra.Command {
	var repoFlag string
	var apply bool
	var requireBase bool

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
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply uncommitted changes (when no commits exist)")
	cmd.Flags().BoolVar(&requireBase, "require-base", false, "Fail if integration has diverged from base_commit")

	return cmd
}

func newAgentDiscardCmd() *cobra.Command {
	var repoFlag string

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
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")

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

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repo id or unique prefix")

	return cmd
}
