// Package store provides persistence for agency data.
// This file implements invocation metadata and operations (Slice 8 PR-02).
package store

import (
	"encoding/json"
	"os"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
)

// InvocationStatus represents the lifecycle status of an invocation.
type InvocationStatus string

const (
	// InvocationStatusStarting indicates the invocation is being created.
	InvocationStatusStarting InvocationStatus = "starting"

	// InvocationStatusRunning indicates the invocation is actively running.
	InvocationStatusRunning InvocationStatus = "running"

	// InvocationStatusFinished indicates the runner exited normally.
	InvocationStatusFinished InvocationStatus = "finished"

	// InvocationStatusFailed indicates the runner crashed or was killed.
	InvocationStatusFailed InvocationStatus = "failed"
)

// RunnerMode represents the execution mode of a runner.
type RunnerMode string

const (
	// RunnerModeHeaded indicates interactive tmux execution.
	RunnerModeHeaded RunnerMode = "headed"

	// RunnerModeHeadless indicates non-interactive subprocess execution.
	RunnerModeHeadless RunnerMode = "headless"
)

// LandingStatus represents the landing state of a finished invocation.
type LandingStatus string

const (
	// LandingStatusPending indicates the invocation is ready to land or discard.
	LandingStatusPending LandingStatus = "pending"

	// LandingStatusLanded indicates sandbox changes were applied to integration.
	LandingStatusLanded LandingStatus = "landed"

	// LandingStatusDiscarded indicates sandbox was deleted without landing.
	LandingStatusDiscarded LandingStatus = "discarded"
)

// InvocationMeta represents the metadata for an invocation.
// This is the canonical record for both invocation lifecycle and sandbox state.
// Persisted to meta.json in the invocation record directory.
type InvocationMeta struct {
	// SchemaVersion is the schema version string (e.g., "1.0").
	SchemaVersion string `json:"schema_version"`

	// InvocationID is the unique identifier (format: <yyyymmddhhmmss>-<4hex>).
	InvocationID string `json:"invocation_id"`

	// InvocationName is an optional human-readable label (not used for identity).
	InvocationName string `json:"invocation_name,omitempty"`

	// IntegrationWorktreeID is the target integration worktree.
	IntegrationWorktreeID string `json:"integration_worktree_id"`

	// SandboxPath is the absolute path to the sandbox tree (runner CWD).
	SandboxPath string `json:"sandbox_path"`

	// SandboxBranch is the git branch for the sandbox (agency/sandbox-<invocation_id>).
	SandboxBranch string `json:"sandbox_branch"`

	// BaseCommit is the integration branch commit at invocation start.
	BaseCommit string `json:"base_commit"`

	// Runner is the runner type (claude, codex).
	Runner string `json:"runner"`

	// Mode is the execution mode (headed, headless).
	Mode RunnerMode `json:"mode"`

	// PID is the process ID (headless only, null for headed).
	PID *int `json:"pid,omitempty"`

	// PGID is the process group ID (headless only, null for headed).
	// Persisted for stop/kill operations even after daemon restart.
	PGID *int `json:"pgid,omitempty"`

	// TmuxSession is the session name (headed only, null for headless).
	TmuxSession string `json:"tmux_session,omitempty"`

	// StartedAt is the start timestamp in RFC3339 UTC format.
	StartedAt string `json:"started_at"`

	// FinishedAt is the finish timestamp in RFC3339 UTC format (null if running).
	FinishedAt string `json:"finished_at,omitempty"`

	// Status is the lifecycle status (starting, running, finished, failed).
	Status InvocationStatus `json:"status"`

	// ExitReason describes how the invocation ended (exited, killed, stopped, start_failed, unknown).
	ExitReason string `json:"exit_reason,omitempty"`

	// FailureReason provides detailed failure reason when status is "failed".
	// Values: start_incomplete, sandbox_missing, spawn_failed, runner_exit_nonzero, killed, stopped, daemon_shutdown
	FailureReason string `json:"failure_reason,omitempty"`

	// ExitCode is the process exit code (headless only, null for headed or if running).
	ExitCode *int `json:"exit_code,omitempty"`

	// LastOutputAt is the last stdout/stderr activity timestamp in RFC3339 UTC format.
	LastOutputAt string `json:"last_output_at,omitempty"`

	// LandingStatus indicates landing state (pending, landed, discarded).
	LandingStatus LandingStatus `json:"landing_status,omitempty"`

	// PromptSource indicates how the prompt was provided (file, string, editor, interactive).
	PromptSource string `json:"prompt_source,omitempty"`

	// PromptPath is the path to the prompt file stored by daemon.
	PromptPath string `json:"prompt_path,omitempty"`

	// PromptSHA256 is the SHA-256 hash of the prompt text.
	PromptSHA256 string `json:"prompt_sha256,omitempty"`

	// StopRequestedAt is the timestamp when a graceful stop was requested via agent stop.
	StopRequestedAt string `json:"stop_requested_at,omitempty"`

	// DaemonPID is the PID of the daemon that owns this invocation (headless only).
	DaemonPID *int `json:"daemon_pid,omitempty"`

	// DaemonInstanceID is the UUID of the daemon instance supervising this invocation.
	DaemonInstanceID string `json:"daemon_instance_id,omitempty"`

	// ClaimedAt is the timestamp when the daemon took ownership.
	ClaimedAt string `json:"claimed_at,omitempty"`

	// LifecycleOwner indicates who owns lifecycle updates ("daemon" or "" for CLI-owned).
	LifecycleOwner string `json:"lifecycle_owner,omitempty"`

	// OrphanedAt is the timestamp when the invocation was marked orphaned.
	OrphanedAt string `json:"orphaned_at,omitempty"`

	// Flags contains boolean flags for operational state.
	Flags InvocationFlags `json:"flags,omitempty"`

	// SemanticStatus is the derived semantic status from stream parsing (headless only).
	// Values: working, needs_input, blocked, ready_for_review.
	// This is set by the daemon during stream parsing and is optional.
	SemanticStatus *runnerstatus.Status `json:"semantic_status,omitempty"`

	// SemanticStatusUpdatedAt is the timestamp when semantic_status was last updated.
	SemanticStatusUpdatedAt string `json:"semantic_status_updated_at,omitempty"`

	// CheckpointIncludeUntracked determines whether checkpoints include untracked files.
	// Set at invocation creation time based on CLI flag --no-include-untracked.
	// Default is true (include untracked files).
	CheckpointIncludeUntracked bool `json:"checkpoint_include_untracked"`

	// RunnerArgs stores the invocation runner arguments used at start time.
	// Restart flows use these when explicit runner args are not provided.
	RunnerArgs []string `json:"runner_args,omitempty"`

	// CustomEnvKeys stores environment key names that were provided at start time.
	// Values are intentionally not persisted to avoid storing secrets at rest.
	CustomEnvKeys []string `json:"custom_env_keys,omitempty"`
}

