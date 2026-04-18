package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newAgentStartCmd() *cobra.Command {
	var repoRef string
	var worktree string
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
		Use:   "start",
		Short: "Start a new agent invocation",
		Long: `Start a new agent invocation in a sandbox.

An agent invocation runs a runner (Claude, Codex, etc.) inside an isolated
sandbox worktree derived from the integration branch.

For headed mode (default): requires an interactive terminal, creates the sandbox,
launches the tmux session, and attaches.
Use --detached to start without attaching.

For headless mode: creates sandbox and runs the runner via the daemon.
Headless mode requires a prompt (--prompt or --prompt-file).

Checkpoints are automatically created during headless execution. Use
--no-include-untracked to exclude untracked files from checkpoint snapshots.

Example:
  agency agent start --repo agency --worktree my-feature
  agency agent start --worktree my-feature
  agency agent start
  agency agent start --repo agency --worktree my-feature --runner claude-code
  agency agent start --repo agency --worktree my-feature --detached
  agency agent start --repo agency --worktree my-feature --name arch-agent
  agency agent start --repo agency --worktree my-feature --agency-config /path/to/agency.json
  agency agent start --repo agency --worktree my-feature --headless --prompt "Fix the bug"
  agency agent start --repo agency --worktree my-feature --headless --prompt-file task.md
  agency agent start --repo agency --worktree my-feature --headless --no-include-untracked`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
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

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo name, key, id, or prefix (defaults to current directory)")
	cmd.Flags().StringVar(&worktree, "worktree", "", "Integration worktree to run against (defaults to current integration worktree)")
	cmd.Flags().StringVar(&runner, "runner", "", "Runner to use (defaults to config defaults.runner)")
	cmd.Flags().BoolVar(&headless, "headless", false, "Run in headless mode (non-interactive)")
	cmd.Flags().StringVar(&name, "name", "", "Optional name for the invocation")
	cmd.Flags().BoolVar(&detached, "detached", false, "Skip attach in headed mode")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt string for headless mode")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Path to file containing prompt for headless mode")
	cmd.Flags().StringVar(&agencyConfigPath, "agency-config", "", "load agency config from this file")
	cmd.Flags().StringArrayVar(&runnerArgs, "runner-arg", nil, "Additional argument to pass to the runner (repeatable)")
	cmd.Flags().StringVar(&model, "model", "", "Model override (otherwise uses runner_defaults from agency config, then user config)")
	cmd.Flags().StringVar(&effort, "effort", "", "Effort override (otherwise uses runner_defaults from agency config, then user config)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&noIncludeUntracked, "no-include-untracked", false, "Exclude untracked files from checkpoint snapshots")

	return cmd
}
