// Package commands implements agency CLI commands.
// This file implements --watch mode for list commands (PR-B).
package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"
)

// ANSI escape sequences for watch mode.
const (
	ansiClearScreen = "\x1b[2J\x1b[H" // clear screen + cursor to home
	ansiShowCursor  = "\x1b[?25h"     // show cursor
	ansiHideCursor  = "\x1b[?25l"     // hide cursor
)

// watchLoop implements the shared --watch polling loop for list commands (PR-B).
// fetchAndRender is called on each tick; it should write output to the provided io.Writer.
// Output is buffered then flushed atomically after ANSI clear to minimize flicker.
func watchLoop(ctx context.Context, stdout, stderr io.Writer, interval time.Duration, sleepFn func(time.Duration), maxIterations int, fetchAndRender func(w io.Writer) error) error {
	if interval == 0 {
		interval = 500 * time.Millisecond
	}
	if sleepFn == nil {
		sleepFn = time.Sleep
	}

	// Hide cursor during watch
	_, _ = fmt.Fprint(stdout, ansiHideCursor)

	// Ensure cursor is shown on exit
	defer func() {
		_, _ = fmt.Fprint(stdout, ansiShowCursor)
		_, _ = fmt.Fprintln(stdout) // trailing newline so shell prompt doesn't stick
	}()

	var buf bytes.Buffer

	iterations := 0
	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		buf.Reset()

		// Render into buffer
		if err := fetchAndRender(&buf); err != nil {
			return err
		}

		// Clear screen + write atomically
		_, _ = fmt.Fprint(stdout, ansiClearScreen)
		_, _ = stdout.Write(buf.Bytes())

		iterations++
		if maxIterations > 0 && iterations >= maxIterations {
			return nil
		}

		sleepFn(interval)
	}
}
