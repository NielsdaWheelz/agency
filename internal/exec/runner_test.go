package exec

import (
	"bytes"
	"context"
	"io"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunAttached_PassthroughAndExitCode(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	result, err := RunAttached(context.Background(), "sh", []string{"-c", "echo hi; echo err >&2"}, AttachedRunOpts{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), "hi")
	assert.Contains(t, stderr.String(), "err")
}

func TestMergeEnv_DeterministicNoDuplicatesOverrideWins(t *testing.T) {
	t.Parallel()

	base := []string{"Z=z", "A=one", "A=two", "INVALID", "B=base"}
	merged := MergeEnv(base,
		map[string]string{"B": "first", "C": "three"},
		map[string]string{"B": "override"},
	)

	assert.Equal(t, []string{
		"A=two",
		"B=override",
		"C=three",
		"Z=z",
	}, merged)
}

func TestStartProcess_WithPipesAndWaitExit(t *testing.T) {
	t.Parallel()

	proc, err := StartProcess(context.Background(), "sh", []string{"-c", "echo out; echo err >&2"}, StartOpts{
		StdoutPipe: true,
		StderrPipe: true,
		Setpgid:    true,
	})
	require.NoError(t, err)
	require.NotNil(t, proc.StdoutPipe)
	require.NotNil(t, proc.StderrPipe)
	require.NotZero(t, proc.PID)
	require.NotZero(t, proc.PGID)

	stdoutData, readOutErr := io.ReadAll(proc.StdoutPipe)
	stderrData, readErrErr := io.ReadAll(proc.StderrPipe)
	// Depending on process timing, pipes may be closed before full drain.
	// In that case, output assertions below still validate pipe wiring.
	if readOutErr != nil {
		assert.Contains(t, readOutErr.Error(), "closed")
	}
	if readErrErr != nil {
		assert.Contains(t, readErrErr.Error(), "closed")
	}

	exit, waitErr := proc.WaitExit()
	require.NoError(t, waitErr)
	assert.Equal(t, 0, exit.ExitCode)

	assert.Contains(t, string(stdoutData), "out")
	assert.Contains(t, string(stderrData), "err")
}

func TestStartProcess_SignalGroupTerminatesProcess(t *testing.T) {
	t.Parallel()

	proc, err := StartProcess(context.Background(), "sh", []string{"-c", "sleep 5"}, StartOpts{
		Setpgid: true,
	})
	require.NoError(t, err)

	require.NoError(t, proc.SignalGroup(syscall.SIGKILL))
	exit, waitErr := proc.WaitExit()
	require.NoError(t, waitErr)
	assert.NotEqual(t, 0, exit.ExitCode)
}
