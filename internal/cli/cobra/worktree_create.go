package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newWorktreeCreateCmd() *cobra.Command {
	var repoRef string
	var name string
	var base string
	var open bool
	var editor string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new integration worktree",
		Long: `Create a new integration worktree.

An integration worktree is a stable branch you intend to merge, push, or PR.
It is independent of any agent invocation.

Example:
  agency worktree create --repo agency --name my-feature --base main
  agency worktree create --name my-feature
  agency worktree create --repo agency --name bugfix --base develop --open`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo name, key, id, or prefix (defaults to current directory)")
	cmd.Flags().StringVar(&name, "name", "", "Name for the integration worktree (required)")
	cmd.Flags().StringVar(&base, "base", "", "Base branch to branch from (defaults to current branch)")
	cmd.Flags().BoolVarP(&open, "open", "o", false, "Open the worktree in editor after creation")
	cmd.Flags().StringVar(&editor, "editor", "", "Editor to use (overrides config)")

	return cmd
}
