// Package checkpoint implements the daemon-owned checkpoint engine for sandboxes.
// Checkpoints are per-invocation snapshots stored as private git refs.
package checkpoint

import (
	"time"
)

// SchemaVersion is the current schema version for checkpoints.json.
const SchemaVersion = "1.1"

// maxCheckpoints is the maximum number of checkpoints retained per invocation.
const maxCheckpoints = 200

// RefPrefix is the namespace prefix for checkpoint refs.
const RefPrefix = "refs/agency/snapshots/"

// restoreBackupRefPrefix stores pre-apply HEAD backups for recovery/audit.
const restoreBackupRefPrefix = "refs/agency/restore-backups/"

// Checkpoint represents a single checkpoint entry.
type Checkpoint struct {
	// ID is the checkpoint number (1-indexed, monotonically increasing).
	ID int `json:"id"`

	// SnapshotRef is the full git ref path (e.g., refs/agency/snapshots/<invocation_id>/1).
	SnapshotRef string `json:"snapshot_ref"`

	// SnapshotCommit is the SHA of the snapshot commit object.
	SnapshotCommit string `json:"snapshot_commit"`

	// SandboxHeadSHA is the HEAD of the sandbox branch at snapshot time.
	// Records what the snapshot is relative to.
	SandboxHeadSHA string `json:"sandbox_head_sha"`

	// CreatedAt is the timestamp when the checkpoint was created (RFC3339).
	CreatedAt string `json:"created_at"`

	// IncludesUntracked indicates whether untracked files were included.
	// False if checkpoint was degraded due to denylisted files or --no-include-untracked.
	IncludesUntracked bool `json:"includes_untracked"`

	// Diffstat is a human-readable summary of changes (e.g., "+42 -15 in 3 files").
	Diffstat string `json:"diffstat"`

	// TreeSHA is the SHA of the git tree object for this snapshot.
	// Used to detect duplicate checkpoints with identical content.
	TreeSHA string `json:"tree_sha,omitempty"`

	// Trigger describes what caused this checkpoint.
	// Values: "tool_end", "drift", "poll", "shutdown", "manual".
	Trigger string `json:"trigger,omitempty"`

	// ToolName is the tool that completed when trigger is "tool_end".
	// Examples: "Edit", "Write", "Bash", "NotebookEdit".
	ToolName string `json:"tool_name,omitempty"`

	// StreamSeq is the stream.jsonl sequence number that triggered this checkpoint.
	StreamSeq uint64 `json:"stream_seq,omitempty"`

	// Description is a human-readable label auto-generated from trigger context.
	// Examples: "After Edit", "After Bash", "Drift checkpoint", "Final checkpoint".
	Description string `json:"description,omitempty"`

	// ChangedPaths lists authoritative git-changed paths for this checkpoint's
	// delta interval. The interval is sandbox_head_sha->snapshot_commit for the
	// first checkpoint, then previous_snapshot_commit->snapshot_commit.
	ChangedPaths []string `json:"changed_paths,omitempty"`

	// ChangedPathCount is the total number of changed paths in the interval.
	ChangedPathCount int `json:"changed_path_count,omitempty"`

	// ChangedPathTruncated indicates ChangedPaths is a bounded preview.
	ChangedPathTruncated bool `json:"changed_path_truncated,omitempty"`
}

// CheckpointsFile represents the checkpoints.json structure.
type CheckpointsFile struct {
	// SchemaVersion is the schema version string (e.g., "1.1").
	SchemaVersion string `json:"schema_version"`

	// Checkpoints is the ordered list of checkpoints (oldest first).
	Checkpoints []Checkpoint `json:"checkpoints"`
}

// NewCheckpointsFile creates a new empty CheckpointsFile.
func NewCheckpointsFile() *CheckpointsFile {
	return &CheckpointsFile{
		SchemaVersion: SchemaVersion,
		Checkpoints:   []Checkpoint{},
	}
}

// nextID returns the next checkpoint ID (1 if no checkpoints exist).
func (f *CheckpointsFile) nextID() int {
	if len(f.Checkpoints) == 0 {
		return 1
	}
	return f.Checkpoints[len(f.Checkpoints)-1].ID + 1
}

// findByID returns the checkpoint with the given ID, or nil if not found.
func (f *CheckpointsFile) findByID(id int) *Checkpoint {
	for i := range f.Checkpoints {
		if f.Checkpoints[i].ID == id {
			return &f.Checkpoints[i]
		}
	}
	return nil
}

// eventKind is the type of checkpoint event.
type eventKind string

