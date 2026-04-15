package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func newWorktreeCreateCmd() *cobra.Command {
	var repoRef string
	var name string
	var parent string
	var open bool
	var editor string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new integration worktree",
		Long: `Create a new integration worktree.

An integration worktree is a stable branch you intend to merge, push, or PR.
It is independent of any agent invocation.

Example:
  agency worktree create --repo agency --name my-feature --parent main
  agency worktree create --repo agency --name bugfix --parent develop --open`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return commands.WorktreeCreate(cmd.Context(), exec.NewRealRunner(), fs.NewRealFS(), commands.WorktreeCreateOpts{
				RepoRef:      repoRef,
				Name:         name,
				ParentBranch: parent,
				Open:         open,
				Editor:       editor,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo name, key, id, or prefix (required)")
	cmd.Flags().StringVar(&name, "name", "", "Name for the integration worktree (required)")
	cmd.Flags().StringVar(&parent, "parent", "", "Parent branch to branch from (required)")
	cmd.Flags().BoolVarP(&open, "open", "o", false, "Open the worktree in editor after creation")
	cmd.Flags().StringVar(&editor, "editor", "", "Editor to use (overrides config)")

	return cmd
}
