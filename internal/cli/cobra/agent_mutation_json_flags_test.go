package cobra

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestAgentMutationCommandsAcceptJSONFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		newCmd func() *cobra.Command
	}{
		{name: "start", newCmd: newAgentStartCmd},
		{name: "stop", newCmd: newAgentStopCmd},
		{name: "kill", newCmd: newAgentKillCmd},
		{name: "land", newCmd: newAgentLandCmd},
		{name: "discard", newCmd: newAgentDiscardCmd},
		{name: "chat", newCmd: newAgentChatCmd},
		{name: "restart", newCmd: newAgentRestartCmd},
		{name: "merge", newCmd: newAgentMergeCmd},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := tt.newCmd()
			require.NoError(t, cmd.ParseFlags([]string{"--json"}))
		})
	}
}
