// Package relay provides runner-specific delivery for follow-up messages.
//
// Each runner has a different mechanism for multi-turn headless conversations:
//   - StdinRelay: writes JSONL user messages to the runner's stdin pipe (claude-code, amp, droid)
//   - ResumeRelay: queues messages for session-resume delivery (codex and other resume-style runners)
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

// FollowUpRelay delivers user prompts to a running or recently-stopped agent.
type FollowUpRelay interface {
	// Send delivers a user prompt.
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

// StdinRelay delivers user messages by writing JSONL to the runner's stdin.
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
func NewResumeRelay() *ResumeRelay {
	return &ResumeRelay{}
}

// ResumeRelay queues follow-up messages for delivery when the current process exits.
// Runners with configured resume templates are restarted by the daemon with the queued prompt.
type ResumeRelay struct {
	mu     sync.Mutex
	queue  []string
	closed bool
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
