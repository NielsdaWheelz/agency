package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
