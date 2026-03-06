package cobra

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestAgentModelThinkingFlagsParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		newCmd func() *cobra.Command
	}{
		{name: "start", newCmd: newAgentStartCmd},
		{name: "restart", newCmd: newAgentRestartCmd},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := tt.newCmd()
			require.NoError(t, cmd.ParseFlags([]string{"--model", "opus", "--thinking", "high"}))
		})
	}
}
