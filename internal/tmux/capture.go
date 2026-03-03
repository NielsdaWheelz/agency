// Package tmux provides tmux integration for agency.
// This file implements tmux session detection and scrollback capture.
package tmux

import (
	"context"
	"fmt"
	"strings"

	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
)

// Executor abstracts command execution for testing.
// The Run method executes a command and returns its output.
type Executor interface {
	// Run executes a command and returns stdout, stderr, exit code, and error.
	// err is non-nil only if the command failed to start (e.g., binary not found).
	// exitCode captures the actual exit status of the command.
	Run(name string, args ...string) (stdout string, stderr string, exitCode int, err error)
}

// RealExecutor implements Executor using internal/exec.
type RealExecutor struct {
	runner agencyexec.CommandRunner
}

// NewRealExecutor creates a new RealExecutor.
func NewRealExecutor() *RealExecutor {
	return &RealExecutor{runner: agencyexec.NewRealRunner()}
}

// Run executes the command and returns its output.
func (e *RealExecutor) Run(name string, args ...string) (stdout string, stderr string, exitCode int, err error) {
	result, runErr := e.runner.Run(context.Background(), name, args, agencyexec.RunOpts{})
	return result.Stdout, result.Stderr, result.ExitCode, runErr
}

// HasSession checks if a tmux session exists.
// Uses: tmux has-session -t <session>
// Returns true if the session exists (exit code 0).
func HasSession(exec Executor, session string) bool {
	_, _, exitCode, err := exec.Run("tmux", "has-session", "-t", session)
	if err != nil {
		// tmux binary not found or server not running
		return false
	}
	return exitCode == 0
}

// CaptureScrollback captures the full scrollback from a tmux pane.
// Uses: tmux capture-pane -p -S - -t <target>
//
// The target should be in format: session:window.pane (e.g., "agency_abc123:0.0")
// -S - means capture from the start of the scrollback history
// -p means print to stdout instead of to a buffer
//
// Returns the captured text (may include ANSI codes) or error if capture failed.
func CaptureScrollback(exec Executor, target string) (string, error) {
	stdout, stderr, exitCode, err := exec.Run("tmux", "capture-pane", "-p", "-S", "-", "-t", target)
	if err != nil {
		return "", fmt.Errorf("failed to run tmux capture-pane: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("tmux capture-pane failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

// SessionTarget returns the tmux target string for a run's primary pane.
// Format: agency_<run_id>:0.0
func SessionTarget(runID string) string {
	return fmt.Sprintf("agency_%s:0.0", runID)
}

// SessionName returns the tmux session name for a run.
// Format: agency_<run_id>
func SessionName(runID string) string {
	return fmt.Sprintf("agency_%s", runID)
}
