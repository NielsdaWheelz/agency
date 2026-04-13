// Package daemon implements the agency daemon supervisor for headless invocations.
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
const APIVersion = 1

// MaxPromptSize is the maximum allowed prompt size in bytes (256 KB).
const MaxPromptSize = 256 * 1024

// IdempotencyTTL is how long idempotency entries are retained (5 minutes).
const IdempotencyTTL = 5 * 60 // seconds

// HeadedReconcileInterval is the default interval for headed invocation reconciliation.
// Per PR-11 spec: default 3 seconds, configurable via constant (no CLI flag).
const HeadedReconcileInterval = 3 * time.Second

// HeadedStartingGraceCount is the number of reconciliation ticks a "starting"
// invocation must be observed without a tmux session before being marked failed.
// Per PR-11 spec: at least 2 consecutive ticks (i.e., ≥1 full tick interval after first observation).
const HeadedStartingGraceCount = 2

// ControlPlaneStartRequest is the request body for POST /invocations/start_headless (PR-05 control plane).
// This is the new endpoint where daemon creates everything.
type ControlPlaneStartRequest struct {
	// RepoRoot is the absolute path to the repository root.
	RepoRoot string `json:"repo_root"`

	// WorktreeRef is the integration worktree reference (name, id, or prefix).
	WorktreeRef string `json:"worktree_ref"`

	// Runner is the canonical runner id (claude-code, codex, amp, opencode, cursor, droid).
	Runner string `json:"runner"`

	// Prompt is the full prompt text (max 256KB).
	Prompt string `json:"prompt"`

	// InvocationName is an optional human-readable label.
	InvocationName string `json:"invocation_name,omitempty"`

	// RunnerArgs are optional pass-through args appended after the base command.
	RunnerArgs []string `json:"runner_args,omitempty"`

	// Env are optional environment variable overrides.
	Env map[string]string `json:"env,omitempty"`

	// ClientRequestID is required for idempotency (UUID format).
	ClientRequestID string `json:"client_request_id"`

	// NoIncludeUntracked excludes untracked files from checkpoint snapshots.
	// Default is false (include untracked files).
	NoIncludeUntracked bool `json:"no_include_untracked,omitempty"`
}

