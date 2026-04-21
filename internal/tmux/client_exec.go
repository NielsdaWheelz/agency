// Package tmux provides tmux integration for agency.
// This file implements the exec-backed Client using internal/exec.CommandRunner.
package tmux

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
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

// CaptureScrollback implements Client.CaptureScrollback.
// Uses: tmux capture-pane -p -S - -t <target>
func (c *ExecClient) CaptureScrollback(ctx context.Context, target string) (string, error) {
	result, err := c.runner.Run(ctx, "tmux", []string{"capture-pane", "-p", "-S", "-", "-t", target}, exec.RunOpts{})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", c.formatError("capture-pane", result.ExitCode, result.Stderr)
	}
	return result.Stdout, nil
}

// PipePane implements Client.PipePane.
// Uses: tmux pipe-pane -o -t <target> "cat >> <logPath>"
func (c *ExecClient) PipePane(ctx context.Context, target, logPath string) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("tmux pipe-pane: target is required")
	}
	if strings.TrimSpace(logPath) == "" {
		return fmt.Errorf("tmux pipe-pane: log path is required")
	}
	result, err := c.runner.Run(ctx, "tmux", []string{"pipe-pane", "-o", "-t", target, "cat >> " + shellQuote(logPath)}, exec.RunOpts{})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return c.formatError("pipe-pane", result.ExitCode, result.Stderr)
	}
	return nil
}

// ListAttachedClients implements Client.ListAttachedClients.
// Uses: tmux list-clients -t <name> -F "#{client_name}\t#{client_tty}\t#{client_pid}\t#{client_readonly}"
func (c *ExecClient) ListAttachedClients(ctx context.Context, name string) ([]AttachedClient, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("tmux list-clients: session name is required")
	}

	const format = "#{client_name}\t#{client_tty}\t#{client_pid}\t#{client_readonly}"
	result, err := c.runner.Run(ctx, "tmux", []string{"list-clients", "-t", name, "-F", format}, exec.RunOpts{})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, c.formatError("list-clients", result.ExitCode, result.Stderr)
	}

	trimmed := strings.TrimSpace(result.Stdout)
	if trimmed == "" {
		return []AttachedClient{}, nil
	}

	clients := make([]AttachedClient, 0, strings.Count(trimmed, "\n")+1)
	scanner := bufio.NewScanner(strings.NewReader(trimmed))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("tmux list-clients: malformed client row %q", line)
		}

		pid, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("tmux list-clients: invalid client pid %q: %w", parts[2], err)
		}

		var readOnly bool
		switch parts[3] {
		case "0":
			readOnly = false
		case "1":
			readOnly = true
		default:
			return nil, fmt.Errorf("tmux list-clients: invalid read-only value %q", parts[3])
		}

		clients = append(clients, AttachedClient{
			Name:     parts[0],
			TTY:      parts[1],
			PID:      pid,
			ReadOnly: readOnly,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("tmux list-clients: %w", err)
	}
	return clients, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
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

// IsNoSessionErr returns true if the error indicates a tmux session does not exist.
// This is used to treat "session missing" as a non-error condition for archive cleanup.
//
// Per S6 spec, "no session" is defined as exit code 1 with stderr containing any of:
//   - "no server running"
//   - "can't find session"
//   - "no sessions"
func IsNoSessionErr(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "no server running") ||
		strings.Contains(errStr, "can't find session") ||
		strings.Contains(errStr, "no sessions") ||
		strings.Contains(errStr, "session not found")
}
