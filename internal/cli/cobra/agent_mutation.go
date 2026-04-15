package cobra

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newAgentStopCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "stop <invocation_id|prefix>",
		Short: "Stop an invocation gracefully",
		Long: `Send a graceful stop signal (Ctrl-C) to a running invocation.

For headed invocations, this sends C-c via tmux send-keys.
The runner may ignore the signal; use 'kill' for forceful termination.

Example:
  agency agent stop 20260131
  agency agent stop --repo agency 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentStop(ctx, cr, fsys, cwd, commands.AgentStopOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	return cmd
}

func newAgentKillCmd() *cobra.Command {
	var repoRef string
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
  agency agent kill --repo agency 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentKill(ctx, cr, fsys, cwd, commands.AgentKillOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	return cmd
}

func newAgentLandCmd() *cobra.Command {
	var repoRef string
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
  agency agent land --repo agency my-invocation --apply
  agency agent land 20260131 --require-base`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentLand(ctx, cr, fsys, cwd, commands.AgentLandOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				Apply:         apply,
				RequireBase:   requireBase,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoRef, "repo", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply uncommitted changes (when no commits exist)")
	cmd.Flags().BoolVar(&requireBase, "require-base", false, "Fail if integration has diverged from base_commit")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}

func newAgentDiscardCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "discard <invocation_ref>",
		Short: "Discard sandbox changes",
		Long: `Discard a sandbox without landing its changes.

If the invocation is still running, it will be stopped first (gracefully,
then forcefully killed after 5 seconds).

Example:
  agency agent discard 20260131
  agency agent discard --repo agency my-invocation`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentDiscard(ctx, cr, fsys, cwd, commands.AgentDiscardOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	return cmd
}

func newAgentChatCmd() *cobra.Command {
	var repoRef string
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
  agency agent chat --repo agency my-invocation --prompt-file followup.md
  agency agent chat --json 20260131 --prompt "next step"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentChat(ctx, cr, fsys, cwd, commands.AgentChatOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				Prompt:        prompt,
				PromptFile:    promptFile,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoRef, "repo", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Follow-up prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Path to file containing follow-up prompt")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}

func newAgentRestartCmd() *cobra.Command {
	var repoRef string
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
  agency agent restart --repo agency my-invocation --checkpoint 7
  agency agent restart 20260131 --checkpoint 3 --runner-arg "--model=sonnet"
  agency agent restart 20260131 --checkpoint 3 --env FAKE_RUNNER_MODE=sleep
  agency agent restart --json 20260131 --checkpoint 3`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envMap, err := parseEnvAssignments(envAssignments)
			if err != nil {
				if jsonOut {
					return commands.WriteAgentMutationJSONError(cmd.OutOrStdout(), errors.New(errors.EUsage, err.Error()))
				}
				return err
			}

			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.AgentRestart(ctx, cr, fsys, cwd, commands.AgentRestartOpts{
				InvocationRef:      args[0],
				RepoRef:            repoRef,
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

	cmd.Flags().StringVar(&repoRef, "repo", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
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