// ControlPlaneStartResponse is the response body for POST /invocations/start_headless (PR-05 control plane).
type ControlPlaneStartResponse struct {
	OK                      bool      `json:"ok"`
	InvocationID            string    `json:"invocation_id,omitempty"`
	SandboxPath             string    `json:"sandbox_path,omitempty"`
	RepoID                  string    `json:"repo_id,omitempty"`
	IntegrationWorktreeID   string    `json:"integration_worktree_id,omitempty"`
	IntegrationWorktreeName string    `json:"integration_worktree_name,omitempty"`
	PID                     int       `json:"pid,omitempty"`
	PGID                    int       `json:"pgid,omitempty"`
	DaemonInstanceID        string    `json:"daemon_instance_id,omitempty"`
	AlreadyRunning          bool      `json:"already_running,omitempty"`
	LogPaths                *LogPaths `json:"log_paths,omitempty"`
	RequestID               string    `json:"request_id,omitempty"`

	// Standard response fields
	APIVersion      int    `json:"api_version"`
	BuildVersion    string `json:"build_version,omitempty"`
	ClientRequestID string `json:"client_request_id,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// ControlPlaneFollowUpPromptRequest is the request body for POST /invocations/{ref}/chat (S3 PR-02).
type ControlPlaneFollowUpPromptRequest struct {
	// Prompt is the follow-up prompt text (max 256KB).
	Prompt string `json:"prompt"`

	// ClientRequestID is required for idempotent retries.
	ClientRequestID string `json:"client_request_id"`
}

// ControlPlaneFollowUpPromptResponse is the response body for POST /invocations/{ref}/chat (S3 PR-02).
type ControlPlaneFollowUpPromptResponse struct {
	OK             bool   `json:"ok"`
	InvocationID   string `json:"invocation_id,omitempty"`
	TimelineEntry  string `json:"timeline_entry_id,omitempty"`
	AlreadyApplied bool   `json:"already_applied,omitempty"`
	DeliveryMode   string `json:"delivery_mode,omitempty"` // "delivered" (stdin), "queued" (resume), "audit_only" (no relay)
	RequestID      string `json:"request_id,omitempty"`

	// Standard response fields
	APIVersion      int    `json:"api_version"`
	BuildVersion    string `json:"build_version,omitempty"`
	ClientRequestID string `json:"client_request_id,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// IdempotencyEntry tracks a recent request for idempotency.
type IdempotencyEntry struct {
	InvocationID string
	CreatedAt    int64 // Unix timestamp
}

// LogPaths contains paths to log files.
type LogPaths struct {
	Raw    string `json:"raw"`
	Stderr string `json:"stderr"`
	Stream string `json:"stream"`
}

// InvocationActionResponse is the shared response body for POST /invocations/{id}/stop and /kill.
type InvocationActionResponse struct {
	OK           bool   `json:"ok"`
	InvocationID string `json:"invocation_id,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	Message      string `json:"message,omitempty"`
	Hint         string `json:"hint,omitempty"`
}

// ShutdownResponse is the response body for POST /shutdown.
type ShutdownResponse struct {
	OK        bool   `json:"ok"`
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
	// RunningInvocations lists invocation IDs that are still running (when E_DAEMON_BUSY).
	RunningInvocations []string `json:"running_invocations,omitempty"`
}

// HealthResponse is the response body for GET /health.
type HealthResponse struct {
	OK               bool   `json:"ok"`
	APIVersion       int    `json:"api_version"`
	BuildVersion     string `json:"build_version"`
	GitSHA           string `json:"git_sha"`
	PID              int    `json:"pid"`
	DaemonInstanceID string `json:"daemon_instance_id"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
}

// ErrorResponse is a generic error response.
type ErrorResponse struct {
	OK        bool   `json:"ok"`
	RequestID string `json:"request_id,omitempty"`
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
}

// SupervisedProcess holds runtime state for a supervised process (headless or headed).
type SupervisedProcess struct {
	InvocationID          string
	RepoID                string
	IntegrationWorktreeID string // PR-06: track which worktree this invocation targets
	Mode                  string // PR-10: "headless" or "headed"
	PID                   int    // Headless only
	PGID                  int    // Headless only
	TmuxSession           string // PR-10: Headed only - tmux session name
	SandboxPath           string // Headless only: runner working directory
	RawLogFile            string
	StderrFile            string
	StreamLogFile         string // PR-07: path to stream.jsonl for normalized events
	Runner                string // PR-07: runner type for stream parsing
	RepoRoot              string // PR-08: repo root path for checkpoint engine
	RunnerArgs            []string
	Env                   map[string]string
	NoIncludeUntracked    bool

	// Parser handles stream parsing and semantic status (PR-07).
	// May be nil for headed invocations or unsupported runners.
	Parser *stream.Parser

	// CheckpointEngine manages checkpoint creation (PR-08).
	// May be nil if checkpointing is disabled.
	CheckpointEngine CheckpointEngine

	// Relay delivers follow-up messages to the runner.
	// StdinRelay for stdin-capable runners, ResumeRelay for session-resume runners.
	// May be nil for headed invocations.
	Relay relay.ChatRelay

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

// CheckpointEngine is the interface for the checkpoint engine.
// Used to allow mocking in tests.
type CheckpointEngine interface {
	Run(ctx context.Context) error
	Stop()
}

// ----- PR-06 Worktree Types -----

// WorktreeCreateRequest is the request body for POST /worktrees/create.
type WorktreeCreateRequest struct {
	// RepoRoot is the absolute path to the repository root.
	RepoRoot string `json:"repo_root"`

	// Name is the human-readable name (required, validated).
	Name string `json:"name"`

	// ParentBranch is the branch to branch from (optional, defaults to current branch).
	ParentBranch string `json:"parent_branch,omitempty"`

	// IdempotencyKey is an optional UUID for idempotent create (scoped to repo_id).
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// WorktreeCreateResponse is the response body for POST /worktrees/create.
type WorktreeCreateResponse struct {
	OK           bool   `json:"ok"`
	WorktreeID   string `json:"worktree_id,omitempty"`
	TreePath     string `json:"tree_path,omitempty"`
	Branch       string `json:"branch,omitempty"`
	RepoID       string `json:"repo_id,omitempty"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// WorktreeRmRequest is the request body for POST /worktrees/{id}/rm.
type WorktreeRmRequest struct {
	// Force forces removal even if the worktree is dirty or has active invocations.
	Force bool `json:"force,omitempty"`
}

// WorktreeRmResponse is the response body for POST /worktrees/{id}/rm.
type WorktreeRmResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// WorktreeIdempotencyEntry tracks a recent worktree create request for idempotency.
type WorktreeIdempotencyEntry struct {
	WorktreeID string
	TreePath   string
	Branch     string
	CreatedAt  int64 // Unix timestamp
}

// ----- PR-08 Checkpoint Types -----

// CheckpointApplyRequest is the request body for POST /invocations/{id}/checkpoints/apply.
type CheckpointApplyRequest struct {
	// CheckpointID is the checkpoint number to restore.
	CheckpointID int `json:"checkpoint_id"`
}

// CheckpointApplyResponse is the response body for POST /invocations/{id}/checkpoints/apply.
type CheckpointApplyResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	CheckpointID   int    `json:"checkpoint_id,omitempty"`
	SnapshotCommit string `json:"snapshot_commit,omitempty"`
	RestoredAt     string `json:"restored_at,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// RestartFromCheckpointRequest is the request body for POST /invocations/{ref}/restart.
// S3 PR-03: canonical invocation-scoped restart flow.
type RestartFromCheckpointRequest struct {
	// CheckpointID is the checkpoint number to restore before runner restart.
	CheckpointID int `json:"checkpoint_id"`

	// Env are optional environment variable overrides for the restarted runner.
	Env map[string]string `json:"env,omitempty"`

	// RunnerArgs are optional additional runner arguments for the restarted runner.
	RunnerArgs []string `json:"runner_args,omitempty"`
}

// RestartFromCheckpointResponse is the response body for POST /invocations/{ref}/restart.
type RestartFromCheckpointResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	InvocationID     string    `json:"invocation_id,omitempty"`
	CheckpointID     int       `json:"checkpoint_id,omitempty"`
	SnapshotCommit   string    `json:"snapshot_commit,omitempty"`
	RestoredAt       string    `json:"restored_at,omitempty"`
	PID              int       `json:"pid,omitempty"`
	PGID             int       `json:"pgid,omitempty"`
	DaemonInstanceID string    `json:"daemon_instance_id,omitempty"`
	LogPaths         *LogPaths `json:"log_paths,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// ----- PR-09 Landing Types -----

// LandingMode indicates which landing strategy was used.
type LandingMode string

const (
	// LandingModeCherryPick indicates commits were cherry-picked.
	LandingModeCherryPick LandingMode = "cherry_pick"

	// LandingModeApplyPatch indicates a patch was applied (no commits).
	LandingModeApplyPatch LandingMode = "apply_patch"
)

// LandRequest is the request body for POST /invocations/{id}/land.
type LandRequest struct {
	// Apply enables apply mode (for uncommitted changes).
	// If false and no commits exist, returns error with hint.
	Apply bool `json:"apply,omitempty"`

	// RequireBase fails if integration branch has diverged from base_commit.
	// If false (default), cherry-pick onto current HEAD.
	RequireBase bool `json:"require_base,omitempty"`
}

// LandResponse is the response body for POST /invocations/{id}/land.
type LandResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	InvocationID          string      `json:"invocation_id,omitempty"`
	AppliedMode           LandingMode `json:"applied_mode,omitempty"`
	IntegrationHeadBefore string      `json:"integration_head_before,omitempty"`
	IntegrationHeadAfter  string      `json:"integration_head_after,omitempty"`
	CommitsLanded         int         `json:"commits_landed,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode     string   `json:"error_code,omitempty"`
	Message       string   `json:"message,omitempty"`
	Hint          string   `json:"hint,omitempty"`
	ConflictFiles []string `json:"conflict_files,omitempty"`
}

// DiscardRequest is the request body for POST /invocations/{id}/discard.
type DiscardRequest struct {
	// No fields currently - discard always succeeds
	// (stopping running invocations if necessary)
}

// DiscardResponse is the response body for POST /invocations/{id}/discard.
type DiscardResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	InvocationID string `json:"invocation_id,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// WorktreePRSyncRequest is the request body for POST /worktrees/{ref}/pr/sync.
type WorktreePRSyncRequest struct {
	// AllowDirty permits PR sync when integration worktree has uncommitted changes.
	AllowDirty bool `json:"allow_dirty,omitempty"`

	// ForceWithLease uses git push --force-with-lease.
	ForceWithLease bool `json:"force_with_lease,omitempty"`
}

// ReportDiagnostic is an explicit machine-readable report contract diagnostic.
type ReportDiagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
}

// WorktreePRSyncResponse is the response body for POST /worktrees/{ref}/pr/sync.
type WorktreePRSyncResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	RepoID                string             `json:"repo_id,omitempty"`
	IntegrationWorktreeID string             `json:"integration_worktree_id,omitempty"`
	Branch                string             `json:"branch,omitempty"`
	PRNumber              int                `json:"pr_number,omitempty"`
	PRURL                 string             `json:"pr_url,omitempty"`
	PRAction              string             `json:"pr_action,omitempty"` // created|updated
	ReportSource          string             `json:"report_source,omitempty"`
	ReportDiagnostics     []ReportDiagnostic `json:"report_diagnostics,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// WorktreePRMergeRequest is the request body for POST /worktrees/{ref}/merge.
type WorktreePRMergeRequest struct {
	// Strategy selects merge strategy: squash|merge|rebase (default: squash).
	Strategy string `json:"strategy,omitempty"`

	// ConfirmationMode is the explicit confirmation contract: yes|typed.
	ConfirmationMode string `json:"confirmation_mode,omitempty"`

	// Confirmed indicates caller has already satisfied the selected confirmation mode.
	Confirmed bool `json:"confirmed,omitempty"`

	// NoDeleteBranch preserves the remote branch after merge.
	NoDeleteBranch bool `json:"no_delete_branch,omitempty"`
}

// WorktreePRMergeResponse is the response body for POST /worktrees/{ref}/merge.
type WorktreePRMergeResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	RepoID                string             `json:"repo_id,omitempty"`
	IntegrationWorktreeID string             `json:"integration_worktree_id,omitempty"`
	Branch                string             `json:"branch,omitempty"`
	PRNumber              int                `json:"pr_number,omitempty"`
	PRURL                 string             `json:"pr_url,omitempty"`
	Strategy              string             `json:"strategy,omitempty"`
	DeleteBranch          bool               `json:"delete_branch,omitempty"`
	MergeLogPath          string             `json:"merge_log_path,omitempty"`
	VerifyLogPath         string             `json:"verify_log_path,omitempty"`
	ReportSource          string             `json:"report_source,omitempty"`
	ReportDiagnostics     []ReportDiagnostic `json:"report_diagnostics,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// WorktreeUpdateRequest is the request body for POST /worktrees/{ref}/update.
type WorktreeUpdateRequest struct{}

// WorktreeUpdateResponse is the response body for POST /worktrees/{ref}/update.
type WorktreeUpdateResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	RepoID                string `json:"repo_id,omitempty"`
	IntegrationWorktreeID string `json:"integration_worktree_id,omitempty"`
	Branch                string `json:"branch,omitempty"`
	ParentBranch          string `json:"parent_branch,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// ----- PR-10 Headed Invocation Types -----

// ControlPlaneStartHeadedRequest is the request body for POST /invocations/start_headed (PR-10).
// This endpoint creates and starts a headed (tmux) invocation end-to-end.
type ControlPlaneStartHeadedRequest struct {
	// RepoRoot is the absolute path to the repository root.
	RepoRoot string `json:"repo_root"`

	// WorktreeRef is the integration worktree reference (name, id, or prefix).
	WorktreeRef string `json:"worktree_ref"`

	// Runner is the canonical runner id (claude-code, codex, amp, opencode, cursor, droid).
	Runner string `json:"runner"`

	// InvocationName is an optional human-readable label.
	InvocationName string `json:"invocation_name,omitempty"`

	// RunnerArgs are optional pass-through args appended after the base command.
	RunnerArgs []string `json:"runner_args,omitempty"`

	// Env are optional environment variable overrides.
	Env map[string]string `json:"env,omitempty"`

	// ClientRequestID is required for idempotency (UUID format).
	ClientRequestID string `json:"client_request_id"`

	// NoIncludeUntracked excludes untracked files from checkpoint snapshots.
	// Default is false (include untracked files).
	NoIncludeUntracked bool `json:"no_include_untracked,omitempty"`
}

// ControlPlaneStartHeadedResponse is the response body for POST /invocations/start_headed (PR-10).
type ControlPlaneStartHeadedResponse struct {
	OK                      bool   `json:"ok"`
	InvocationID            string `json:"invocation_id,omitempty"`
	SandboxPath             string `json:"sandbox_path,omitempty"`
	RepoID                  string `json:"repo_id,omitempty"`
	IntegrationWorktreeID   string `json:"integration_worktree_id,omitempty"`
	IntegrationWorktreeName string `json:"integration_worktree_name,omitempty"`
	TmuxSession             string `json:"tmux_session,omitempty"`
	DaemonInstanceID        string `json:"daemon_instance_id,omitempty"`
	AlreadyRunning          bool   `json:"already_running,omitempty"`
	RequestID               string `json:"request_id,omitempty"`

	// Standard response fields
	APIVersion      int    `json:"api_version"`
	BuildVersion    string `json:"build_version,omitempty"`
	GitSHA          string `json:"git_sha,omitempty"`
	ClientRequestID string `json:"client_request_id,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// HeadedIdempotencyEntry tracks a recent headed invocation request for idempotency.
type HeadedIdempotencyEntry struct {
	InvocationID string
	TmuxSession  string
	SandboxPath  string
	CreatedAt    int64 // Unix timestamp
}

// ----- PR-A Repo Registry Types -----

// RepoRegisterRequest is the request body for POST /repos/register.
type RepoRegisterRequest struct {
	// RepoRoot is the absolute path to the repository root (or a subdirectory).
	// Daemon normalizes to git toplevel.
	RepoRoot string `json:"repo_root"`
}

// RepoRegisterResponse is the response for POST /repos/register.
type RepoRegisterResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	GitSHA       string `json:"git_sha,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Data (on success)
	Data *RepoRegisterData `json:"data,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// RepoRegisterData is the data payload for a successful register response.
type RepoRegisterData struct {
	RepoID                  string   `json:"repo_id"`
	RepoKey                 string   `json:"repo_key"`
	Paths                   []string `json:"paths"`
	PreferredRoot           string   `json:"preferred_root"`
	PreferredRootAccessible bool     `json:"preferred_root_accessible"`
	LastSeenAt              string   `json:"last_seen_at"`
}

// RepoDTO is the data transfer object for a single repo.
type RepoDTO struct {
	RepoID                  string     `json:"repo_id"`
	RepoKey                 string     `json:"repo_key"`
	Paths                   []string   `json:"paths"`
	PreferredRoot           string     `json:"preferred_root"`
	PreferredRootAccessible bool       `json:"preferred_root_accessible"`
	Origin                  *OriginDTO `json:"origin,omitempty"`
	LastSeenAt              string     `json:"last_seen_at"`
	UpdatedAt               string     `json:"updated_at,omitempty"`
}

// OriginDTO is the origin info for a repo.
type OriginDTO struct {
	Present bool   `json:"present"`
	URL     string `json:"url,omitempty"`
	Host    string `json:"host,omitempty"`
}

// ListReposData is the data payload for GET /repos.
type ListReposData struct {
	Repos []RepoDTO `json:"repos"`
}
