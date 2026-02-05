package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchLoop_MultipleIterations(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	calls := 0

	fetchAndRender := func(w io.Writer) error {
		calls++
		_, _ = fmt.Fprintf(w, "iteration %d\n", calls)
		return nil
	}

	err := watchLoop(context.Background(), &stdout, &stderr, 10*time.Millisecond, func(d time.Duration) {}, 3, fetchAndRender)
	require.NoError(t, err)

	assert.Equal(t, 3, calls)
	output := stdout.String()
	assert.Contains(t, output, ansiHideCursor)
	assert.Contains(t, output, ansiShowCursor)
	assert.Contains(t, output, ansiClearScreen)
	assert.Contains(t, output, "iteration 3")
}

func TestWatchLoop_ErrorStopsLoop(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	calls := 0

	fetchAndRender := func(w io.Writer) error {
		calls++
		if calls >= 2 {
			return fmt.Errorf("simulated error")
		}
		_, _ = fmt.Fprintf(w, "ok\n")
		return nil
	}

	err := watchLoop(context.Background(), &stdout, &stderr, 10*time.Millisecond, func(d time.Duration) {}, 0, fetchAndRender)
	require.Error(t, err)
	assert.Equal(t, 2, calls)
	assert.Contains(t, stderr.String(), "simulated error")
}

func TestWatchLoop_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer
	calls := 0

	fetchAndRender := func(w io.Writer) error {
		calls++
		if calls >= 2 {
			cancel()
		}
		_, _ = fmt.Fprintf(w, "ok\n")
		return nil
	}

	err := watchLoop(ctx, &stdout, &stderr, 10*time.Millisecond, func(d time.Duration) {}, 0, fetchAndRender)
	require.NoError(t, err)
	assert.LessOrEqual(t, calls, 3)
}

func TestWatchLoop_DefaultInterval(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	var sleepDurations []time.Duration

	sleepFn := func(d time.Duration) {
		sleepDurations = append(sleepDurations, d)
	}

	fetchAndRender := func(w io.Writer) error {
		_, _ = fmt.Fprintln(w, "ok")
		return nil
	}

	// interval=0 should default to 500ms; loop exits before sleeping on last iteration
	err := watchLoop(context.Background(), &stdout, &stderr, 0, sleepFn, 3, fetchAndRender)
	require.NoError(t, err)

	require.Len(t, sleepDurations, 2) // 3 iterations = 2 sleeps (no sleep after final render)
	assert.Equal(t, 500*time.Millisecond, sleepDurations[0])
	assert.Equal(t, 500*time.Millisecond, sleepDurations[1])
}
