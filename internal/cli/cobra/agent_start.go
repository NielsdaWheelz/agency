package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newAgentStartCmd() *cobra.Command {
	var repoRef string
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
	var noIncludeUntracked bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "start [<worktree-ref>]",
		Short: "Start a new agent invocation",
		Long: `Start a new agent invocation in a sandbox.

The selected integration worktree is the source of truth for the sandbox.
Use --repo to target a registered repo from any cwd. Pass both --repo and
the worktree ref when you want an explicit, scriptable command that works from
any directory.

Omitting the worktree ref only works when cwd is already inside the integration
worktree you want to use. If you are somewhere else, pass the worktree ref
explicitly.

Headed mode is the default. It creates the sandbox, starts the runner in tmux,
and attaches unless you pass --detached. Headless mode runs through the daemon
and requires exactly one prompt source: --prompt or --prompt-file.

If --agency-config is relative, it is resolved from the current directory
before loading.

Examples:
  agency agent start my-feature --repo agency
  agency agent start my-feature --repo agency --runner codex
  agency agent start my-feature --repo agency --detached
  agency agent start my-feature --repo agency --name fix-auth
  agency agent start my-feature --repo agency --agency-config /path/to/agency.json
  agency agent start --headless --prompt-file task.md
  agency agent start my-feature --repo agency --headless --prompt "Fix the failing test"
  agency agent start my-feature --repo agency --headless --no-include-untracked`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			worktree := ""
			if len(args) == 1 {
				worktree = args[0]
			}

			return commands.AgentStart(ctx, cr, fsys, cwd, commands.AgentStartOpts{
				RepoRef:            repoRef,
				WorktreeRef:        worktree,
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
				JSON:               jsonOut,
				NoIncludeUntracked: noIncludeUntracked,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "run"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Registered repo ref. Omit only when cwd already identifies the repo.")
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
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	cmd.Flags().BoolVar(&noIncludeUntracked, "no-include-untracked", false, "Exclude untracked files from headless checkpoint snapshots")
	cmd.MarkFlagsMutuallyExclusive("prompt", "prompt-file")
	_ = cmd.MarkFlagFilename("prompt-file")
	_ = cmd.MarkFlagFilename("agency-config", "json")
	registerRepoFlagCompletion(cmd)
	registerRunnerFlagCompletion(cmd)
	setWorktreeArgCompletion(cmd, "present")

	return cmd
}
