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

Use --repo to target a registered repo from any cwd. If you omit --repo, cwd
must already identify the repo, either because you are inside the repo checkout
or inside one of its present integration worktrees.

If you omit --base, it defaults to the current branch of the selected checkout.
The checkout used to resolve that branch must be clean.

Examples:
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

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Registered repo ref. Omit only when cwd already identifies the repo.")
	cmd.Flags().StringVar(&name, "name", "", "New integration worktree name")
	cmd.Flags().StringVar(&base, "base", "", "Base branch. Omit to use the current branch of the selected checkout.")
	cmd.Flags().BoolVarP(&open, "open", "o", false, "Open the new worktree in your editor after creation")
	cmd.Flags().StringVar(&editor, "editor", "", "Editor override to use with --open")
	_ = cmd.MarkFlagRequired("name")
	registerRepoFlagCompletion(cmd)

	return cmd
}
