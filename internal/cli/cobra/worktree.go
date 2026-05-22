package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newWorktreeCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool
	var editor string
	var force bool
	var yes bool
	var allowDirty bool
	var forceWithLease bool
	var squash, merge, rebase bool
	var noDeleteBranch bool
	var agencyConfigPath string
	createCmd := newWorktreeCreateCmd()
	lsCmd := newWorktreeLSCmd()

	cmd := &cobra.Command{
		Use:   "worktree [create|ls|<worktree-ref> [action]]",
		Short: "Manage integration worktrees",
		Long: `Manage integration worktrees.

Integration worktrees are the long-lived branches you intend to merge, push,
rebase, and open pull requests from. They are separate from agent sandboxes.

Use:
  agency worktree create <worktree-name>
                           to make a new integration worktree
  agency worktree ls       to list worktrees
  agency worktree <worktree-ref>
                           to show one worktree
  agency worktree <worktree-ref> path
                           to print one worktree path
  agency worktree <worktree-ref> open
                           to open one worktree
  agency worktree <worktree-ref> shell
                           to open a shell in one worktree
  agency worktree <worktree-ref> pr sync
                           to push and sync one pull request
  agency worktree <worktree-ref> pr merge
                           to verify, merge, and archive one pull request

Target action flags:
  pr sync uses --allow-dirty and --force-with-lease.
  pr merge uses --squash, --merge, --rebase, --no-delete-branch, --yes, and --agency-config.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				_ = cmd.Help()
			}

			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}
			if err := validateWorktreeTargetFlags(cmd, args); err != nil {
				return err
			}

			return commands.WorktreeTarget(ctx, cr, fsys, cwd, commands.WorktreeTargetOpts{
				Args:             args,
				RepoRef:          repoRef,
				JSON:             jsonOut,
				Editor:           editor,
				Force:            force,
				Yes:              yes,
				AllowDirty:       allowDirty,
				ForceWithLease:   forceWithLease,
				Strategy:         resolveMergeStrategy(squash, merge, rebase),
				NoDeleteBranch:   noDeleteBranch,
				AgencyConfigPath: agencyConfigPath,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.AddGroup(
		&cobra.Group{ID: "run", Title: "Run"},
		&cobra.Group{ID: "inspect", Title: "Inspect"},
	)

	cmd.AddCommand(createCmd, lsCmd)

	cmd.PersistentFlags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	cmd.Flags().StringVar(&editor, "editor", "", "Editor to use")
	cmd.Flags().BoolVar(&force, "force", false, "Force removal even if worktree has uncommitted changes")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm destructive or non-interactive actions without prompting")
	cmd.Flags().BoolVar(&allowDirty, "allow-dirty", false, "Allow sync with dirty integration worktree")
	cmd.Flags().BoolVar(&forceWithLease, "force-with-lease", false, "Use git push --force-with-lease")
	cmd.Flags().BoolVar(&squash, "squash", false, "Use squash merge strategy (default)")
	cmd.Flags().BoolVar(&merge, "merge", false, "Use regular merge strategy")
	cmd.Flags().BoolVar(&rebase, "rebase", false, "Use rebase merge strategy")
	cmd.Flags().BoolVar(&noDeleteBranch, "no-delete-branch", false, "Preserve remote branch after merge")
	cmd.Flags().StringVar(&agencyConfigPath, "agency-config", "", "Load agency config from this file")
	cmd.MarkFlagsMutuallyExclusive("squash", "merge", "rebase")
	lsCmd.MarkFlagsMutuallyExclusive("repo", "all-repos")
	registerRepoFlagCompletion(cmd)

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
			return completeWorktreeRefsForState(cmd, toComplete, "all")
		case 1:
			return completeStaticValues(worktreeTargetActionCompletions(), toComplete), cobra.ShellCompDirectiveNoFileComp
		case 2:
			if args[1] != commands.WorktreeTargetActionPR {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeStaticValues(worktreeTargetPRActionCompletions(), toComplete), cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	return cmd
}

// resolveMergeStrategy maps cobra's mutually-exclusive --squash/--merge/--rebase
// flags into the daemon's strategy string. "" means "use the daemon default".
func resolveMergeStrategy(squash, merge, rebase bool) string {
	switch {
	case merge:
		return "merge"
	case rebase:
		return "rebase"
	case squash:
		return "squash"
	}
	return ""
}

func worktreeTargetActionCompletions() []string {
	return []string{
		commands.WorktreeTargetActionPath,
		commands.WorktreeTargetActionOpen,
		commands.WorktreeTargetActionShell,
		commands.WorktreeTargetActionRm,
		commands.WorktreeTargetActionRebase,
		commands.WorktreeTargetActionPR,
	}
}

func worktreeTargetPRActionCompletions() []string {
	return []string{
		commands.WorktreeTargetPRActionSync,
		commands.WorktreeTargetPRActionMerge,
	}
}

func validateWorktreeTargetFlags(cmd *cobra.Command, args []string) error {
	targetFlags := []string{
		"json",
		"editor",
		"force",
		"yes",
		"allow-dirty",
		"force-with-lease",
		"squash",
		"merge",
		"rebase",
		"no-delete-branch",
		"agency-config",
	}
	if policy, ok := commands.WorktreeTargetFlagPolicy(args); ok {
		return validateChangedTargetFlags(cmd, "worktree", policy.Action, targetFlags, policy.AllowedFlags...)
	}
	return nil
}

func newWorktreeCreateCmd() *cobra.Command {
	var base string
	var open bool
	var editor string

	cmd := &cobra.Command{
		Use:     "create <worktree-name>",
		Short:   "Create a new integration worktree",
		GroupID: "run",
		Args: func(cmd *cobra.Command, args []string) error {
			switch len(args) {
			case 0:
				return errors.New(errors.EUsage, "use 'agency worktree create <worktree-name>'")
			case 1:
				return nil
			default:
				return errors.New(errors.EUsage, "too many arguments for \"agency worktree create\"")
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

			return commands.WorktreeCreate(ctx, cr, fsys, cwd, commands.WorktreeCreateOpts{
				RepoRef:    repoRef,
				Name:       args[0],
				BaseBranch: strings.TrimSpace(base),
				Open:       open,
				Editor:     editor,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
	}

	cmd.Flags().StringVar(&base, "base", "", "Base branch. Omit to use the current branch of the selected checkout.")
	cmd.Flags().BoolVarP(&open, "open", "o", false, "Open the new worktree in your editor after creation")
	cmd.Flags().StringVar(&editor, "editor", "", "Editor to use")

	return cmd
}

func newWorktreeLSCmd() *cobra.Command {
	var allRepos bool
	var all bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:     "ls",
		Short:   "List integration worktrees",
		GroupID: "inspect",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return errors.New(errors.EUsage, "too many arguments for \"agency worktree ls\"")
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

			return commands.WorktreeLS(ctx, cr, fsys, cwd, commands.WorktreeLSOpts{
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
	cmd.Flags().BoolVar(&all, "all", false, "Include archived worktrees")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}
