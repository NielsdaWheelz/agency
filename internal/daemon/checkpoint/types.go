// Package checkpoint implements the daemon-owned checkpoint engine for sandboxes.
// Checkpoints are per-invocation snapshots stored as private git refs.
package checkpoint

import (
	"time"
)

// SchemaVersion is the current schema version for checkpoints.json.
const SchemaVersion = "1.0"

// MaxCheckpoints is the maximum number of checkpoints retained per invocation.
const MaxCheckpoints = 200

// RefPrefix is the namespace prefix for checkpoint refs.
const RefPrefix = "refs/agency/snapshots/"

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

// Config holds the checkpoint engine configuration.
type Config struct {
	// IncludeUntracked determines whether untracked files are included in snapshots.
	// If false, only tracked files are staged (git add -u).
	IncludeUntracked bool

	// DebounceInterval is the duration to wait after the last file change before snapshotting.
	DebounceInterval time.Duration

	// RateLimit is the minimum duration between snapshots.
	RateLimit time.Duration

	// PollInterval is the interval for the fallback polling check.
	PollInterval time.Duration
}

// DefaultConfig returns the default checkpoint configuration.
func DefaultConfig() Config {
	return Config{
		IncludeUntracked: true,
		DebounceInterval: 3 * time.Second,
		RateLimit:        10 * time.Second,
		PollInterval:     30 * time.Second,
	}
}
