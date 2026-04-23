package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newContextCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "context",
		Short: "Show or change the active repo/worktree context",
		Long: `Show or change the active repo/worktree context.

The active context is the fallback repo/worktree target for context-aware
commands such as "agency agent start" after explicit flags and current-directory
resolution. Set it when you want cross-directory fallback without repeating
--repo and --worktree.

Use:
  agency context           to show the active context
  agency context use <worktree-ref>
                           to set the active context
  agency context unset     to clear the active context`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}
			return commands.ContextShow(ctx, cr, fsys, cwd, commands.ContextShowOpts{
				JSON: jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "workflow"

	cmd.AddCommand(newContextUseCmd(), newContextUnsetCmd())
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		values := []string{"use", "unset"}
		candidates := make([]string, 0, len(values))
		for _, value := range values {
			if toComplete != "" && !strings.HasPrefix(value, toComplete) {
				continue
			}
			candidates = append(candidates, value)
		}
		return candidates, cobra.ShellCompDirectiveNoFileComp
	}

	return cmd
}

func newContextUseCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "use <worktree-ref>",
		Short: "Set the active repo/worktree context",
		Long: `Set the active repo/worktree context.

Pass --repo when cwd does not already identify the repo that owns the selected
worktree. This is the stateful fallback to repeating --repo and --worktree
from unrelated directories.`,
		Example: `  agency context use my-feature --repo agency
  agency context use 20260420185710-eaed --repo agency`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}
			return commands.ContextUse(ctx, cr, fsys, cwd, commands.ContextUseOpts{
				RepoRef:     repoRef,
				WorktreeRef: args[0],
				JSON:        jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Registered repo ref. Omit only when cwd already identifies the repo.")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	registerRepoFlagCompletion(cmd)
	setWorktreeArgCompletion(cmd, "present")

	return cmd
}

func newContextUnsetCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "unset",
		Short: "Clear the active repo/worktree context",
		Long: `Clear the active repo/worktree context.

After unset, context-aware commands must use explicit selectors such as
--repo and --worktree or rely on cwd-only inference.`,
		Example: `  agency context unset`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}
			return commands.ContextUnset(ctx, cr, fsys, cwd, commands.ContextUnsetOpts{
				JSON: jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}
