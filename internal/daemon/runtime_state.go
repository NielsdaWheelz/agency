// Package daemon implements the agency daemon supervisor.
package daemon

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/relay"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
)

// APIVersion is the current API version. Incremented on breaking changes.
const APIVersion = 3

// MaxPromptSize is the maximum allowed prompt size in bytes (256 KB).
const MaxPromptSize = 256 * 1024

// idempotencyTTL is how long idempotency entries are retained (5 minutes).
const idempotencyTTL = 5 * 60 // seconds

// headedReconcileInterval is the default interval for headed invocation reconciliation.
const headedReconcileInterval = 3 * time.Second

// headedStartingGraceCount is the number of reconciliation ticks a "starting"
// invocation must be observed without a tmux session before being marked failed.
const headedStartingGraceCount = 2

// idempotencyEntry tracks a recent request for idempotency.
type idempotencyEntry struct {
	invocationID string
	fingerprint  string
	createdAt    int64 // Unix timestamp
}

// LogPaths contains paths to log files.
type LogPaths struct {
	Raw      string `json:"raw"`
	Stderr   string `json:"stderr"`
	Stream   string `json:"stream"`
	Hooks    string `json:"hooks"`
	Terminal string `json:"terminal"`
}

// worktreeIdempotencyEntry tracks a recent worktree create request for idempotency.
type worktreeIdempotencyEntry struct {
	worktreeID  string
	fingerprint string
	createdAt   int64 // Unix timestamp
}

// worktreeMergeProcess holds runtime state for one accepted worktree merge attempt.
type worktreeMergeProcess struct {
	repoID     string
	worktreeID string
	attemptID  string
	request    normalizedMergeRequest

	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	doneOnce sync.Once
}

// closeDone safely closes the done channel for a worktree merge attempt.
func (p *worktreeMergeProcess) closeDone() {
	p.doneOnce.Do(func() { close(p.done) })
}

// supervisedProcess holds runtime state for a supervised process (headless or headed).
type supervisedProcess struct {
	invocationID          string
	repoID                string
	integrationWorktreeID string
	mode                  string
	pgid                  int    // Headless only
	tmuxSession           string // Headed only - tmux session name
	sandboxPath           string // Headless only: runner working directory
	runner                string // runner type for stream parsing
	repoRoot              string // repo root path for checkpoint engine
	runnerArgs            []string
	env                   map[string]string
	noIncludeUntracked    bool

	// Parser handles stream parsing and semantic status.
	// May be nil for unsupported runners.
	parser   *stream.Parser
	parserMu sync.Mutex

	// CheckpointEngine manages checkpoint creation.
	// May be nil if checkpointing is disabled.
	checkpointEngine *checkpoint.Engine

	// Relay delivers follow-up messages to the runner.
	// StdinRelay for stdin-capable runners, ResumeRelay for session-resume runners.
	// May be nil for headed invocations.
	relay relay.FollowUpRelay

	// lastOutputAt is updated in-memory on every chunk; persisted with throttling.
	lastOutputAt atomic.Int64

	// exitReason is set by handleKill/stopEscalation before sending signals.
	// waitForExit* checks this after cmd.Wait() returns to use the correct reason.
	// Possible values: "" (not set), "killed", "stopped".
	exitReason atomic.Value

	// failureReason is set alongside exitReason for the same purpose.
	failureReason atomic.Value

	// resumeSessionID tracks the runner session/thread identifier used for
	// explicit resume turns (for resume-mode runners like codex).
	resumeSessionID atomic.Value // string

	// streamWg tracks active streaming goroutines (stdout, stderr).
	// waitForExit* must Wait() on this before setting terminal status
	// to ensure all pipe data is flushed to log files.
	streamWg sync.WaitGroup

	// done channel is closed when the process exits.
	done chan struct{}

	// doneOnce ensures done channel is only closed once.
	doneOnce sync.Once

	// expectedTurns tracks how many successful finals are required for lifecycle
	// completion in this supervised process instance.
	expectedTurns atomic.Int32

	// completedTurns tracks how many successful final events were observed.
	completedTurns atomic.Int32

	// completionFinalizePending gates scheduling of stdin completion convergence.
	completionFinalizePending atomic.Bool
}

// closeDone safely closes the done channel, ensuring it is only closed once.
func (p *supervisedProcess) closeDone() {
	p.doneOnce.Do(func() { close(p.done) })
}

// setResumeSessionID stores an explicit resume session/thread identifier.
func (p *supervisedProcess) setResumeSessionID(sessionID string) {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return
	}
	p.resumeSessionID.Store(trimmed)
}

// getResumeSessionID returns the stored explicit resume session/thread identifier.
func (p *supervisedProcess) getResumeSessionID() string {
	raw := p.resumeSessionID.Load()
	if raw == nil {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// initializeTurnTracking seeds expected/completed turn tracking for this process.
func (p *supervisedProcess) initializeTurnTracking() {
	p.expectedTurns.Store(1)
	p.completedTurns.Store(0)
}

// incrementExpectedTurns records that an additional successful final is expected.
func (p *supervisedProcess) incrementExpectedTurns() int32 {
	if p.expectedTurns.Load() <= 0 {
		p.initializeTurnTracking()
	}
	return p.expectedTurns.Add(1)
}

// decrementExpectedTurns rolls back a reserved expected turn, clamped to 1.
func (p *supervisedProcess) decrementExpectedTurns() int32 {
	for {
		current := p.expectedTurns.Load()
		if current <= 1 {
			if current <= 0 {
				p.initializeTurnTracking()
				return p.expectedTurns.Load()
			}
			return current
		}
		next := current - 1
		if p.expectedTurns.CompareAndSwap(current, next) {
			return next
		}
	}
}

// recordSuccessfulFinalTurn increments completed turn count and reports whether
// completion has converged for this process.
func (p *supervisedProcess) recordSuccessfulFinalTurn() (completed int32, expected int32, completionSatisfied bool) {
	if p.expectedTurns.Load() <= 0 {
		return 0, 0, false
	}
	completed = p.completedTurns.Add(1)
	expected = p.expectedTurns.Load()
	return completed, expected, expected > 0 && completed >= expected
}

// successfulCompletionObserved reports whether all expected turns completed.
func (p *supervisedProcess) successfulCompletionObserved() bool {
	expected := p.expectedTurns.Load()
	if expected <= 0 {
		return false
	}
	return p.completedTurns.Load() >= expected
}

// tryBeginCompletionFinalize acquires the completion-finalize scheduling latch.
func (p *supervisedProcess) tryBeginCompletionFinalize() bool {
	return p.completionFinalizePending.CompareAndSwap(false, true)
}

// endCompletionFinalize releases the completion-finalize scheduling latch.
func (p *supervisedProcess) endCompletionFinalize() {
	p.completionFinalizePending.Store(false)
}
