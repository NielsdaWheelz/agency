package cobra

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func completeRunnerModes(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	values := []string{string(store.RunnerModeHeadless), string(store.RunnerModeHeaded)}
	return completeStaticValues(values, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeRunnerKinds(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeStaticValues(runners.CanonicalIDs(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeLogKinds(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeStaticValues(daemon.InvocationLogKinds(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeStaticValues(values []string, toComplete string) []string {
	candidates := make([]string, 0, len(values))
	for _, value := range values {
		if toComplete != "" && !strings.HasPrefix(value, toComplete) {
			continue
		}
		candidates = append(candidates, value)
	}
	return candidates
}

func registerRunnerFlagCompletion(cmd *cobra.Command) {
	if cmd == nil || cmd.Flag("runner") == nil {
		return
	}
	if err := cmd.RegisterFlagCompletionFunc("runner", completeRunnerKinds); err != nil {
		panic(err)
	}
}

func registerLogKindFlagCompletion(cmd *cobra.Command) {
	if cmd == nil || cmd.Flag("kind") == nil {
		return
	}
	if err := cmd.RegisterFlagCompletionFunc("kind", completeLogKinds); err != nil {
		panic(err)
	}
}
