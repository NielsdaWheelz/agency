// Package tty provides TTY detection helpers for agency commands.
package tty

import "os"

// IsTTY returns true if the given file is a TTY.
func IsTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	// Check if it's a character device (terminal)
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// IsInteractive returns true if both stdin and stderr are TTYs.
// This is the condition required for interactive prompts.
func IsInteractive() bool {
	return IsTTY(os.Stdin) && IsTTY(os.Stderr)
}