// InvocationFlags contains boolean flags for operational state.
type InvocationFlags struct {
	// NeedsAttention indicates user attention may be required (e.g., stop requested).
	NeedsAttention bool `json:"needs_attention,omitempty"`

	// Orphaned indicates the process was left without daemon supervision.
	// Set on daemon restart when PID is found alive but daemon_instance_id doesn't match,
	// or when PID is found dead.
	Orphaned bool `json:"orphaned,omitempty"`

	// CheckpointDegraded indicates the last checkpoint was degraded (tracked-only due to denylisted files).
	CheckpointDegraded bool `json:"checkpoint_degraded,omitempty"`
}

// NewInvocationMeta creates a new InvocationMeta with required fields set.
func NewInvocationMeta(
	invocationID string,
	invocationName string,
	integrationWorktreeID string,
	sandboxPath string,
	sandboxBranch string,
	baseCommit string,
	runner string,
	mode RunnerMode,
	startedAt time.Time,
) *InvocationMeta {
	return &InvocationMeta{
		SchemaVersion:              "1.0",
		InvocationID:               invocationID,
		InvocationName:             invocationName,
		IntegrationWorktreeID:      integrationWorktreeID,
		SandboxPath:                sandboxPath,
		SandboxBranch:              sandboxBranch,
		BaseCommit:                 baseCommit,
		Runner:                     runner,
		Mode:                       mode,
		StartedAt:                  startedAt.UTC().Format(time.RFC3339),
		Status:                     InvocationStatusStarting,
		CheckpointIncludeUntracked: true, // Default: include untracked files in checkpoints
	}
}

