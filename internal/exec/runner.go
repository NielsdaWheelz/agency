// Package exec provides a stub-friendly interface for running external commands.
package exec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sort"
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

// RealRunner is the production implementation of CommandRunner using os/exec.
type RealRunner struct{}

// NewRealRunner creates a new RealRunner.
func NewRealRunner() *RealRunner {
	return &RealRunner{}
}

// LookPath searches for an executable using the system PATH.
func (r *RealRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
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
		cmd.Env = mergeEnv(cmd.Environ(), opts.Env)
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
	ExitTimeout   = 124 // command timed out
	ExitCanceled  = 125 // context was canceled
	ExitStartFail = -1  // command failed to start
)

// RunAttached executes a command with stdio passthrough.
// Exit code semantics mirror RunScript/Run:
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
		cmd.Env = mergeEnv(os.Environ(), opts.Env)
	}

	err := cmd.Run()
	result := CmdResult{}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.ExitCode = ExitTimeout
			return result, nil
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			result.ExitCode = ExitCanceled
			return result, context.Canceled
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		result.ExitCode = ExitStartFail
		return result, err
	}

	result.ExitCode = 0
	return result, nil
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
		baseEnv = append([]string(nil), opts.EnvList...)
	}
	if len(opts.Env) > 0 {
		cmd.Env = mergeEnv(baseEnv, opts.Env)
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
	result := ProcessExit{}
	err := p.cmd.Wait()
	if err != nil {
		if errors.Is(p.ctx.Err(), context.DeadlineExceeded) {
			result.ExitCode = ExitTimeout
			return result, nil
		}
		if errors.Is(p.ctx.Err(), context.Canceled) {
			result.ExitCode = ExitCanceled
			return result, context.Canceled
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				result.Signal = status.Signal().String()
			}
			return result, nil
		}
		result.ExitCode = ExitStartFail
		return result, err
	}

	result.ExitCode = 0
	return result, nil
}

// SignalGroup sends a signal to the process group when available.
// Falls back to signaling the PID directly when no PGID is set.
func (p *StartedProcess) SignalGroup(sig syscall.Signal) error {
	if p.PGID > 0 {
		return syscall.Kill(-p.PGID, sig)
	}
	if p.PID > 0 {
		return syscall.Kill(p.PID, sig)
	}
	return errors.New("process has no pid")
}

// mergeEnv applies overlay vars on top of base env deterministically.
// Rules:
// - override wins
// - no duplicate keys in output
// - keys sorted for reproducibility
func mergeEnv(base []string, overlay map[string]string) []string {
	merged := make(map[string]string, len(base)+len(overlay))

	for _, item := range base {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 1 {
			merged[parts[0]] = ""
			continue
		}
		merged[parts[0]] = parts[1]
	}
	for k, v := range overlay {
		merged[k] = v
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+merged[k])
	}
	return out
}
