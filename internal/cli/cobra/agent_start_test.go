package cobra

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentStart_HasRepoRef(t *testing.T) {
	t.Parallel()

	cmd := newAgentStartCmd()
	require.NotNil(t, cmd.Flag("repo"), "agent start must expose explicit repo selection")
	require.NotNil(t, cmd.Flag("agency-config"), "agent start must expose explicit agency config selection")
}
