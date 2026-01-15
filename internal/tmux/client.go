// Package tmux provides tmux integration for agency.
// This file defines the Client interface for testable tmux operations.
package tmux

import "context"

// Key represents a tmux key identifier for send-keys.
type Key string

// Key constants for common keys.
const (
	KeyCtrlC Key = "C-c"
)

// Client is the interface for tmux operations.
// All methods accept a context for cancellation (no hidden timeouts).
// Implementations must be safe for testing without tmux installed.
type Client interface {
	// HasSession checks if a tmux session exists by name.
	// Returns (true, nil) if session exists (exit code 0).
	// Returns (false, nil) if session does not exist (exit code 1).
	// Returns (false, error) for other exit codes or execution failures.
	HasSession(ctx context.Context, name string) (bool, error)

	// NewSession creates a new detached tmux session.
	// name: session name
	// cwd: working directory for the session
	// argv: command and arguments to run (must have at least 1 element)
	// Returns error if argv is empty or if tmux fails.
	NewSession(ctx context.Context, name, cwd string, argv []string) error

	// Attach attaches to an existing tmux session.
	// This blocks until the user detaches.
	// Returns error if session does not exist or attach fails.
	Attach(ctx context.Context, name string) error

	// KillSession kills an existing tmux session.
	// Returns error if session does not exist or kill fails.
	KillSession(ctx context.Context, name string) error

	// SendKeys sends keys to a tmux session.
	// keys must have at least 1 element.
	// Returns error if keys is empty, session does not exist, or send fails.
	SendKeys(ctx context.Context, name string, keys []Key) error
}
