package cobra

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInternalHeadedHookRejectsArgs(t *testing.T) {
	_, _, err := executeCmd("internal", "headed-hook", "--repo-id", "repo", "--invocation-id", "inv", "--runner", "codex", "unexpected")
	require.Error(t, err)
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error = %q, want unknown command", err.Error())
	}
}
