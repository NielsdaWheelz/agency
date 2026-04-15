package cobra

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentStart_HasRepoRef(t *testing.T) {
	t.Parallel()

	cmd := newAgentStartCmd()
	require.NotNil(t, cmd.Flag("repo"), "agent start must require explicit repo selection")
}
