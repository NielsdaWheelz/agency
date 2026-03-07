// Package checkpoint implements the daemon-owned checkpoint engine for sandboxes.
// Checkpoints are per-invocation snapshots stored as private git refs.
package checkpoint

import (
	"time"
)

// SchemaVersion is the current schema version for checkpoints.json.
// Bumped to 1.1 for semantic trigger metadata (Trigger, ToolName, StreamSeq, Description).
const SchemaVersion = "1.1"

// SchemaVersionLegacy is the previous schema version (accepted on read).
const SchemaVersionLegacy = "1.0"

// MaxCheckpoints is the maximum number of checkpoints retained per invocation.
const MaxCheckpoints = 200

// RefPrefix is the namespace prefix for checkpoint refs.
const RefPrefix = "refs/agency/snapshots/"

// RestoreBackupRefPrefix stores pre-apply HEAD backups for recovery/audit.
const RestoreBackupRefPrefix = "refs/agency/restore-backups/"

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

	// Trigger describes what caused this checkpoint (schema 1.1+).
	// Values: "tool_end", "drift", "poll", "shutdown", "manual".
	Trigger string `json:"trigger,omitempty"`

	// ToolName is the tool that completed when trigger is "tool_end" (schema 1.1+).
	// Examples: "Edit", "Write", "Bash", "NotebookEdit".
	ToolName string `json:"tool_name,omitempty"`

	// StreamSeq is the stream.jsonl sequence number that triggered this checkpoint (schema 1.1+).
	StreamSeq uint64 `json:"stream_seq,omitempty"`

	// Description is a human-readable label auto-generated from trigger context (schema 1.1+).
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
	// SchemaVersion is the schema version string (e.g., "1.0").
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

// NextID returns the next checkpoint ID (1 if no checkpoints exist).
func (f *CheckpointsFile) NextID() int {
	if len(f.Checkpoints) == 0 {
		return 1
	}
	return f.Checkpoints[len(f.Checkpoints)-1].ID + 1
}

// FindByID returns the checkpoint with the given ID, or nil if not found.
func (f *CheckpointsFile) FindByID(id int) *Checkpoint {
	for i := range f.Checkpoints {
		if f.Checkpoints[i].ID == id {
			return &f.Checkpoints[i]
		}
	}
	return nil
}

// EventKind is the type of checkpoint event.
type EventKind string

const (
	// EventKindCheckpointCreated indicates a checkpoint was successfully created.
	EventKindCheckpointCreated EventKind = "agency.checkpoint_created"

	// EventKindCheckpointFailed indicates checkpoint creation failed.
	EventKindCheckpointFailed EventKind = "agency.checkpoint_failed"

	// EventKindCheckpointApplyStarted indicates checkpoint apply has started.
	EventKindCheckpointApplyStarted EventKind = "agency.checkpoint_apply_started"

	// EventKindCheckpointApplied indicates a checkpoint was applied (rollback).
	EventKindCheckpointApplied EventKind = "agency.checkpoint_applied"

	// EventKindCheckpointDenylistTriggered indicates denylisted files were found.
	EventKindCheckpointDenylistTriggered EventKind = "agency.checkpoint_denylist_triggered"
)

// Event represents a checkpoint-related event for events.jsonl.
type Event struct {
	// SchemaVersion is always "1.0".
	SchemaVersion string `json:"schema_version"`

	// Seq is the monotonically increasing sequence number per invocation.
	Seq uint64 `json:"seq"`

	// Timestamp is when the event occurred (RFC3339).
	Timestamp string `json:"timestamp"`

	// InvocationID is the invocation this event belongs to.
	InvocationID string `json:"invocation_id"`

	// Kind is the event type (agency-prefixed).
	Kind EventKind `json:"kind"`

	// Data contains event-specific details.
	Data map[string]any `json:"data,omitempty"`
}

// NewEvent creates a new Event with the common fields populated.
func NewEvent(invocationID string, seq uint64, kind EventKind, data map[string]any, now time.Time) Event {
	return Event{
		SchemaVersion: "1.0",
		Seq:           seq,
		Timestamp:     now.UTC().Format(time.RFC3339),
		InvocationID:  invocationID,
		Kind:          kind,
		Data:          data,
	}
}

// ValidSchemaVersion reports whether v is a known checkpoints.json schema version.
func ValidSchemaVersion(v string) bool {
	return v == SchemaVersion || v == SchemaVersionLegacy
}

// CheckpointCreatedData returns the data map for a checkpoint_created event.
func CheckpointCreatedData(checkpointID int, includesUntracked bool, sandboxHeadSHA string) map[string]any {
	return map[string]any{
		"checkpoint_id":      checkpointID,
		"includes_untracked": includesUntracked,
		"sandbox_head_sha":   sandboxHeadSHA,
	}
}

// CheckpointFailedData returns the data map for a checkpoint_failed event.
func CheckpointFailedData(reason string) map[string]any {
	return map[string]any{
		"reason": reason,
	}
}

// CheckpointAppliedData returns the data map for a checkpoint_applied event.
func CheckpointAppliedData(checkpointID int, snapshotCommit string) map[string]any {
	return map[string]any{
		"checkpoint_id":   checkpointID,
		"snapshot_commit": snapshotCommit,
	}
}

// CheckpointApplyStartedData returns the data map for a checkpoint_apply_started event.
func CheckpointApplyStartedData(checkpointID int, snapshotCommit string, rewindHead bool) map[string]any {
	return map[string]any{
		"checkpoint_id":   checkpointID,
		"snapshot_commit": snapshotCommit,
		"rewind_head":     rewindHead,
	}
}

// CheckpointDenylistTriggeredData returns the data map for a checkpoint_denylist_triggered event.
func CheckpointDenylistTriggeredData(excludedFiles []string) map[string]any {
	return map[string]any{
		"excluded_files": excludedFiles,
		"snapshot_type":  "tracked_only",
	}
}

// DenylistPatterns are the hardcoded patterns for files that should not be included in snapshots.
// Matching is done on the base filename only using filepath.Match (glob).
var DenylistPatterns = []string{
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
	TriggerDrift    = "drift"
	TriggerPoll     = "poll"
	TriggerShutdown = "shutdown"
	TriggerManual   = "manual"
)

// MutatingTools is the set of tool names that modify the filesystem and
// should trigger a checkpoint on completion.
var MutatingTools = map[string]bool{
	"Edit":         true,
	"Write":        true,
	"MultiEdit":    true,
	"NotebookEdit": true,
	"Bash":         true,
}

// IsMutatingTool reports whether the given tool name modifies the filesystem.
func IsMutatingTool(name string) bool {
	return MutatingTools[name]
}

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

	// PollInterval is the interval for the fallback polling check.
	PollInterval time.Duration

	// DriftInterval is the minimum time between fsnotify-based drift checkpoints.
	// If zero, defaults to 60 seconds. Only applies when semantic triggers are active.
	DriftInterval time.Duration
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
