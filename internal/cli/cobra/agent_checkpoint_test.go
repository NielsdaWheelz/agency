package cobra

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentCheckpointLS_HasRepoFlag(t *testing.T) {
	t.Parallel()

	cmd := newAgentCheckpointLSCmd()
	require.NotNil(t, cmd.Flag("repo"), "checkpoint ls must support daemon repo-resolution via --repo")
}

func TestAgentCheckpointApply_HasRepoFlag(t *testing.T) {
	t.Parallel()

	cmd := newAgentCheckpointApplyCmd()
	require.NotNil(t, cmd.Flag("repo"), "checkpoint apply must support daemon repo-resolution via --repo")
}
