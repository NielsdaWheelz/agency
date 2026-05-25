// Package exec provides a stub-friendly interface for running external commands.
package exec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
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

// AttachedRunOpts holds optional parameters for interactive/attached execution.
// Stdout/stderr/stdin are passed through as provided.
type AttachedRunOpts struct {
	Dir    string            // working directory (optional)
	Env    map[string]string // extra environment variables (overlay)
	Stdin  io.Reader         // stdin passthrough (optional)
	Stdout io.Writer         // stdout passthrough (optional)
	Stderr io.Writer         // stderr passthrough (optional)
}

// StartOpts holds options for starting a long-running process.
// This is used when callers need live pipes and a separate wait phase.
type StartOpts struct {
	Dir string // working directory (optional)

	// Env overlays variables on top of os.Environ() deterministically.
	Env map[string]string

	// EnvList, if set, is used as the full environment baseline.
	// Overlay Env still applies on top deterministically.
	EnvList []string

	Stdin  io.Reader // stdin source (optional)
	Stdout io.Writer // stdout sink (optional, incompatible with StdoutPipe)
	Stderr io.Writer // stderr sink (optional, incompatible with StderrPipe)

	StdoutPipe bool // expose stdout as pipe
	StderrPipe bool // expose stderr as pipe
	Setpgid    bool // start in its own process group
}

// ProcessExit is the normalized exit status of a started process.
type ProcessExit struct {
	ExitCode int
	Signal   string
}

// StartedProcess wraps a started command with optional pipes.
type StartedProcess struct {
	cmd        *exec.Cmd
	ctx        context.Context
	PID        int
	PGID       int
	StdoutPipe io.ReadCloser
	StderrPipe io.ReadCloser
}

// CommandRunner is the interface for running external commands.
// Implementations must be safe for stubbing in tests.
type CommandRunner interface {
	// Run executes a command and returns the result.
	// Returns CmdResult with ExitCode set if the process exits (even non-zero).
	// Returns error only for execution failures (binary not found, ctx canceled, io failure).
	Run(ctx context.Context, name string, args []string, opts RunOpts) (CmdResult, error)

	// LookPath searches for an executable named file in the directories
	// named by the PATH environment variable.
	// Returns the path to the executable, or an error if not found.
	LookPath(file string) (string, error)
}

type realRunner struct{}

// NewRealRunner creates a production command runner.
func NewRealRunner() CommandRunner {
	return &realRunner{}
}

// LookPath searches for an executable using the system PATH.
func (r *realRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// Run executes the command and captures stdout/stderr.
func (r *realRunner) Run(ctx context.Context, name string, args []string, opts RunOpts) (CmdResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	if len(opts.Env) > 0 {
		cmd.Env = MergeEnv(cmd.Environ(), opts.Env)
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

// Exit codes for special conditions.
const (
	exitTimeout   = 124 // command timed out
	exitCanceled  = 125 // context was canceled
	exitStartFail = -1  // command failed to start
)

// RunAttached executes a command with stdio passthrough.
// Exit code semantics mirror Run:
// - process exits with code N: ExitCode=N, err=nil
// - command failed to start: ExitCode=-1, err!=nil
// - context deadline exceeded: ExitCode=124, err=nil
// - context canceled: ExitCode=125, err=context.Canceled
func RunAttached(ctx context.Context, name string, args []string, opts AttachedRunOpts) (CmdResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr

	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	if len(opts.Env) > 0 {
		cmd.Env = MergeEnv(os.Environ(), opts.Env)
	}

	exitCode, _, returnedErr := classifyRunError(cmd.Run(), ctx.Err())
	return CmdResult{ExitCode: exitCode}, returnedErr
}

// StartProcess starts a command and returns a handle for streaming + waiting.
func StartProcess(ctx context.Context, name string, args []string, opts StartOpts) (*StartedProcess, error) {
	if opts.StdoutPipe && opts.Stdout != nil {
		return nil, errors.New("stdout pipe cannot be combined with stdout writer")
	}
	if opts.StderrPipe && opts.Stderr != nil {
		return nil, errors.New("stderr pipe cannot be combined with stderr writer")
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr

	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if opts.Setpgid {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	baseEnv := os.Environ()
	if len(opts.EnvList) > 0 {
		baseEnv = slices.Clone(opts.EnvList)
	}
	if len(opts.Env) > 0 {
		cmd.Env = MergeEnv(baseEnv, opts.Env)
	} else if len(opts.EnvList) > 0 {
		cmd.Env = baseEnv
	}

	var stdoutPipe io.ReadCloser
	var stderrPipe io.ReadCloser
	var err error
	if opts.StdoutPipe {
		stdoutPipe, err = cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
	}
	if opts.StderrPipe {
		stderrPipe, err = cmd.StderrPipe()
		if err != nil {
			if stdoutPipe != nil {
				_ = stdoutPipe.Close()
			}
			return nil, err
		}
	}

	if err := cmd.Start(); err != nil {
		if stdoutPipe != nil {
			_ = stdoutPipe.Close()
		}
		if stderrPipe != nil {
			_ = stderrPipe.Close()
		}
		return nil, err
	}

	pgid := 0
	if opts.Setpgid {
		pgid = cmd.Process.Pid
	}

	return &StartedProcess{
		cmd:        cmd,
		ctx:        ctx,
		PID:        cmd.Process.Pid,
		PGID:       pgid,
		StdoutPipe: stdoutPipe,
		StderrPipe: stderrPipe,
	}, nil
}

// WaitExit waits for the started process and normalizes exit status.
func (p *StartedProcess) WaitExit() (ProcessExit, error) {
	exitCode, signal, returnedErr := classifyRunError(p.cmd.Wait(), p.ctx.Err())
	return ProcessExit{ExitCode: exitCode, Signal: signal}, returnedErr
}

// classifyRunError converts a cmd.Run/cmd.Wait error and the parent context's
// error into normalized (exitCode, signal, returnedErr). signal is "" unless
// the process was terminated by a POSIX signal.
func classifyRunError(runErr, ctxErr error) (exitCode int, signal string, returnedErr error) {
	if runErr == nil {
		return 0, "", nil
	}
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		return exitTimeout, "", nil
	}
	if errors.Is(ctxErr, context.Canceled) {
		return exitCanceled, "", context.Canceled
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			signal = status.Signal().String()
		}
		return exitErr.ExitCode(), signal, nil
	}
	return exitStartFail, "", runErr
}

// MergeEnv applies overlay maps on top of a base environment, deterministically:
// later overlays win, malformed base entries (no "=") are dropped, no duplicate
// keys, and keys are sorted for reproducible output.
func MergeEnv(baseEnv []string, overlays ...map[string]string) []string {
	merged := make(map[string]string, len(baseEnv))

	for _, entry := range baseEnv {
		key, val, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		merged[key] = val
	}
	for _, overlay := range overlays {
		for k, v := range overlay {
			merged[k] = v
		}
	}

	keys := slices.Sorted(maps.Keys(merged))

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+merged[k])
	}
	return out
}

// IsPIDAlive reports whether a process with the given PID exists. Returns
// false for non-positive PIDs. Treats EPERM (process exists but we lack
// permission to signal it) as alive.
func IsPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// NonInteractiveEnv returns the environment overlay that disables interactive
// prompts in spawned git, gh, and runner processes.
func NonInteractiveEnv() map[string]string {
	return map[string]string{
		"CI":                  "1",
		"GIT_TERMINAL_PROMPT": "0",
		"GH_PROMPT_DISABLED":  "1",
	}
}
