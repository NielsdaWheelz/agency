package cobra

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func newLSCmd() *cobra.Command {
	var repoPath string
	var all bool
	var allRepos bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List runs and their statuses",
		Long: `List runs and their statuses.
By default, lists runs for the current repo (excludes archived).
If not inside a git repo, lists runs across all repos.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get working directory", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			opts := commands.LSOpts{
				RepoPath: repoPath,
				All:      all,
				AllRepos: allRepos,
				JSON:     jsonOutput,
			}

			return commands.LS(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", "", "scope listing to a specific repo")
	cmd.Flags().BoolVar(&all, "all", false, "include archived runs")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "list runs across all repos (ignores current repo scope)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON (stable format)")

	return cmd
}
