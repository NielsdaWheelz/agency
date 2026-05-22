package cobra

import (
	"slices"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func validateChangedTargetFlags(cmd *cobra.Command, subject string, action string, targetFlags []string, allowed ...string) error {
	for _, flag := range targetFlags {
		if cmd.Flags().Changed(flag) && !slices.Contains(allowed, flag) {
			return errors.New(errors.EUsage, "--"+flag+" is not valid for agency "+subject+" "+action)
		}
	}
	return nil
}
