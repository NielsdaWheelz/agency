// Package tmux provides tmux integration for agency.
// This file implements the exec-backed Client using internal/exec.CommandRunner.
package tmux

import (
	"context"
	"fmt"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/exec"
)

// maxStderrLen is the maximum stderr length to include in error messages.
const maxStderrLen = 4096

// ExecClient is a tmux Client implementation that shells out to tmux
// via internal/exec.CommandRunner.
type ExecClient struct {
	runner exec.CommandRunner
}

// NewExecClient creates a new ExecClient with the given CommandRunner.
func NewExecClient(runner exec.CommandRunner) *ExecClient {
	return &ExecClient{runner: runner}
}

// HasSession implements Client.HasSession.
// Uses: tmux has-session -t <name>
// Exit code 0 = exists, 1 = not exists, other = error.
func (c *ExecClient) HasSession(ctx context.Context, name string) (bool, error) {
	args := []string{"has-session", "-t", name}
	result, err := c.runner.Run(ctx, "tmux", args, exec.RunOpts{})
	if err != nil {
		// Execution failure (binary not found, ctx canceled, etc.)
		return false, err
	}

	switch result.ExitCode {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, c.formatError("has-session", result.ExitCode, result.Stderr)
	}
}

// NewSession implements Client.NewSession.
// Uses: tmux new-session -d -s <name> -c <cwd> -- <argv...>
func (c *ExecClient) NewSession(ctx context.Context, name, cwd string, argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("tmux new-session: argv must have at least 1 element")
	}

	// Build args: new-session -d -s <name> -c <cwd> -- <cmd> <args...>
	args := []string{"new-session", "-d", "-s", name, "-c", cwd, "--"}
	args = append(args, argv...)

	result, err := c.runner.Run(ctx, "tmux", args, exec.RunOpts{})
	if err != nil {
		return err
	}

	if result.ExitCode != 0 {
		return c.formatError("new-session", result.ExitCode, result.Stderr)
	}
	return nil
}

// Attach implements Client.Attach.
// Uses: tmux attach -t <name>
func (c *ExecClient) Attach(ctx context.Context, name string) error {
	args := []string{"attach", "-t", name}
	result, err := c.runner.Run(ctx, "tmux", args, exec.RunOpts{})
	if err != nil {
		return err
	}

	if result.ExitCode != 0 {
		return c.formatError("attach", result.ExitCode, result.Stderr)
	}
	return nil
}

// KillSession implements Client.KillSession.
// Uses: tmux kill-session -t <name>
func (c *ExecClient) KillSession(ctx context.Context, name string) error {
	args := []string{"kill-session", "-t", name}
	result, err := c.runner.Run(ctx, "tmux", args, exec.RunOpts{})
	if err != nil {
		return err
	}

	if result.ExitCode != 0 {
		return c.formatError("kill-session", result.ExitCode, result.Stderr)
	}
	return nil
}

// SendKeys implements Client.SendKeys.
// Uses: tmux send-keys -t <name> <key1> <key2> ...
func (c *ExecClient) SendKeys(ctx context.Context, name string, keys []Key) error {
	if len(keys) == 0 {
		return fmt.Errorf("tmux send-keys: keys must have at least 1 element")
	}

	// Build args: send-keys -t <name> <key1> <key2> ...
	args := []string{"send-keys", "-t", name}
	for _, k := range keys {
		args = append(args, string(k))
	}

	result, err := c.runner.Run(ctx, "tmux", args, exec.RunOpts{})
	if err != nil {
		return err
	}

	if result.ExitCode != 0 {
		return c.formatError("send-keys", result.ExitCode, result.Stderr)
	}
	return nil
}

// formatError formats a tmux error with subcommand, exit code, and capped stderr.
func (c *ExecClient) formatError(subcmd string, exitCode int, stderr string) error {
	trimmed := strings.TrimSpace(stderr)
	if len(trimmed) > maxStderrLen {
		trimmed = trimmed[:maxStderrLen] + "..."
	}
	if trimmed == "" {
		return fmt.Errorf("tmux %s failed (exit=%d)", subcmd, exitCode)
	}
	return fmt.Errorf("tmux %s failed (exit=%d): %s", subcmd, exitCode, trimmed)
}
