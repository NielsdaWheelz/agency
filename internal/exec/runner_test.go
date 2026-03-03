package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunScript_ExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		args       []string
		expectCode int
	}{
		{"exit 0", []string{"-c", "exit 0"}, 0},
		{"exit 1", []string{"-c", "exit 1"}, 1},
		{"exit 42", []string{"-c", "exit 42"}, 42},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			result, err := RunScript(ctx, "sh", tt.args, ScriptOpts{})
			require.NoError(t, err, "RunScript returned error")
			assert.Equal(t, tt.expectCode, result.ExitCode)
		})
	}
}

func TestRunScript_StdoutStderr(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	result, err := RunScript(ctx, "sh", []string{"-c", "echo stdout; echo stderr >&2"}, ScriptOpts{})
	require.NoError(t, err, "RunScript returned error")

	assert.Contains(t, result.Stdout, "stdout")
	assert.Contains(t, result.Stderr, "stderr")
}

func TestRunScript_TimeoutExit124(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	result, err := RunScript(ctx, "sh", []string{"-c", "sleep 10"}, ScriptOpts{
		Timeout: 50 * time.Millisecond,
	})

	assert.NoError(t, err, "RunScript with timeout should return nil error")
	assert.Equal(t, ExitTimeout, result.ExitCode)
}

func TestRunScript_CanceledExit125(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	startedFile := filepath.Join(t.TempDir(), "started")

	// Start a slow command and cancel once it has started
	done := make(chan struct{})
	var result CmdResult
	var err error

	go func() {
		result, err = RunScript(ctx, "sh", []string{"-c", fmt.Sprintf("touch %s; sleep 10", startedFile)}, ScriptOpts{})
		close(done)
	}()

	// Wait for the command to actually start, then cancel
	require.Eventually(t, func() bool {
		_, statErr := os.Stat(startedFile)
		return statErr == nil
	}, 5*time.Second, 10*time.Millisecond, "command did not start")
	cancel()

	<-done

	assert.Equal(t, context.Canceled, err, "RunScript with cancel should return context.Canceled")
	assert.Equal(t, ExitCanceled, result.ExitCode)
}

func TestRunScript_StartFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	result, err := RunScript(ctx, "no_such_command_abc123", nil, ScriptOpts{})

	require.Error(t, err, "RunScript with non-existent command should return error")
	assert.Equal(t, ExitStartFail, result.ExitCode)
}

func TestRunScript_Dir(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	result, err := RunScript(ctx, "sh", []string{"-c", "pwd"}, ScriptOpts{
		Dir: "/tmp",
	})
	require.NoError(t, err, "RunScript returned error")

	// On macOS, /tmp is a symlink to /private/tmp
	assert.Contains(t, result.Stdout, "tmp")
}

func TestRunScript_Env(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	result, err := RunScript(ctx, "sh", []string{"-c", "echo $TEST_VAR"}, ScriptOpts{
		Env: map[string]string{"TEST_VAR": "hello_world"},
	})
	require.NoError(t, err, "RunScript returned error")

	assert.Contains(t, result.Stdout, "hello_world")
}

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

	base := []string{
		"Z=z",
		"A=one",
		"A=two",
		"B=base",
	}
	overlay := map[string]string{
		"B": "override",
		"C": "three",
	}

	merged := mergeEnv(base, overlay)
	require.NotEmpty(t, merged)

	// Sorted keys
	keys := make([]string, 0, len(merged))
	for _, item := range merged {
		parts := strings.SplitN(item, "=", 2)
		require.Len(t, parts, 2)
		keys = append(keys, parts[0])
	}
	sortedKeys := append([]string(nil), keys...)
	sort.Strings(sortedKeys)
	assert.Equal(t, sortedKeys, keys)

	// No duplicate keys + override behavior
	values := map[string]string{}
	for _, item := range merged {
		parts := strings.SplitN(item, "=", 2)
		values[parts[0]] = parts[1]
	}
	assert.Equal(t, "override", values["B"])
	assert.Equal(t, "three", values["C"])
	assert.Equal(t, "two", values["A"])
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
