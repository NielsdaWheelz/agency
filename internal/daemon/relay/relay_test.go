package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- StdinRelay Tests ---

func TestStdinRelay_Send_WritesJSONLToWriter(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	defer func() { _ = pr.Close() }()

	r := NewStdinRelay(pw, runners.RunnerClaudeCode)
	defer func() { _ = r.Close() }()

	// Send in a goroutine since pipe writes block until read.
	go func() {
		err := r.Send(context.Background(), "hello agent")
		require.NoError(t, err)
	}()

	// Read the line.
	buf := make([]byte, 4096)
	n, err := pr.Read(buf)
	require.NoError(t, err)
	line := string(buf[:n])

	// Must end with newline (JSONL protocol).
	assert.True(t, strings.HasSuffix(line, "\n"), "must end with newline")

	// Must be valid JSON.
	trimmed := strings.TrimSpace(line)
	var parsed claudeStyleMessage
	require.NoError(t, json.Unmarshal([]byte(trimmed), &parsed))
	assert.Equal(t, "user", parsed.Type)
	assert.Equal(t, "hello agent", parsed.Message.Content[0].Text)
}

func TestStdinRelay_Send_MultipleMessages(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	r := NewStdinRelay(&nopWriteCloser{Writer: &buf}, runners.RunnerAmp)
	defer func() { _ = r.Close() }()

	require.NoError(t, r.Send(context.Background(), "message 1"))
	require.NoError(t, r.Send(context.Background(), "message 2"))
	require.NoError(t, r.Send(context.Background(), "message 3"))

	// Each message is a separate JSONL line.
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 3)

	for i, line := range lines {
		var parsed claudeStyleMessage
		require.NoError(t, json.Unmarshal([]byte(line), &parsed), "line %d must be valid JSON", i)
	}
}

func TestStdinRelay_Send_AfterClose_ReturnsError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewStdinRelay(&nopWriteCloser{Writer: &buf}, runners.RunnerClaudeCode)

	require.NoError(t, r.Close())

	err := r.Send(context.Background(), "too late")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRelayClosed)
}

func TestStdinRelay_Send_BrokenPipe_ReturnsDeliveryError(t *testing.T) {
	t.Parallel()

	w := &failWriter{err: errors.New("broken pipe")}
	r := NewStdinRelay(w, runners.RunnerClaudeCode)

	err := r.Send(context.Background(), "hello")
	require.Error(t, err)

	var de *DeliveryError
	assert.True(t, errors.As(err, &de), "must be a DeliveryError")
	assert.Contains(t, de.Error(), "broken pipe")
}

func TestStdinRelay_Close_Idempotent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewStdinRelay(&nopWriteCloser{Writer: &buf}, runners.RunnerClaudeCode)

	require.NoError(t, r.Close())
	require.NoError(t, r.Close()) // second close is safe
}

func TestStdinRelay_Mode(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewStdinRelay(&nopWriteCloser{Writer: &buf}, runners.RunnerClaudeCode)
	assert.Equal(t, ModeStdin, r.Mode())
}

func TestStdinRelay_ConcurrentSendSafe(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	r := NewStdinRelay(&nopWriteCloser{Writer: &buf}, runners.RunnerDroid)
	defer func() { _ = r.Close() }()

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Send(context.Background(), "concurrent msg")
		}()
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 10)
	for _, line := range lines {
		assert.True(t, json.Valid([]byte(line)))
	}
}

// --- ResumeRelay Tests ---

func TestResumeRelay_Send_QueuesMessage(t *testing.T) {
	t.Parallel()

	r := NewResumeRelay(runners.RunnerCodex)
	defer func() { _ = r.Close() }()

	require.NoError(t, r.Send(context.Background(), "task 1"))
	require.NoError(t, r.Send(context.Background(), "task 2"))

	assert.Equal(t, 2, r.Pending())
}

func TestResumeRelay_Drain_ReturnsAndClearsQueue(t *testing.T) {
	t.Parallel()

	r := NewResumeRelay(runners.RunnerOpenCode)

	require.NoError(t, r.Send(context.Background(), "alpha"))
	require.NoError(t, r.Send(context.Background(), "beta"))

	msgs := r.Drain()
	require.Len(t, msgs, 2)
	assert.Equal(t, "alpha", msgs[0])
	assert.Equal(t, "beta", msgs[1])

	// Queue is now empty.
	assert.Nil(t, r.Drain())
	assert.Equal(t, 0, r.Pending())
}

func TestResumeRelay_Drain_EmptyQueue(t *testing.T) {
	t.Parallel()

	r := NewResumeRelay(runners.RunnerCodex)
	assert.Nil(t, r.Drain())
}

func TestResumeRelay_Send_AfterClose_ReturnsError(t *testing.T) {
	t.Parallel()

	r := NewResumeRelay(runners.RunnerCursor)
	require.NoError(t, r.Close())

	err := r.Send(context.Background(), "too late")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRelayClosed)
}

func TestResumeRelay_Close_Idempotent(t *testing.T) {
	t.Parallel()

	r := NewResumeRelay(runners.RunnerCodex)
	require.NoError(t, r.Close())
	require.NoError(t, r.Close())
}

func TestResumeRelay_Mode(t *testing.T) {
	t.Parallel()

	r := NewResumeRelay(runners.RunnerCodex)
	assert.Equal(t, ModeResume, r.Mode())
}

func TestResumeRelay_SessionID(t *testing.T) {
	t.Parallel()

	r := NewResumeRelay(runners.RunnerCodex)

	assert.Empty(t, r.SessionID())

	r.SetSessionID("abc-123")
	assert.Equal(t, "abc-123", r.SessionID())

	r.SetSessionID("def-456")
	assert.Equal(t, "def-456", r.SessionID())
}

func TestResumeRelay_ConcurrentSendAndDrain(t *testing.T) {
	t.Parallel()

	r := NewResumeRelay(runners.RunnerCodex)
	defer func() { _ = r.Close() }()

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Send(context.Background(), "msg")
		}()
	}
	wg.Wait()

	msgs := r.Drain()
	assert.Len(t, msgs, 20)
}

// --- Interface compliance ---

func TestStdinRelay_ImplementsChatRelay(t *testing.T) {
	t.Parallel()
	var _ ChatRelay = (*StdinRelay)(nil)
}

func TestResumeRelay_ImplementsChatRelay(t *testing.T) {
	t.Parallel()
	var _ ChatRelay = (*ResumeRelay)(nil)
}

// --- Test helpers ---

// nopWriteCloser wraps a Writer with a no-op Close.
type nopWriteCloser struct {
	io.Writer
}

func (n *nopWriteCloser) Close() error { return nil }

// failWriter always returns an error on Write.
type failWriter struct {
	err error
}

func (f *failWriter) Write([]byte) (int, error) { return 0, f.err }
func (f *failWriter) Close() error              { return nil }

// safeBuffer is a thread-safe bytes.Buffer.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
