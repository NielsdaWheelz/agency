// Package daemon implements the agency daemon supervisor.
package daemon

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/relay"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
)

// APIVersion is the current API version. Incremented on breaking changes.
const APIVersion = 3

// MaxPromptSize is the maximum allowed prompt size in bytes (256 KB).
const MaxPromptSize = 256 * 1024

// IdempotencyTTL is how long idempotency entries are retained (5 minutes).
const IdempotencyTTL = 5 * 60 // seconds

// HeadedReconcileInterval is the default interval for headed invocation reconciliation.
const HeadedReconcileInterval = 3 * time.Second

// HeadedStartingGraceCount is the number of reconciliation ticks a "starting"
// invocation must be observed without a tmux session before being marked failed.
const HeadedStartingGraceCount = 2

// IdempotencyEntry tracks a recent request for idempotency.
type IdempotencyEntry struct {
	InvocationID string
	Fingerprint  string
	CreatedAt    int64 // Unix timestamp
}

// LogPaths contains paths to log files.
type LogPaths struct {
	Raw      string `json:"raw"`
	Stderr   string `json:"stderr"`
	Stream   string `json:"stream"`
	Hooks    string `json:"hooks"`
	Terminal string `json:"terminal"`
}

// WorktreeIdempotencyEntry tracks a recent worktree create request for idempotency.
type WorktreeIdempotencyEntry struct {
	WorktreeID  string
	Fingerprint string
	TreePath    string
	Branch      string
	CreatedAt   int64 // Unix timestamp
}

// HeadedIdempotencyEntry tracks a recent headed invocation request for idempotency.
type HeadedIdempotencyEntry struct {
	InvocationID string
	Fingerprint  string
	TmuxSession  string
	SandboxPath  string
	CreatedAt    int64 // Unix timestamp
}

// WorktreeMergeProcess holds runtime state for one accepted worktree merge attempt.
type WorktreeMergeProcess struct {
	RepoID     string
	WorktreeID string
	AttemptID  string
	RequestID  string
	Request    normalizedMergeRequest

	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	doneOnce sync.Once
}

// CloseDone safely closes the done channel for a worktree merge attempt.
func (p *WorktreeMergeProcess) CloseDone() {
	p.doneOnce.Do(func() { close(p.done) })
}

// SupervisedProcess holds runtime state for a supervised process (headless or headed).
type SupervisedProcess struct {
	InvocationID          string
	RepoID                string
	IntegrationWorktreeID string
	Mode                  string
	PID                   int    // Headless only
	PGID                  int    // Headless only
	TmuxSession           string // Headed only - tmux session name
	SandboxPath           string // Headless only: runner working directory
	RawLogFile            string
	StderrFile            string
	StreamLogFile         string // path to stream.jsonl for normalized events
	Runner                string // runner type for stream parsing
	RepoRoot              string // repo root path for checkpoint engine
	RunnerArgs            []string
	Env                   map[string]string
	NoIncludeUntracked    bool

	// Parser handles stream parsing and semantic status.
	// May be nil for unsupported runners.
	Parser   *stream.Parser
	ParserMu sync.Mutex

	// CheckpointEngine manages checkpoint creation.
	// May be nil if checkpointing is disabled.
	CheckpointEngine CheckpointEngine

	// Relay delivers follow-up messages to the runner.
	// StdinRelay for stdin-capable runners, ResumeRelay for session-resume runners.
	// May be nil for headed invocations.
	Relay relay.FollowUpRelay

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

// CheckpointEngine is the interface for the checkpoint engine.
// Used to allow mocking in tests.
type CheckpointEngine interface {
	Run(ctx context.Context) error
	Stop()
}

// CloseDone safely closes the done channel, ensuring it is only closed once.
func (p *SupervisedProcess) CloseDone() {
	p.doneOnce.Do(func() { close(p.done) })
}

// SetResumeSessionID stores an explicit resume session/thread identifier.
func (p *SupervisedProcess) SetResumeSessionID(sessionID string) {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return
	}
	p.resumeSessionID.Store(trimmed)
}

// GetResumeSessionID returns the stored explicit resume session/thread identifier.
func (p *SupervisedProcess) GetResumeSessionID() string {
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

// InitializeTurnTracking seeds expected/completed turn tracking for this process.
func (p *SupervisedProcess) InitializeTurnTracking() {
	p.expectedTurns.Store(1)
	p.completedTurns.Store(0)
}

// IncrementExpectedTurns records that an additional successful final is expected.
func (p *SupervisedProcess) IncrementExpectedTurns() int32 {
	if p.expectedTurns.Load() <= 0 {
		p.InitializeTurnTracking()
	}
	return p.expectedTurns.Add(1)
}

// DecrementExpectedTurns rolls back a reserved expected turn, clamped to 1.
func (p *SupervisedProcess) DecrementExpectedTurns() int32 {
	for {
		current := p.expectedTurns.Load()
		if current <= 1 {
			if current <= 0 {
				p.InitializeTurnTracking()
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

// RecordSuccessfulFinalTurn increments completed turn count and reports whether
// completion has converged for this process.
func (p *SupervisedProcess) RecordSuccessfulFinalTurn() (completed int32, expected int32, completionSatisfied bool) {
	if p.expectedTurns.Load() <= 0 {
		return 0, 0, false
	}
	completed = p.completedTurns.Add(1)
	expected = p.expectedTurns.Load()
	return completed, expected, expected > 0 && completed >= expected
}

// SuccessfulCompletionObserved reports whether all expected turns completed.
func (p *SupervisedProcess) SuccessfulCompletionObserved() bool {
	expected := p.expectedTurns.Load()
	if expected <= 0 {
		return false
	}
	return p.completedTurns.Load() >= expected
}

// TryBeginCompletionFinalize acquires the completion-finalize scheduling latch.
func (p *SupervisedProcess) TryBeginCompletionFinalize() bool {
	return p.completionFinalizePending.CompareAndSwap(false, true)
}

// EndCompletionFinalize releases the completion-finalize scheduling latch.
func (p *SupervisedProcess) EndCompletionFinalize() {
	p.completionFinalizePending.Store(false)
}
