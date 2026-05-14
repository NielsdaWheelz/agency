// Package store provides persistence for agency data.
// This file implements invocation metadata and operations.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// invocationMetaLocks serializes read-modify-write updates per meta path.
var invocationMetaLocks sync.Map // map[string]*sync.Mutex

func invocationMetaLock(metaPath string) *sync.Mutex {
	lock, _ := invocationMetaLocks.LoadOrStore(invocationMetaLockKey(metaPath), &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func invocationMetaLockKey(metaPath string) string {
	clean := filepath.Clean(metaPath)
	if abs, err := filepath.Abs(clean); err == nil {
		clean = abs
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved
	}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(clean)); err == nil {
		return filepath.Join(parent, filepath.Base(clean))
	}
	return clean
}

// InvocationStatus represents the lifecycle status of an invocation.
type InvocationStatus string

const (
	// InvocationStatusStarting indicates the invocation is being created.
	InvocationStatusStarting InvocationStatus = "starting"

	// InvocationStatusRunning indicates the invocation is actively running.
	InvocationStatusRunning InvocationStatus = "running"

	// InvocationStatusStopping indicates graceful shutdown was requested and
	// terminal exit has not been observed yet.
	InvocationStatusStopping InvocationStatus = "stopping"

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
	// SchemaVersion is the store schema version string.
	SchemaVersion string `json:"schema_version"`

	// InvocationID is the unique identifier (format: <yyyymmddhhmmss>-<4hex>).
	InvocationID string `json:"invocation_id"`

	// InvocationName is an optional human-readable label (not used for identity).
	InvocationName string `json:"invocation_name,omitempty"`

	// IntegrationWorktreeID is the target integration worktree.
	IntegrationWorktreeID string `json:"integration_worktree_id"`

	// SandboxPath is the absolute path to the sandbox tree (runner CWD).
	SandboxPath string `json:"sandbox_path"`

	// CheckoutRoot is the repo-scoped root that contains Agency-managed checkouts.
	CheckoutRoot string `json:"checkout_root"`

	// ExecutionProfile is the profile label selected when this invocation was created.
	ExecutionProfile string `json:"execution_profile"`

	// SandboxBranch is the git branch for the sandbox (agency/sandbox-<invocation_id>).
	SandboxBranch string `json:"sandbox_branch"`

	// BaseCommit is the integration branch commit at invocation start.
	BaseCommit string `json:"base_commit"`

	// Runner is the canonical runner id for this invocation.
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

	// Status is the lifecycle status (starting, running, stopping, finished, failed).
	Status InvocationStatus `json:"status"`

	// ExitReason describes how the invocation ended (exited, killed, stopped, start_failed, unknown).
	ExitReason string `json:"exit_reason,omitempty"`

	// FailureReason provides detailed failure reason when status is "failed".
	// Values: start_incomplete, sandbox_missing, spawn_failed, runner_exit_nonzero, killed, stopped, daemon_shutdown, stream_write_failed
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

	// TaskID is the high-level task that created this invocation, when any.
	TaskID string `json:"task_id,omitempty"`

	// ClientRequestID is the control-plane start idempotency key, when any.
	ClientRequestID string `json:"client_request_id,omitempty"`

	// RequestFingerprint is the durable fingerprint for ClientRequestID.
	RequestFingerprint string `json:"request_fingerprint,omitempty"`
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
	checkoutRoot string,
	executionProfile string,
	sandboxBranch string,
	baseCommit string,
	runner string,
	mode RunnerMode,
	startedAt time.Time,
) *InvocationMeta {
	return &InvocationMeta{
		SchemaVersion:              SchemaVersion,
		InvocationID:               invocationID,
		InvocationName:             invocationName,
		IntegrationWorktreeID:      integrationWorktreeID,
		SandboxPath:                sandboxPath,
		CheckoutRoot:               checkoutRoot,
		ExecutionProfile:           executionProfile,
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

// EnsureInvocationLogsDir creates the invocation-owned logs directory.
func (s *Store) EnsureInvocationLogsDir(repoID, invocationID string) (string, error) {
	logsDir := s.InvocationLogsDir(repoID, invocationID)
	if err := s.FS.MkdirAll(logsDir, 0o700); err != nil {
		return "", errors.WrapWithDetails(
			errors.EInvocationCreateFailed,
			"failed to create invocation logs directory",
			err,
			map[string]string{"logs_dir": logsDir},
		)
	}
	if err := s.FS.Chmod(logsDir, 0o700); err != nil {
		return "", errors.WrapWithDetails(
			errors.EInvocationCreateFailed,
			"failed to enforce invocation logs directory permissions",
			err,
			map[string]string{"logs_dir": logsDir},
		)
	}
	return logsDir, nil
}

// EnsureInvocationRunnerStatusDir creates the invocation-owned runner status directory.
func (s *Store) EnsureInvocationRunnerStatusDir(repoID, invocationID string) (string, error) {
	statusDir := filepath.Dir(s.InvocationRunnerStatusPath(repoID, invocationID))
	if err := s.FS.MkdirAll(statusDir, 0o700); err != nil {
		return "", errors.WrapWithDetails(
			errors.EInvocationCreateFailed,
			"failed to create invocation runner status directory",
			err,
			map[string]string{"status_dir": statusDir},
		)
	}
	if err := s.FS.Chmod(statusDir, 0o700); err != nil {
		return "", errors.WrapWithDetails(
			errors.EInvocationCreateFailed,
			"failed to enforce invocation runner status directory permissions",
			err,
			map[string]string{"status_dir": statusDir},
		)
	}
	return statusDir, nil
}

// PrepareInvocationLogPath ensures the invocation-owned logs directory exists
// before returning the canonical path.
func (s *Store) PrepareInvocationLogPath(repoID, invocationID, kind string) (string, error) {
	if _, err := s.EnsureInvocationLogsDir(repoID, invocationID); err != nil {
		return "", err
	}
	switch kind {
	case "stderr":
		return s.InvocationStderrLogPath(repoID, invocationID), nil
	case "stream":
		return s.InvocationStreamLogPath(repoID, invocationID), nil
	case "hooks":
		return s.InvocationHooksLogPath(repoID, invocationID), nil
	case "terminal":
		return s.InvocationTerminalLogPath(repoID, invocationID), nil
	default:
		return s.InvocationRawLogPath(repoID, invocationID), nil
	}
}

// WriteInvocationMeta writes the meta.json for an invocation atomically.
func (s *Store) WriteInvocationMeta(repoID, invocationID string, meta *InvocationMeta) error {
	metaPath := s.InvocationMetaPath(repoID, invocationID)

	if err := fs.WriteJSONAtomic(metaPath, meta, 0o600); err != nil {
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
	lock := invocationMetaLock(metaPath)
	lock.Lock()
	defer lock.Unlock()

	// Read current meta
	meta, err := s.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		return err
	}

	// Apply update
	updateFn(meta)

	// Write back atomically
	if err := fs.WriteJSONAtomic(metaPath, meta, 0o600); err != nil {
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
	if err := decodeStrictJSON(data, &meta); err != nil {
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to parse invocation meta.json",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}
	fields, err := strictJSONObjectFields(data)
	if err != nil {
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to parse invocation meta.json",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}
	if err := validateInvocationMeta(meta, invocationID, metaPath, fields); err != nil {
		return nil, err
	}

	return &meta, nil
}

func validateInvocationMeta(meta InvocationMeta, invocationID, metaPath string, fields map[string]json.RawMessage) error {
	if meta.SchemaVersion == "" {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"invocation meta.json missing schema_version",
			map[string]string{"meta_path": metaPath},
		)
	}
	if meta.SchemaVersion != SchemaVersion {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"invocation meta.json has unsupported schema_version",
			map[string]string{
				"meta_path":       metaPath,
				"schema_version":  meta.SchemaVersion,
				"expected_schema": SchemaVersion,
			},
		)
	}
	if meta.InvocationID == "" || meta.IntegrationWorktreeID == "" || meta.SandboxPath == "" ||
		meta.CheckoutRoot == "" || meta.ExecutionProfile == "" || meta.SandboxBranch == "" ||
		meta.BaseCommit == "" || meta.Runner == "" || meta.Mode == "" || meta.StartedAt == "" || meta.Status == "" {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"invocation meta.json missing required fields",
			map[string]string{"meta_path": metaPath},
		)
	}
	if _, ok := fields["checkpoint_include_untracked"]; !ok {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"invocation meta.json missing checkpoint_include_untracked",
			map[string]string{"meta_path": metaPath},
		)
	}
	if meta.InvocationID != invocationID {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"invocation meta.json id does not match record path",
			map[string]string{
				"meta_path":          metaPath,
				"path_invocation_id": invocationID,
				"meta_invocation_id": meta.InvocationID,
			},
		)
	}
	if !filepath.IsAbs(meta.SandboxPath) || !filepath.IsAbs(meta.CheckoutRoot) || (meta.PromptPath != "" && !filepath.IsAbs(meta.PromptPath)) {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"invocation meta.json paths must be absolute",
			map[string]string{"meta_path": metaPath},
		)
	}
	if !validRunnerMode(meta.Mode) {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"invocation meta.json has unsupported mode",
			map[string]string{
				"meta_path": metaPath,
				"mode":      string(meta.Mode),
			},
		)
	}
	if !validInvocationStatus(meta.Status) {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"invocation meta.json has unsupported status",
			map[string]string{
				"meta_path": metaPath,
				"status":    string(meta.Status),
			},
		)
	}
	if meta.LandingStatus != "" && !validLandingStatus(meta.LandingStatus) {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"invocation meta.json has unsupported landing_status",
			map[string]string{
				"meta_path":      metaPath,
				"landing_status": string(meta.LandingStatus),
			},
		)
	}
	return nil
}

func validInvocationStatus(status InvocationStatus) bool {
	switch status {
	case InvocationStatusStarting, InvocationStatusRunning, InvocationStatusStopping, InvocationStatusFinished, InvocationStatusFailed:
		return true
	default:
		return false
	}
}

func validLandingStatus(status LandingStatus) bool {
	switch status {
	case LandingStatusPending, LandingStatusLanded, LandingStatusDiscarded:
		return true
	default:
		return false
	}
}

// RemoveInvocationDir removes the invocation record directory completely.
// This is used for cleanup on failed creation.
func (s *Store) RemoveInvocationDir(repoID, invocationID string) error {
	invocationDir := s.InvocationDir(repoID, invocationID)
	return fs.SafeRemoveAll(invocationDir, s.DataDir)
}
