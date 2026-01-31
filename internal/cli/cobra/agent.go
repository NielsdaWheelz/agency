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
  attach    Attach to a running invocation (future)
  stop      Stop an invocation gracefully (future)
  kill      Kill an invocation forcefully (future)
  diff      Show sandbox changes (future)
  land      Apply sandbox changes to integration (future)
  discard   Discard sandbox changes (future)
  open      Open sandbox in editor (future)
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
	)

	return cmd
}

func newAgentStartCmd() *cobra.Command {
	var worktree string
	var runner string
	var headless bool
	var name string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new agent invocation",
		Long: `Start a new agent invocation in a sandbox.

An agent invocation runs a runner (Claude, Codex, etc.) inside an isolated
sandbox worktree derived from the integration branch.

This creates the sandbox and invocation record but does NOT execute the runner
(runner execution will be added in a future PR).

Example:
  agency agent start --worktree my-feature
  agency agent start --worktree my-feature --runner claude --headless
  agency agent start --worktree my-feature --name arch-agent`,
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
				WorktreeRef:    worktree,
				Runner:         runner,
				Headless:       headless,
				InvocationName: name,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&worktree, "worktree", "", "Integration worktree to run against (required)")
	cmd.Flags().StringVar(&runner, "runner", "claude", "Runner to use (claude, codex)")
	cmd.Flags().BoolVar(&headless, "headless", false, "Run in headless mode (non-interactive)")
	cmd.Flags().StringVar(&name, "name", "", "Optional name for the invocation")

	return cmd
}

func newAgentLSCmd() *cobra.Command {
	var worktree string
	var all bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List agent invocations",
		Long: `List agent invocations for the current repository.

By default, shows active invocations (not yet landed/discarded).
Use --all to include finished invocations.
Use --worktree to filter by integration worktree.

Example:
  agency agent ls
  agency agent ls --worktree my-feature
  agency agent ls --all
  agency agent ls --json`,
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
				WorktreeRef: worktree,
				All:         all,
				JSON:        jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&worktree, "worktree", "", "Filter by integration worktree")
	cmd.Flags().BoolVar(&all, "all", false, "Include finished (landed/discarded) invocations")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func newAgentShowCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "show <invocation_id|prefix>",
		Short: "Show details of an invocation",
		Long: `Show details of an agent invocation.

The invocation can be specified by full ID or unique prefix.

Example:
  agency agent show 20260131
  agency agent show 20260131120500-a3f2
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
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}
