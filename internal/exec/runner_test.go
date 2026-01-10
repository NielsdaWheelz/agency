package exec

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunScript_ExitCode(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := RunScript(ctx, "sh", tt.args, ScriptOpts{})
			if err != nil {
				t.Fatalf("RunScript returned error: %v", err)
			}
			if result.ExitCode != tt.expectCode {
				t.Errorf("exit code = %d, want %d", result.ExitCode, tt.expectCode)
			}
		})
	}
}

func TestRunScript_StdoutStderr(t *testing.T) {
	ctx := context.Background()
	result, err := RunScript(ctx, "sh", []string{"-c", "echo stdout; echo stderr >&2"}, ScriptOpts{})
	if err != nil {
		t.Fatalf("RunScript returned error: %v", err)
	}

	if !strings.Contains(result.Stdout, "stdout") {
		t.Errorf("stdout = %q, want to contain 'stdout'", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "stderr") {
		t.Errorf("stderr = %q, want to contain 'stderr'", result.Stderr)
	}
}

func TestRunScript_TimeoutExit124(t *testing.T) {
	ctx := context.Background()
	result, err := RunScript(ctx, "sh", []string{"-c", "sleep 10"}, ScriptOpts{
		Timeout: 50 * time.Millisecond,
	})

	if err != nil {
		t.Errorf("RunScript with timeout should return nil error, got: %v", err)
	}
	if result.ExitCode != ExitTimeout {
		t.Errorf("timeout exit code = %d, want %d", result.ExitCode, ExitTimeout)
	}
}

func TestRunScript_CanceledExit125(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Start a slow command and cancel quickly
	done := make(chan struct{})
	var result CmdResult
	var err error

	go func() {
		result, err = RunScript(ctx, "sh", []string{"-c", "sleep 10"}, ScriptOpts{})
		close(done)
	}()

	// Give the command time to start, then cancel
	time.Sleep(50 * time.Millisecond)
	cancel()

	<-done

	if err != context.Canceled {
		t.Errorf("RunScript with cancel should return context.Canceled, got: %v", err)
	}
	if result.ExitCode != ExitCanceled {
		t.Errorf("cancel exit code = %d, want %d", result.ExitCode, ExitCanceled)
	}
}

func TestRunScript_StartFailure(t *testing.T) {
	ctx := context.Background()
	result, err := RunScript(ctx, "no_such_command_abc123", nil, ScriptOpts{})

	if err == nil {
		t.Errorf("RunScript with non-existent command should return error")
	}
	if result.ExitCode != ExitStartFail {
		t.Errorf("start failure exit code = %d, want %d", result.ExitCode, ExitStartFail)
	}
}

func TestRunScript_Dir(t *testing.T) {
	ctx := context.Background()
	result, err := RunScript(ctx, "sh", []string{"-c", "pwd"}, ScriptOpts{
		Dir: "/tmp",
	})
	if err != nil {
		t.Fatalf("RunScript returned error: %v", err)
	}

	// On macOS, /tmp is a symlink to /private/tmp
	if !strings.Contains(result.Stdout, "tmp") {
		t.Errorf("with Dir=/tmp, pwd output = %q, want to contain 'tmp'", result.Stdout)
	}
}

func TestRunScript_Env(t *testing.T) {
	ctx := context.Background()
	result, err := RunScript(ctx, "sh", []string{"-c", "echo $TEST_VAR"}, ScriptOpts{
		Env: map[string]string{"TEST_VAR": "hello_world"},
	})
	if err != nil {
		t.Fatalf("RunScript returned error: %v", err)
	}

	if !strings.Contains(result.Stdout, "hello_world") {
		t.Errorf("with Env, output = %q, want to contain 'hello_world'", result.Stdout)
	}
}
