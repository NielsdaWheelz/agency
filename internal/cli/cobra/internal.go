package cobra

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func newInternalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "internal",
		Hidden: true,
	}
	cmd.AddCommand(newInternalHeadedHookCmd())
	return cmd
}

func newInternalHeadedHookCmd() *cobra.Command {
	var repoID string
	var invocationID string
	var runner string
	var dataDir string

	cmd := &cobra.Command{
		Use:    "headed-hook",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return commands.HeadedHook(context.Background(), fs.NewRealFS(), commands.HeadedHookOpts{
				RepoID:          repoID,
				InvocationID:    invocationID,
				Runner:          runner,
				Stdin:           cmd.InOrStdin(),
				DataDirOverride: dataDir,
			})
		},
	}
	cmd.Flags().StringVar(&repoID, "repo-id", "", "Repository id")
	cmd.Flags().StringVar(&invocationID, "invocation-id", "", "Invocation id")
	cmd.Flags().StringVar(&runner, "runner", "", "Runner id")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Agency data dir")
	return cmd
}
