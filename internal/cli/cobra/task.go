package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newTaskCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool
	var mode string
	var runner string
	var invocationName string
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
	startCmd := newTaskStartCmd()
	lsCmd := newTaskLSCmd()

	cmd := &cobra.Command{
		Use:   "task",
		Short: "Create and manage delegated tasks",
		Long: `Create and manage delegated tasks.

A task is the high-level delegation surface. Starting a task creates one
integration worktree and one primary agent invocation through a daemon-owned
mutation.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				_ = cmd.Help()
			}

			if err := validateTaskTargetFlags(cmd, args); err != nil {
				return err
			}
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}
			return commands.TaskTarget(ctx, cr, fsys, cwd, commands.TaskTargetOpts{
				Args:               args,
				RepoRef:            repoRef,
				JSON:               jsonOut,
				Mode:               mode,
				Runner:             runner,
				InvocationName:     invocationName,
				Detached:           detached,
				Prompt:             prompt,
				PromptFile:         promptFile,
				AgencyConfigPath:   agencyConfigPath,
				ExecutionProfile:   executionProfile,
				RunnerArgs:         runnerArgs,
				Model:              model,
				Effort:             effort,
				PermissionMode:     permissionMode,
				NoIncludeUntracked: noIncludeUntracked,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.AddGroup(
		&cobra.Group{ID: "run", Title: "Run"},
		&cobra.Group{ID: "inspect", Title: "Inspect"},
		&cobra.Group{ID: "finish", Title: "Finish"},
	)
	cmd.AddCommand(startCmd, lsCmd)
	cmd.PersistentFlags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	cmd.Flags().StringVar(&mode, "mode", "headless", "Retry mode (headless or headed)")
	cmd.Flags().StringVar(&runner, "runner", "", "Runner id to use for retry")
	cmd.Flags().StringVar(&invocationName, "name", "", "Optional retry invocation name")
	cmd.Flags().BoolVar(&detached, "detached", false, "Create headed tmux session but do not attach")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Inline headless retry prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Read headless retry prompt from this file")
	cmd.Flags().StringVar(&agencyConfigPath, "agency-config", "", "Load agency config from this file for retry")
	cmd.Flags().StringVar(&executionProfile, "execution-profile", "", "Execution profile override for retry")
	cmd.Flags().StringArrayVar(&runnerArgs, "runner-arg", nil, "Additional runner argument for retry (repeatable)")
	cmd.Flags().StringVar(&model, "model", "", "Runner model override for retry")
	cmd.Flags().StringVar(&effort, "effort", "", "Runner effort override for retry")
	cmd.Flags().StringVar(&permissionMode, "permission-mode", "", "Claude permission mode override for retry")
	cmd.Flags().BoolVar(&noIncludeUntracked, "no-include-untracked", false, "Exclude untracked files from headless checkpoint snapshots")
	cmd.MarkFlagsMutuallyExclusive("prompt", "prompt-file")
	lsCmd.MarkFlagsMutuallyExclusive("repo", "all-repos")
	registerRepoFlagCompletion(cmd)
	registerRunnerFlagCompletion(startCmd)
	registerRunnerFlagCompletion(cmd)
	_ = cmd.RegisterFlagCompletionFunc("mode", completeRunnerModes)

	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		for i, arg := range args {
			if arg != "--repo" {
				continue
			}
			if i == len(args)-1 {
				return completeRepoRefs(cmd, args, toComplete)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		switch len(args) {
		case 0:
			values := []string{"start", "ls"}
			return completeStaticValues(values, toComplete), cobra.ShellCompDirectiveNoFileComp
		case 1:
			return completeStaticValues(taskTargetActionCompletions(), toComplete), cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}
	return cmd
}

func taskTargetActionCompletions() []string {
	return []string{
		commands.TaskTargetActionArchive,
		commands.TaskTargetActionWatch,
		commands.TaskTargetActionRetry,
	}
}

func validateTaskTargetFlags(cmd *cobra.Command, args []string) error {
	targetFlags := []string{"json", "mode", "runner", "name", "detached", "prompt", "prompt-file", "agency-config", "execution-profile", "runner-arg", "model", "effort", "permission-mode", "no-include-untracked"}
	if policy, ok := commands.TaskTargetFlagPolicy(args); ok {
		return validateChangedTargetFlags(cmd, "task", policy.Action, targetFlags, policy.AllowedFlags...)
	}
	return nil
}

func newTaskStartCmd() *cobra.Command {
	var base string
	var jsonOut bool
	var runner string
	var mode string
	var invocationName string
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
		Use:     "start <name>",
		Short:   "Start a delegated task",
		GroupID: "run",
		Args: func(cmd *cobra.Command, args []string) error {
			switch len(args) {
			case 0:
				return errors.New(errors.EUsage, "use 'agency task start <name>'")
			case 1:
				return nil
			default:
				return errors.New(errors.EUsage, "too many arguments for \"agency task start\"")
			}
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
			return commands.TaskStart(ctx, cr, fsys, cwd, commands.TaskStartOpts{
				RepoRef:            repoRef,
				Name:               args[0],
				BaseBranch:         strings.TrimSpace(base),
				Mode:               strings.TrimSpace(mode),
				Runner:             runner,
				InvocationName:     invocationName,
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
	cmd.Flags().StringVar(&base, "base", "", "Base branch. Omit to use the current branch of the selected checkout.")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	cmd.Flags().StringVar(&runner, "runner", "", "Runner id to use")
	cmd.Flags().StringVar(&mode, "mode", "headless", "Task mode (headless or headed)")
	cmd.Flags().StringVar(&invocationName, "name", "", "Optional invocation name")
	cmd.Flags().BoolVar(&detached, "detached", false, "Create headed tmux session but do not attach")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Inline headless prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "Read headless prompt from this file")
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

func newTaskLSCmd() *cobra.Command {
	var allRepos bool
	var all bool
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "ls",
		Short:   "List tasks",
		GroupID: "inspect",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return errors.New(errors.EUsage, "too many arguments for \"agency task ls\"")
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
			return commands.TaskLS(ctx, cr, fsys, cwd, commands.TaskLSOpts{
				RepoRef:  repoRef,
				AllRepos: allRepos,
				All:      all,
				JSON:     jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
	}
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "List across all registered repos")
	cmd.Flags().BoolVar(&all, "all", false, "Include archived tasks")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	return cmd
}
