// Package daemon implements the agency daemon supervisor for headless invocations.
package daemon

import (
	"context"
	"sync/atomic"

	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
)

// APIVersion is the current API version. Incremented on breaking changes.
const APIVersion = 1

// MaxPromptSize is the maximum allowed prompt size in bytes (256 KB).
const MaxPromptSize = 256 * 1024

// IdempotencyTTL is how long idempotency entries are retained (5 minutes).
const IdempotencyTTL = 5 * 60 // seconds

// StartHeadlessRequest is the request body for POST /invocations/{id}/start_headless (legacy PR-04).
type StartHeadlessRequest struct {
	// RepoID is the repo identifier.
	RepoID string `json:"repo_id"`

	// InvocationID is the invocation identifier (created by CLI).
	InvocationID string `json:"invocation_id"`

	// Runner is the runner type (claude, codex).
	Runner string `json:"runner"`

	// SandboxPath is the absolute path to the sandbox tree (runner CWD).
	SandboxPath string `json:"sandbox_path"`

	// Prompt is the full prompt text.
	Prompt string `json:"prompt"`

	// RunnerArgs are optional pass-through args appended after the base command.
	RunnerArgs []string `json:"runner_args,omitempty"`

	// Env are optional environment variable overrides.
	Env map[string]string `json:"env,omitempty"`
}

// ControlPlaneStartRequest is the request body for POST /invocations/start_headless (PR-05 control plane).
// This is the new endpoint where daemon creates everything.
type ControlPlaneStartRequest struct {
	// RepoRoot is the absolute path to the repository root.
	RepoRoot string `json:"repo_root"`

	// WorktreeRef is the integration worktree reference (name, id, or prefix).
	WorktreeRef string `json:"worktree_ref"`

	// Runner is the runner type (claude, codex).
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

// StartHeadlessResponse is the response body for POST /invocations/{id}/start_headless.
type StartHeadlessResponse struct {
	OK               bool      `json:"ok"`
	PID              int       `json:"pid,omitempty"`
	PGID             int       `json:"pgid,omitempty"`
	DaemonInstanceID string    `json:"daemon_instance_id,omitempty"`
	AlreadyRunning   bool      `json:"already_running,omitempty"`
	Orphaned         bool      `json:"orphaned,omitempty"`
	LogPaths         *LogPaths `json:"log_paths,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// LogPaths contains paths to log files.
type LogPaths struct {
	Raw    string `json:"raw"`
	Stderr string `json:"stderr"`
}

// StopResponse is the response body for POST /invocations/{id}/stop.
type StopResponse struct {
	OK        bool   `json:"ok"`
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// KillResponse is the response body for POST /invocations/{id}/kill.
type KillResponse struct {
	OK        bool   `json:"ok"`
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
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
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
}

// SupervisedProcess holds runtime state for a supervised headless process.
type SupervisedProcess struct {
	InvocationID          string
	RepoID                string
	IntegrationWorktreeID string // PR-06: track which worktree this invocation targets
	PID                   int
	PGID                  int
	RawLogFile            string
	StderrFile            string
	StreamLogFile         string // PR-07: path to stream.jsonl for normalized events
	Runner                string // PR-07: runner type for stream parsing
	RepoRoot              string // PR-08: repo root path for checkpoint engine

	// Parser handles stream parsing and semantic status (PR-07).
	// May be nil for headed invocations or unsupported runners.
	Parser *stream.Parser

	// CheckpointEngine manages checkpoint creation (PR-08).
	// May be nil if checkpointing is disabled.
	CheckpointEngine CheckpointEngine

	// lastOutputAt is updated in-memory on every chunk; persisted with throttling.
	lastOutputAt atomic.Int64

	// exitReason is set by handleKill/stopEscalation before sending signals.
	// waitForExit* checks this after cmd.Wait() returns to use the correct reason.
	// Possible values: "" (not set), "killed", "stopped".
	exitReason atomic.Value

	// failureReason is set alongside exitReason for the same purpose.
	failureReason atomic.Value

	// done channel is closed when the process exits.
	done chan struct{}
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

	// Success fields
	CheckpointID   int    `json:"checkpoint_id,omitempty"`
	SnapshotCommit string `json:"snapshot_commit,omitempty"`
	RestoredAt     string `json:"restored_at,omitempty"`

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

	// Success fields
	InvocationID string `json:"invocation_id,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}
