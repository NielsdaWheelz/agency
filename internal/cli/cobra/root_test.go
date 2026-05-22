package cobra

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoot_UnknownCommand(t *testing.T) {
	_, _, err := executeCmd("nonexistent")
	require.Error(t, err, "expected error for unknown command")
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error = %q, want unknown command", err.Error())
	}
}
