package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newWorktreeCmd() *cobra.Command {
	var repoRef string
	var allRepos bool
	var all bool
	var jsonOut bool
	var base string
	var open bool
	var editor string
	var force bool
	var yes bool
	var allowDirty bool
	var forceWithLease bool
	var squash bool
	var mergeStrategy bool
	var rebaseStrategy bool
	var noDeleteBranch bool
	var agencyConfigPath string

	cmd := &cobra.Command{
		Use:   "worktree",
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
  agency worktree <worktree-ref> open
                           to open one worktree
  agency worktree <worktree-ref> pr sync
                           to push and sync one pull request`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case len(args) == 0:
				_ = cmd.Help()
				return errors.New(errors.EUsage, "specify 'create', 'ls', or a worktree ref")
			case args[0] == "create":
				if len(args) != 2 {
					if len(args) < 2 {
						return errors.New(errors.EUsage, "use 'agency worktree create <worktree-name>'")
					}
					return errors.New(errors.EUsage, "too many arguments for \"agency worktree create\"")
				}
				return runWorktreeCreate(cmd, args[1], repoRef, base, open, editor)
			case args[0] == "ls":
				if len(args) != 1 {
					return errors.New(errors.EUsage, "too many arguments for \"agency worktree ls\"")
				}
				return runWorktreeLS(cmd, repoRef, allRepos, all, jsonOut)
			default:
				worktreeRef := args[0]
				switch {
				case len(args) == 1:
					return runWorktreeShow(cmd, worktreeRef, repoRef, jsonOut)
				case len(args) == 2:
					switch args[1] {
					case "path":
						return runWorktreePath(cmd, worktreeRef, repoRef)
					case "open":
						return runWorktreeOpen(cmd, worktreeRef, repoRef, editor)
					case "shell":
						return runWorktreeShell(cmd, worktreeRef, repoRef)
					case "rm":
						return runWorktreeRm(cmd, worktreeRef, repoRef, force, yes)
					case "rebase":
						return runWorktreeRebase(cmd, worktreeRef, repoRef, jsonOut)
					case "pr":
						return errors.New(errors.EUsage, "use 'agency worktree <worktree-ref> pr sync' or 'agency worktree <worktree-ref> pr merge'")
					default:
						return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency worktree\"")
					}
				case len(args) == 3 && args[1] == "pr":
					switch args[2] {
					case "sync":
						return runWorktreePRSync(cmd, worktreeRef, repoRef, jsonOut, allowDirty, forceWithLease)
					case "merge":
						return runWorktreePRMerge(cmd, worktreeRef, repoRef, jsonOut, squash, mergeStrategy, rebaseStrategy, noDeleteBranch, yes, agencyConfigPath)
					default:
						return errors.New(errors.EUsage, "unknown command \""+args[2]+"\" for \"agency worktree "+worktreeRef+" pr\"")
					}
				default:
					return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency worktree\"")
				}
			}
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "List across all registered repos")
	cmd.Flags().BoolVar(&all, "all", false, "Include archived worktrees")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	cmd.Flags().StringVar(&base, "base", "", "Base branch. Omit to use the current branch of the selected checkout.")
	cmd.Flags().BoolVarP(&open, "open", "o", false, "Open the new worktree in your editor after creation")
	cmd.Flags().StringVar(&editor, "editor", "", "Editor to use")
	cmd.Flags().BoolVar(&force, "force", false, "Force removal even if worktree has uncommitted changes")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm removal in non-interactive mode")
	cmd.Flags().BoolVar(&allowDirty, "allow-dirty", false, "Allow sync with dirty integration worktree")
	cmd.Flags().BoolVar(&forceWithLease, "force-with-lease", false, "Use git push --force-with-lease")
	cmd.Flags().BoolVar(&squash, "squash", false, "Use squash merge strategy (default)")
	cmd.Flags().BoolVar(&mergeStrategy, "merge", false, "Use regular merge strategy")
	cmd.Flags().BoolVar(&rebaseStrategy, "rebase", false, "Use rebase merge strategy")
	cmd.Flags().BoolVar(&noDeleteBranch, "no-delete-branch", false, "Preserve remote branch after merge")
	cmd.Flags().StringVar(&agencyConfigPath, "agency-config", "", "load agency config from this file")
	cmd.MarkFlagsMutuallyExclusive("repo", "all-repos")
	cmd.MarkFlagsMutuallyExclusive("squash", "merge", "rebase")
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
			candidates := []string{"create\tCreate a new integration worktree", "ls\tList integration worktrees"}
			worktrees, directive := completeWorktreeRefsForState(cmd, toComplete, "all")
			return append(candidates, worktrees...), directive
		case 1:
			if args[0] == "create" || args[0] == "ls" {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			values := []string{"path", "open", "shell", "rm", "rebase", "pr"}
			candidates := make([]string, 0, len(values))
			for _, value := range values {
				if toComplete != "" && !strings.HasPrefix(value, toComplete) {
					continue
				}
				candidates = append(candidates, value)
			}
			return candidates, cobra.ShellCompDirectiveNoFileComp
		case 2:
			if args[1] != "pr" {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			values := []string{"sync", "merge"}
			candidates := make([]string, 0, len(values))
			for _, value := range values {
				if toComplete != "" && !strings.HasPrefix(value, toComplete) {
					continue
				}
				candidates = append(candidates, value)
			}
			return candidates, cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	return cmd
}

func runWorktreeCreate(cmd *cobra.Command, name string, repoRef string, base string, open bool, editor string) error {
	ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
	if err != nil {
		return err
	}

	return commands.WorktreeCreate(ctx, cr, fsys, cwd, commands.WorktreeCreateOpts{
		RepoRef:    repoRef,
		Name:       name,
		BaseBranch: strings.TrimSpace(base),
		Open:       open,
		Editor:     editor,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runWorktreeLS(cmd *cobra.Command, repoRef string, allRepos bool, all bool, jsonOut bool) error {
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
}

func runWorktreeShow(cmd *cobra.Command, worktreeRef string, repoRef string, jsonOut bool) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.WorktreeShow(ctx, cr, fsys, cwd, commands.WorktreeShowOpts{
		WorktreeRef: worktreeRef,
		RepoRef:     repoRef,
		JSON:        jsonOut,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runWorktreePath(cmd *cobra.Command, worktreeRef string, repoRef string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.WorktreePath(ctx, cr, fsys, cwd, commands.WorktreePathOpts{
		WorktreeRef: worktreeRef,
		RepoRef:     repoRef,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runWorktreeOpen(cmd *cobra.Command, worktreeRef string, repoRef string, editor string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.WorktreeOpen(ctx, cr, fsys, cwd, commands.WorktreeOpenOpts{
		WorktreeRef: worktreeRef,
		RepoRef:     repoRef,
		Editor:      editor,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runWorktreeShell(cmd *cobra.Command, worktreeRef string, repoRef string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.WorktreeShell(ctx, cr, fsys, cwd, commands.WorktreeShellOpts{
		WorktreeRef: worktreeRef,
		RepoRef:     repoRef,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runWorktreeRm(cmd *cobra.Command, worktreeRef string, repoRef string, force bool, yes bool) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.WorktreeRm(ctx, cr, fsys, cwd, commands.WorktreeRmOpts{
		WorktreeRef: worktreeRef,
		RepoRef:     repoRef,
		Force:       force,
		Yes:         yes,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runWorktreeRebase(cmd *cobra.Command, worktreeRef string, repoRef string, jsonOut bool) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.WorktreeRebase(ctx, cr, fsys, cwd, commands.WorktreeRebaseOpts{
		WorktreeRef: worktreeRef,
		RepoRef:     repoRef,
		JSON:        jsonOut,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runWorktreePRSync(cmd *cobra.Command, worktreeRef string, repoRef string, jsonOut bool, allowDirty bool, forceWithLease bool) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.WorktreePRSync(ctx, cr, fsys, cwd, commands.WorktreePRSyncOpts{
		WorktreeRef:    worktreeRef,
		RepoRef:        repoRef,
		AllowDirty:     allowDirty,
		ForceWithLease: forceWithLease,
		JSON:           jsonOut,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func runWorktreePRMerge(cmd *cobra.Command, worktreeRef string, repoRef string, jsonOut bool, squash bool, mergeStrategy bool, rebaseStrategy bool, noDeleteBranch bool, yes bool, agencyConfigPath string) error {
	ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
	if err != nil {
		return err
	}

	return commands.WorktreePRMerge(ctx, cr, fsys, cwd, commands.WorktreePRMergeOpts{
		WorktreeRef:      worktreeRef,
		RepoRef:          repoRef,
		Squash:           squash,
		Merge:            mergeStrategy,
		Rebase:           rebaseStrategy,
		NoDeleteBranch:   noDeleteBranch,
		Yes:              yes,
		JSON:             jsonOut,
		AgencyConfigPath: agencyConfigPath,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}