const (
	// eventKindCheckpointCreated indicates a checkpoint was successfully created.
	eventKindCheckpointCreated eventKind = "agency.checkpoint_created"

	// eventKindCheckpointFailed indicates checkpoint creation failed.
	eventKindCheckpointFailed eventKind = "agency.checkpoint_failed"

	// eventKindCheckpointApplyStarted indicates checkpoint apply has started.
	eventKindCheckpointApplyStarted eventKind = "agency.checkpoint_apply_started"

	// eventKindCheckpointApplied indicates a checkpoint was applied (rollback).
	eventKindCheckpointApplied eventKind = "agency.checkpoint_applied"

	// eventKindCheckpointDenylistTriggered indicates denylisted files were found.
	eventKindCheckpointDenylistTriggered eventKind = "agency.checkpoint_denylist_triggered"
)

// event represents a checkpoint-related event for events.jsonl.
type event struct {
	// SchemaVersion is always "1.0".
	SchemaVersion string `json:"schema_version"`

	// Seq is the monotonically increasing sequence number per invocation.
	Seq uint64 `json:"seq"`

	// Timestamp is when the event occurred (RFC3339).
	Timestamp string `json:"timestamp"`

	// InvocationID is the invocation this event belongs to.
	InvocationID string `json:"invocation_id"`

	// Kind is the event type (agency-prefixed).
	Kind eventKind `json:"kind"`

	// Data contains event-specific details.
	Data map[string]any `json:"data,omitempty"`
}

// checkpointCreatedData returns the data map for a checkpoint_created event.
func checkpointCreatedData(checkpointID int, includesUntracked bool, sandboxHeadSHA string) map[string]any {
	return map[string]any{
		"checkpoint_id":      checkpointID,
		"includes_untracked": includesUntracked,
		"sandbox_head_sha":   sandboxHeadSHA,
	}
}

// checkpointFailedData returns the data map for a checkpoint_failed event.
func checkpointFailedData(reason string) map[string]any {
	return map[string]any{
		"reason": reason,
	}
}

// checkpointAppliedData returns the data map for a checkpoint_applied event.
func checkpointAppliedData(checkpointID int, snapshotCommit string) map[string]any {
	return map[string]any{
		"checkpoint_id":   checkpointID,
		"snapshot_commit": snapshotCommit,
	}
}

// checkpointApplyStartedData returns the data map for a checkpoint_apply_started event.
func checkpointApplyStartedData(checkpointID int, snapshotCommit string, rewindHead bool) map[string]any {
	return map[string]any{
		"checkpoint_id":   checkpointID,
		"snapshot_commit": snapshotCommit,
		"rewind_head":     rewindHead,
	}
}

// checkpointDenylistTriggeredData returns the data map for a checkpoint_denylist_triggered event.
func checkpointDenylistTriggeredData(excludedFiles []string) map[string]any {
	return map[string]any{
		"excluded_files": excludedFiles,
		"snapshot_type":  "tracked_only",
	}
}

// denylistPatterns are the hardcoded patterns for files that should not be included in snapshots.
// Matching is done on the base filename only using filepath.Match (glob).
var denylistPatterns = []string{
	".env",
	".env.*",
	"*.key",
	"*.pem",
	"credentials.json",
	"secrets.json",
}

// TriggerEvent is a semantic signal from the stream parser that a mutating
// tool has completed and a checkpoint should be created.
type TriggerEvent struct {
	// Kind is the trigger type (see Trigger* constants).
	Kind string

	// ToolName is the tool that completed (e.g., "Edit", "Write", "Bash").
	ToolName string

	// ToolNames lists all mutating tools in a multi-tool message turn.
	ToolNames []string

	// Seq is the stream.jsonl sequence number of the triggering event.
	Seq uint64
}

// Trigger constants for Checkpoint.Trigger field.
const (
	TriggerToolEnd  = "tool_end"
	triggerDrift    = "drift"
	triggerPoll     = "poll"
	triggerShutdown = "shutdown"
	triggerManual   = "manual"
)

// Config holds the checkpoint engine configuration.
type Config struct {
	// IncludeUntracked determines whether untracked files are included in snapshots.
	// If false, only tracked files are staged (git add -u).
	IncludeUntracked bool

	// DebounceInterval is the duration to wait after the last file change before
	// creating a drift checkpoint (fsnotify safety net).
	DebounceInterval time.Duration

	// RateLimit is the minimum duration between drift/poll checkpoints.
	// Semantic trigger checkpoints are NOT rate-limited (each tool completion is distinct).
	RateLimit time.Duration

	// PollInterval is the interval for polling-based drift checks.
	PollInterval time.Duration

	// DriftInterval is the minimum time between fsnotify-based drift checkpoints.
	// If zero, defaults to 60 seconds. Only applies when semantic triggers are active.
	DriftInterval time.Duration

	// Env overlays every git command the checkpoint engine runs.
	Env map[string]string
}

// DefaultConfig returns the default checkpoint configuration.
func DefaultConfig() Config {
	return Config{
		IncludeUntracked: true,
		DebounceInterval: 3 * time.Second,
		RateLimit:        10 * time.Second,
		PollInterval:     30 * time.Second,
		DriftInterval:    60 * time.Second,
	}
}
