package cobra

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckpointLS_HasRepoFlag(t *testing.T) {
	t.Parallel()

	cmd := newCheckpointLSCmd()
	require.NotNil(t, cmd.Flag("repo"), "checkpoint ls must support daemon repo-resolution via --repo")
}
