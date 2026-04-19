package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newAgentStopCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "stop <invocation_id|prefix>",
		Short: "Stop an invocation gracefully",
		Long: `Send a graceful stop signal (Ctrl-C) to a running invocation.

This is the polite shutdown path. The runner may ignore the interrupt; use
"agency agent kill" when you need forceful termination.

Examples:
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
	cmd.GroupID = "finish"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
	return cmd
}

func newAgentKillCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "kill <invocation_id|prefix>",
		Short: "Kill an invocation forcefully",
		Long: `Forcefully terminate a running invocation.

The sandbox is preserved for inspection after termination.

Examples:
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
	cmd.GroupID = "finish"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
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

By default this cherry-picks sandbox commits onto the current integration
branch head. If the sandbox has no commits but still has uncommitted changes,
pass --apply.

Examples:
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
	cmd.GroupID = "finish"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply uncommitted changes (when no commits exist)")
	cmd.Flags().BoolVar(&requireBase, "require-base", false, "Fail if integration has diverged from base_commit")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
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

Examples:
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
	cmd.GroupID = "finish"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
	return cmd
}

func newAgentFollowupCmd() *cobra.Command {
	var repoRef string
	var prompt string
	var promptFile string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "followup <invocation_ref>",
		Short: "Send follow-up prompt to a headless invocation",
		Long: `Send a follow-up prompt to an existing headless invocation.

Use exactly one prompt source: --prompt or --prompt-file.

Examples:
  agency agent followup 20260131 --prompt "continue with test fixes"
  agency agent followup --repo agency my-invocation --prompt-file followup.md
  agency agent followup --json 20260131 --prompt "next step"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentFollowup(ctx, cr, fsys, cwd, commands.AgentFollowupOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				Prompt:        prompt,
				PromptFile:    promptFile,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "run"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Follow-up prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Path to file containing follow-up prompt")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	cmd.MarkFlagsMutuallyExclusive("prompt", "prompt-file")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
	return cmd
}

func newAgentRecreateCmd() *cobra.Command {
	var repoRef string
	var detached bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "recreate <invocation_ref>",
		Short: "Recreate a headed invocation's tmux session",
		Long: `Recreate a missing headed invocation tmux session.

This keeps the same invocation id and sandbox, starts the configured headed
runner in that sandbox, and attaches unless --detached or --json is used.

Examples:
  agency agent recreate 20260131
  agency agent recreate --repo agency my-invocation --detached
  agency agent recreate --json 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentRecreate(ctx, cr, fsys, cwd, commands.AgentRecreateOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				Detached:      detached,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "recover"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	cmd.Flags().BoolVar(&detached, "detached", false, "Recreate tmux session without attaching")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
	return cmd
}

func newAgentRestoreCmd() *cobra.Command {
	var repoRef string
	var checkpointID int
	var turnID string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "restore <invocation_ref>",
		Short: "Restore an invocation sandbox to a checkpoint",
		Long: `Restore a headless invocation sandbox to a previous checkpoint.

Use either:
  - --checkpoint <id> for explicit/scripted restore
  - --turn <entry_id> to restore the latest checkpoint at or before a history turn

Examples:
  agency agent restore 20260131 --checkpoint 3
  agency agent restore 20260131 --turn stream:9
  agency agent restore --repo agency my-invocation --checkpoint 7
  agency agent restore --json 20260131 --turn inv_event:2:agency.followup_prompt`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.AgentRestore(ctx, cr, fsys, cwd, commands.AgentRestoreOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				CheckpointID:  checkpointID,
				TurnID:        turnID,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "recover"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	cmd.Flags().IntVar(&checkpointID, "checkpoint", 0, "Checkpoint ID to restore")
	cmd.Flags().StringVar(&turnID, "turn", "", "History turn entry id to restore from")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	cmd.MarkFlagsMutuallyExclusive("checkpoint", "turn")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)

	return cmd
}
