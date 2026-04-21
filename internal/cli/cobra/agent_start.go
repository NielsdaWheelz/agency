package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newAgentStartCmd() *cobra.Command {
	var repoRef string
	var worktreeRef string
	var runner string
	var headless bool
	var name string
	var detached bool
	var prompt string
	var promptFile string
	var agencyConfigPath string
	var runnerArgs []string
	var model string
	var effort string
	var permissionMode string
	var noIncludeUntracked bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new agent invocation",
		Long: `Start a new agent invocation in a sandbox.

The selected integration worktree is the source of truth for the sandbox.
Use --worktree to target a specific worktree from any cwd. If --worktree is
omitted, agency resolves the worktree from the active context first and then
falls back to the current integration worktree only when cwd is already inside
one.

Use --repo to scope worktree resolution when cwd does not already identify the
repo that owns that worktree. Pass both --repo and --worktree when you want an
explicit, scriptable command that works from any directory.

Headed mode is the default. It creates the sandbox, starts the runner in tmux,
and attaches unless you pass --detached. Headless mode runs through the daemon
and requires exactly one prompt source: --prompt or --prompt-file.

If --agency-config is relative, it is resolved from the current directory
before loading.

Examples:
  agency agent start
  agency agent start --worktree my-feature
  agency agent start --worktree my-feature --repo agency
  agency agent start --worktree my-feature --repo agency --runner codex
  agency agent start --worktree my-feature --repo agency --detached
  agency agent start --worktree my-feature --repo agency --name fix-auth
  agency agent start --worktree my-feature --repo agency --agency-config /path/to/agency.json
  agency context use my-feature --repo agency
  agency agent start --headless --prompt-file task.md
  agency agent start --headless --prompt "Fix the failing test"
  agency agent start --worktree my-feature --repo agency --headless --no-include-untracked`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.AgentStart(ctx, cr, fsys, cwd, commands.AgentStartOpts{
				RepoRef:            repoRef,
				WorktreeRef:        worktreeRef,
				Runner:             runner,
				Headless:           headless,
				InvocationName:     name,
				Detached:           detached,
				Prompt:             prompt,
				PromptFile:         promptFile,
				AgencyConfigPath:   agencyConfigPath,
				RunnerArgs:         runnerArgs,
				Model:              model,
				Effort:             effort,
				PermissionMode:     permissionMode,
				JSON:               jsonOut,
				NoIncludeUntracked: noIncludeUntracked,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "run"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Registered repo ref. Omit only when cwd already identifies the repo.")
	cmd.Flags().StringVar(&worktreeRef, "worktree", "", "Integration worktree ref to start from. Omit to use the active context or current integration worktree.")
	cmd.Flags().StringVar(&runner, "runner", "", "Runner id to use (claude-code, codex, amp, opencode, cursor, droid)")
	cmd.Flags().BoolVar(&headless, "headless", false, "Run through the daemon without tmux attachment")
	cmd.Flags().StringVar(&name, "name", "", "Optional invocation name")
	cmd.Flags().BoolVar(&detached, "detached", false, "Create the tmux session but do not attach")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Inline headless prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Read the headless prompt from this file")
	cmd.Flags().StringVar(&agencyConfigPath, "agency-config", "", "Load agency config from this file")
	cmd.Flags().StringArrayVar(&runnerArgs, "runner-arg", nil, "Additional runner argument (repeatable)")
	cmd.Flags().StringVar(&model, "model", "", "Runner model override")
	cmd.Flags().StringVar(&effort, "effort", "", "Runner effort override")
	cmd.Flags().StringVar(&permissionMode, "permission-mode", "", "Claude permission mode override")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	cmd.Flags().BoolVar(&noIncludeUntracked, "no-include-untracked", false, "Exclude untracked files from headless checkpoint snapshots")
	cmd.MarkFlagsMutuallyExclusive("prompt", "prompt-file")
	_ = cmd.MarkFlagFilename("prompt-file")
	_ = cmd.MarkFlagFilename("agency-config", "json")
	registerRepoFlagCompletion(cmd)
	registerWorktreeFlagCompletion(cmd, "present")
	registerRunnerFlagCompletion(cmd)

	return cmd
}
