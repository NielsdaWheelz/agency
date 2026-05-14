package cobra

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentLS_WatchFlagRemoved(t *testing.T) {
	_, _, err := executeCmd("agent", "ls", "--watch", "--json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag: --watch")
}

func TestWorktreeLS_WatchFlagRemoved(t *testing.T) {
	_, _, err := executeCmd("worktree", "ls", "--watch", "--json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag: --watch")
}
