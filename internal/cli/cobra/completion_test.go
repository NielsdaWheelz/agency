package cobra

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestCompletionCmd_Bash(t *testing.T) {
	stdout, _, err := executeCmd("completion", "bash")
	require.NoError(t, err, "completion bash failed")
	if !strings.Contains(stdout, "__agency") {
		t.Fatalf("bash completion script missing function name")
	}
	if !strings.Contains(stdout, "complete") {
		t.Fatalf("bash completion script missing complete directive")
	}
}

func TestCompletionCmd_Zsh(t *testing.T) {
	stdout, _, err := executeCmd("completion", "zsh")
	require.NoError(t, err, "completion zsh failed")
	if !strings.Contains(stdout, "#compdef") {
		t.Fatalf("zsh completion script missing #compdef directive")
	}
}

func TestCompletionCmd_InvalidShell(t *testing.T) {
	_, _, err := executeCmd("completion", "fish")
	require.Error(t, err, "expected error for unsupported shell")
	if got := errors.GetCode(err); got != errors.EUsage {
		t.Fatalf("error code = %s, want %s", got, errors.EUsage)
	}
}

func TestCompletionCmd_MissingArg(t *testing.T) {
	_, _, err := executeCmd("completion")
	require.Error(t, err, "expected error when shell is missing")
}
