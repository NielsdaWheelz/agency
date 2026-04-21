package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newAgentStopCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:     "<invocation-ref> stop",
		Aliases: []string{"_stop"},
		Short:   "Stop an invocation gracefully",
		Long: `Send a graceful stop signal (Ctrl-C) to a running invocation.

This is the polite shutdown path. The runner may ignore the interrupt; use
'agency agent <invocation-ref> kill' when you need forceful termination.

Examples:
  agency agent 20260131 stop
  agency agent 20260131 stop --repo agency`,
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
		Use:     "<invocation-ref> kill",
		Aliases: []string{"_kill"},
		Short:   "Kill an invocation forcefully",
		Long: `Forcefully terminate a running invocation.

The sandbox is preserved for inspection after termination.

Examples:
  agency agent 20260131 kill
  agency agent 20260131 kill --repo agency`,
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
		Use:     "<invocation-ref> land",
		Aliases: []string{"_land"},
		Short:   "Apply sandbox changes to integration",
		Long: `Land sandbox changes into the integration worktree.

By default this cherry-picks sandbox commits onto the current integration
branch head. If the sandbox has no commits but still has uncommitted changes,
pass --apply.

Examples:
  agency agent 20260131 land
  agency agent my-invocation land --repo agency --apply
  agency agent 20260131 land --require-base`,
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
		Use:     "<invocation-ref> discard",
		Aliases: []string{"_discard"},
		Short:   "Discard sandbox changes",
		Long: `Discard a sandbox without landing its changes.

If the invocation is still running, it will be stopped first (gracefully,
then forcefully killed after 5 seconds).

Examples:
  agency agent 20260131 discard
  agency agent my-invocation discard --repo agency`,
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
		Use:     "<invocation-ref> followup",
		Aliases: []string{"_followup"},
		Short:   "Send follow-up prompt to a headless invocation",
		Long: `Send a follow-up prompt to an existing headless invocation.

Use exactly one prompt source: --prompt or --prompt-file.

Examples:
  agency agent 20260131 followup --prompt "continue with test fixes"
  agency agent my-invocation followup --repo agency --prompt-file followup.md
  agency agent 20260131 followup --json --prompt "next step"`,
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
		Use:     "<invocation-ref> recreate",
		Aliases: []string{"_recreate"},
		Short:   "Recreate a headed invocation's tmux session",
		Long: `Recreate a missing headed invocation tmux session.

This keeps the same invocation id and sandbox, starts the configured headed
runner in that sandbox, and attaches unless --detached or --json is used.

Examples:
  agency agent 20260131 recreate
  agency agent my-invocation recreate --repo agency --detached
  agency agent 20260131 recreate --json`,
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
		Use:     "<invocation-ref> restore",
		Aliases: []string{"_restore"},
		Short:   "Restore an invocation sandbox to a checkpoint",
		Long: `Restore a headless invocation sandbox to a previous checkpoint.

Use either:
  - --checkpoint <id> for explicit/scripted restore
  - --turn <entry_id> to restore the latest checkpoint at or before a history turn

Examples:
  agency agent 20260131 restore --checkpoint 3
  agency agent 20260131 restore --turn stream:9
  agency agent my-invocation restore --repo agency --checkpoint 7
  agency agent 20260131 restore --json --turn inv_event:2:agency.followup_prompt`,
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