// EnsureInvocationDir creates the invocation record directory with exclusive semantics.
// Returns the invocation dir path on success.
// Fails with E_INVOCATION_DIR_EXISTS if the directory already exists.
func (s *Store) EnsureInvocationDir(repoID, invocationID string) (string, error) {
	invocationDir := s.InvocationDir(repoID, invocationID)

	// Ensure parent directories exist (invocations/)
	parentDir := s.InvocationsDir(repoID)
	if err := s.FS.MkdirAll(parentDir, 0o700); err != nil {
		return "", errors.WrapWithDetails(
			errors.EInvocationCreateFailed,
			"failed to create invocations directory",
			err,
			map[string]string{"dir": parentDir},
		)
	}

	// Create invocation directory with exclusive semantics using os.Mkdir
	if err := os.Mkdir(invocationDir, 0o700); err != nil {
		if os.IsExist(err) {
			return "", errors.NewWithDetails(
				errors.EInvocationDirExists,
				"invocation directory already exists (invocation_id collision or stale state)",
				map[string]string{"invocation_dir": invocationDir},
			)
		}
		return "", errors.WrapWithDetails(
			errors.EInvocationCreateFailed,
			"failed to create invocation directory",
			err,
			map[string]string{"invocation_dir": invocationDir},
		)
	}

	return invocationDir, nil
}

// EnsureSandboxDir creates the sandbox directory with exclusive semantics.
// Returns the sandbox dir path on success.
// Does NOT create the tree/ directory - that's done by git worktree add.
func (s *Store) EnsureSandboxDir(repoID, invocationID string) (string, error) {
	sandboxDir := s.SandboxDir(repoID, invocationID)

	// Ensure parent directories exist (sandboxes/)
	parentDir := s.SandboxesDir(repoID)
	if err := s.FS.MkdirAll(parentDir, 0o700); err != nil {
		return "", errors.WrapWithDetails(
			errors.ESandboxCreateFailed,
			"failed to create sandboxes directory",
			err,
			map[string]string{"dir": parentDir},
		)
	}

	// Create sandbox directory with exclusive semantics
	if err := os.Mkdir(sandboxDir, 0o700); err != nil {
		if os.IsExist(err) {
			return "", errors.NewWithDetails(
				errors.ESandboxCreateFailed,
				"sandbox directory already exists (invocation_id collision or stale state)",
				map[string]string{"sandbox_dir": sandboxDir},
			)
		}
		return "", errors.WrapWithDetails(
			errors.ESandboxCreateFailed,
			"failed to create sandbox directory",
			err,
			map[string]string{"sandbox_dir": sandboxDir},
		)
	}

	return sandboxDir, nil
}

// WriteInvocationMeta writes the meta.json for an invocation atomically.
func (s *Store) WriteInvocationMeta(repoID, invocationID string, meta *InvocationMeta) error {
	metaPath := s.InvocationMetaPath(repoID, invocationID)

	if err := fs.WriteJSONAtomic(metaPath, meta, 0o644); err != nil {
		return errors.WrapWithDetails(
			errors.EMetaWriteFailed,
			"failed to write invocation meta.json atomically",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	return nil
}

// UpdateInvocationMeta reads, updates, and writes meta.json atomically.
func (s *Store) UpdateInvocationMeta(repoID, invocationID string, updateFn func(*InvocationMeta)) error {
	metaPath := s.InvocationMetaPath(repoID, invocationID)

	// Read current meta
	meta, err := s.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		return err
	}

	// Apply update
	updateFn(meta)

	// Write back atomically
	if err := fs.WriteJSONAtomic(metaPath, meta, 0o644); err != nil {
		return errors.WrapWithDetails(
			errors.EMetaWriteFailed,
			"failed to write invocation meta.json atomically",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	return nil
}

// ReadInvocationMeta reads and parses meta.json for an invocation.
// Returns E_INVOCATION_NOT_FOUND if the meta file doesn't exist.
// Returns E_STORE_CORRUPT if the file can't be parsed.
func (s *Store) ReadInvocationMeta(repoID, invocationID string) (*InvocationMeta, error) {
	metaPath := s.InvocationMetaPath(repoID, invocationID)

	data, err := s.FS.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.NewWithDetails(
				errors.EInvocationNotFound,
				"invocation not found (meta.json does not exist)",
				map[string]string{"meta_path": metaPath},
			)
		}
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to read invocation meta.json",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	var meta InvocationMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to parse invocation meta.json",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	return &meta, nil
}

// RemoveInvocationDir removes the invocation record directory completely.
// This is used for cleanup on failed creation.
func (s *Store) RemoveInvocationDir(repoID, invocationID string) error {
	invocationDir := s.InvocationDir(repoID, invocationID)
	return os.RemoveAll(invocationDir)
}

// RemoveSandboxDir removes the sandbox directory completely.
// This is used for cleanup on failed creation or after landing/discard.
func (s *Store) RemoveSandboxDir(repoID, invocationID string) error {
	sandboxDir := s.SandboxDir(repoID, invocationID)
	return os.RemoveAll(sandboxDir)
}
