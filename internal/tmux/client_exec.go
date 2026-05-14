// Package tmux provides tmux integration for agency.
// This file implements the exec-backed Client using internal/exec.CommandRunner.
package tmux

import (
	"bufio"
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NielsdaWheelz/agency/internal/exec"
)

// maxStderrLen is the maximum stderr length to include in error messages.
const maxStderrLen = 4096

// ExecClient is a tmux Client implementation that shells out to tmux
// via internal/exec.CommandRunner.
type ExecClient struct {
	runner              exec.CommandRunner
	updateEnvironmentMu sync.Mutex
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
//
// Environment values are passed through the tmux client process environment,
// not as tmux argv flags, so secret values do not appear in process listings.
func (c *ExecClient) NewSession(ctx context.Context, name, cwd string, argv []string, env map[string]string) error {
	if len(argv) == 0 {
		return fmt.Errorf("tmux new-session: argv must have at least 1 element")
	}

	args := []string{"new-session", "-d", "-s", name, "-c", cwd}
	args = append(args, "--")
	args = append(args, argv...)

	keys := sortedTmuxUpdateEnvironmentKeys(env)
	if len(keys) > 0 {
		c.updateEnvironmentMu.Lock()
		defer c.updateEnvironmentMu.Unlock()
	}

	restoreUpdateEnvironment, err := c.extendUpdateEnvironment(ctx, keys)
	if err != nil {
		return err
	}
	if restoreUpdateEnvironment != nil {
		defer func() {
			restoreCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			restoreUpdateEnvironment(restoreCtx)
		}()
	}

	result, err := c.runner.Run(ctx, "tmux", args, exec.RunOpts{Env: env})
	if err != nil {
		return err
	}

	if result.ExitCode != 0 {
		return c.formatError("new-session", result.ExitCode, result.Stderr)
	}
	return nil
}

func (c *ExecClient) extendUpdateEnvironment(ctx context.Context, keys []string) (func(context.Context), error) {
	if len(keys) == 0 {
		return nil, nil
	}

	showResult, err := c.runner.Run(ctx, "tmux", []string{"show-option", "-gqv", "update-environment"}, exec.RunOpts{})
	if err != nil {
		return nil, err
	}
	if showResult.ExitCode != 0 {
		// No tmux server is running yet. The server started by new-session will
		// inherit the client process environment directly.
		return nil, nil
	}

	original := strings.TrimSpace(showResult.Stdout)
	updated := mergeUpdateEnvironmentKeys(original, keys)
	if updated == original {
		return nil, nil
	}

	setResult, err := c.runner.Run(ctx, "tmux", []string{"set-option", "-gq", "update-environment", updated}, exec.RunOpts{})
	if err != nil {
		return nil, err
	}
	if setResult.ExitCode != 0 {
		return nil, c.formatError("set-option", setResult.ExitCode, setResult.Stderr)
	}

	return func(ctx context.Context) {
		_, _ = c.runner.Run(ctx, "tmux", []string{"set-option", "-gq", "update-environment", original}, exec.RunOpts{})
	}, nil
}

func sortedTmuxUpdateEnvironmentKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		if isTmuxUpdateEnvironmentKey(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func isTmuxUpdateEnvironmentKey(key string) bool {
	if key == "" || strings.ContainsAny(key, " =\t\r\n") || strings.HasPrefix(key, "-") {
		return false
	}
	return true
}

func mergeUpdateEnvironmentKeys(original string, keys []string) string {
	requested := make(map[string]bool, len(keys))
	for _, key := range keys {
		requested[key] = true
	}
	seen := make(map[string]bool, len(keys))
	fields := strings.Fields(original)
	merged := make([]string, 0, len(fields)+len(keys))
	for _, field := range fields {
		name := strings.TrimPrefix(field, "-")
		if requested[name] && strings.HasPrefix(field, "-") {
			continue
		}
		merged = append(merged, field)
		if !strings.HasPrefix(field, "-") {
			seen[name] = true
		}
	}
	for _, key := range keys {
		if !seen[key] {
			merged = append(merged, key)
		}
	}
	return strings.Join(merged, " ")
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
