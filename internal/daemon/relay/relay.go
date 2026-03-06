// Package relay provides runner-specific chat delivery for follow-up messages.
//
// Each runner has a different mechanism for multi-turn headless conversations:
//   - StdinRelay: writes JSONL user messages to the runner's stdin pipe (claude-code, amp, droid)
//   - ResumeRelay: queues messages and delivers them by spawning a new process with --resume (codex, opencode, cursor)
package relay

import (
	"context"
	"io"
	"sync"
)

// Mode describes the delivery mechanism used by a relay.
type Mode string

const (
	// ModeStdin delivers messages via the runner's stdin pipe in real time.
	ModeStdin Mode = "stdin"

	// ModeResume queues messages and delivers them by resuming the session.
	ModeResume Mode = "resume"
)

// ChatRelay delivers follow-up prompts to a running or recently-stopped agent.
type ChatRelay interface {
	// Send delivers a follow-up prompt.
	// For stdin relays, the message is written immediately.
	// For resume relays, the message is queued for delivery on next turn boundary.
	Send(ctx context.Context, prompt string) error

	// Close releases resources (closes stdin pipe, drains queue).
	// Safe to call multiple times.
	Close() error

	// Mode reports the relay mechanism for observability.
	Mode() Mode
}

// NewStdinRelay creates a relay that writes JSONL user messages to w.
// runner is the canonical runner ID used to select the correct message format.
// The caller must arrange for w to be connected to the runner process stdin.
func NewStdinRelay(w io.WriteCloser, runner string) *StdinRelay {
	return &StdinRelay{
		w:      w,
		runner: runner,
	}
}

// StdinRelay delivers follow-up messages by writing JSONL to the runner's stdin.
type StdinRelay struct {
	mu     sync.Mutex
	w      io.WriteCloser
	runner string
	closed bool
}

func (r *StdinRelay) Send(ctx context.Context, prompt string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return ErrRelayClosed
	}

	line, err := FormatStdinMessage(r.runner, prompt)
	if err != nil {
		return err
	}

	// Write with newline terminator.
	if _, err := r.w.Write(append(line, '\n')); err != nil {
		return &DeliveryError{Cause: err}
	}
	return nil
}

func (r *StdinRelay) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true
	return r.w.Close()
}

func (r *StdinRelay) Mode() Mode { return ModeStdin }

// NewResumeRelay creates a relay that queues messages for delivery via session resume.
// Messages are stored in memory and can be drained by the daemon's process exit handler.
func NewResumeRelay(runner string) *ResumeRelay {
	return &ResumeRelay{
		runner: runner,
	}
}

// ResumeRelay queues follow-up messages for delivery when the current process exits.
// The daemon's process exit handler drains the queue and spawns a resume process.
type ResumeRelay struct {
	mu        sync.Mutex
	runner    string
	queue     []string
	closed    bool
	sessionID string // captured from stream events
}

func (r *ResumeRelay) Send(_ context.Context, prompt string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return ErrRelayClosed
	}

	r.queue = append(r.queue, prompt)
	return nil
}

func (r *ResumeRelay) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true
	return nil
}

func (r *ResumeRelay) Mode() Mode { return ModeResume }

// SetSessionID records the runner's session ID, captured from stream output.
// Thread-safe; may be called from the stream parser goroutine.
func (r *ResumeRelay) SetSessionID(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = id
}

// SessionID returns the captured session ID, or empty if not yet captured.
func (r *ResumeRelay) SessionID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID
}

// Drain returns and removes all queued messages. Returns nil if queue is empty.
// Called by the daemon's process exit handler to get pending follow-ups.
func (r *ResumeRelay) Drain() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.queue) == 0 {
		return nil
	}
	out := r.queue
	r.queue = nil
	return out
}

// Pending returns the number of queued messages without draining.
func (r *ResumeRelay) Pending() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queue)
}
