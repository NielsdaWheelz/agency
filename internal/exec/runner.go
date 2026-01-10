// Package exec provides a stub-friendly interface for running external commands.
package exec

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

// CmdResult holds the result of a command execution.
type CmdResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunOpts holds optional parameters for command execution.
type RunOpts struct {
	Dir string            // working directory (optional)
	Env map[string]string // extra environment variables (overlay)
}

// CommandRunner is the interface for running external commands.
// Implementations must be safe for stubbing in tests.
type CommandRunner interface {
	// Run executes a command and returns the result.
	// Returns CmdResult with ExitCode set if the process exits (even non-zero).
	// Returns error only for execution failures (binary not found, ctx canceled, io failure).
	Run(ctx context.Context, name string, args []string, opts RunOpts) (CmdResult, error)
}

// RealRunner is the production implementation of CommandRunner using os/exec.
type RealRunner struct{}

// NewRealRunner creates a new RealRunner.
func NewRealRunner() *RealRunner {
	return &RealRunner{}
}

// Run executes the command and captures stdout/stderr.
func (r *RealRunner) Run(ctx context.Context, name string, args []string, opts RunOpts) (CmdResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	if len(opts.Env) > 0 {
		cmd.Env = cmd.Environ()
		for k, v := range opts.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	err := cmd.Run()

	result := CmdResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		// Check if it's an exit error (process ran but exited non-zero)
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		// Other errors (binary not found, ctx canceled, etc.)
		return result, err
	}

	result.ExitCode = 0
	return result, nil
}

// ScriptOpts holds options for script execution with timeout handling.
type ScriptOpts struct {
	Dir     string            // working directory (optional)
	Env     map[string]string // extra environment variables (overlay)
	Timeout time.Duration     // 0 = no additional timeout beyond ctx
}

// Exit codes for special conditions.
const (
	ExitTimeout    = 124 // command timed out
	ExitCanceled   = 125 // context was canceled
	ExitStartFail  = -1  // command failed to start
)

// RunScript executes a command with timeout and cancel handling.
// Exit code rules:
// - if process exits with code N: return ExitCode=N, err=nil
// - if command failed to start: return err != nil, ExitCode=-1
// - if context deadline exceeded: return ExitCode=124, err=nil
// - if context canceled: return ExitCode=125, err=context.Canceled
// Stdout/stderr are captured in all cases.
// Stdin is always /dev/null.
func RunScript(ctx context.Context, name string, args []string, opts ScriptOpts) (CmdResult, error) {
	// Apply timeout if specified
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// stdin = /dev/null (go stdlib reads from os.DevNull when cmd.Stdin is nil)
	cmd.Stdin = nil

	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	// Build environment: start from os.Environ(), apply Env overrides
	if len(opts.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range opts.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	err := cmd.Run()

	result := CmdResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		// Check if it's a context deadline exceeded (timeout)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.ExitCode = ExitTimeout
			return result, nil
		}

		// Check if it's a context cancellation
		if errors.Is(ctx.Err(), context.Canceled) {
			result.ExitCode = ExitCanceled
			return result, context.Canceled
		}

		// Check if it's an exit error (process ran but exited non-zero)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}

		// Other errors (binary not found, etc.)
		result.ExitCode = ExitStartFail
		return result, err
	}

	result.ExitCode = 0
	return result, nil
}
