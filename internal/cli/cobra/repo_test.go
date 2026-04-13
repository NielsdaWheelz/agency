package cobra

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestRepoAddReturnsCWDLookupError(t *testing.T) {
	origGetWorkingDir := getWorkingDir
	t.Cleanup(func() { getWorkingDir = origGetWorkingDir })
	getWorkingDir = func() (string, error) {
		return "", errors.New("cwd unavailable")
	}

	cmd := newRepoAddCmd()
	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get cwd")
}

func TestRepoS1CommandsReturnCWDLookupError(t *testing.T) {
	origGetWorkingDir := getWorkingDir
	t.Cleanup(func() { getWorkingDir = origGetWorkingDir })
	getWorkingDir = func() (string, error) {
		return "", errors.New("cwd unavailable")
	}

	cmds := map[string]*cobra.Command{
		"readiness": newRepoS1ReadinessCmd(),
		"report":    newRepoS1ReportCmd(),
		"freeze":    newRepoS1FreezeCmd(),
	}

	for name, cmd := range cmds {
		t.Run(name, func(t *testing.T) {
			err := cmd.RunE(cmd, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), "failed to get cwd")
		})
	}
}
