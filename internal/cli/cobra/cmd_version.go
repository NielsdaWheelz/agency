package cobra

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/version"
)

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print agency version",
		Long:  "Print the agency version string.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "agency %s\n", version.FullVersion())
		},
	}

	return cmd
}
