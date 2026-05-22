// Package tmux provides tmux integration for agency.
// This file defines shared tmux transport types.
package tmux

import "io"

// AttachedClient describes one tmux client attached to a session.
type AttachedClient struct {
	Name     string
	TTY      string
	PID      int
	ReadOnly bool
}

// AttachOpts configures an interactive tmux attach or switch-client handoff.
type AttachOpts struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// InsideTmux selects switch-client instead of attach-session.
	InsideTmux bool
}
