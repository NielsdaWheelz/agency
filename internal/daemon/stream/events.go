// Package stream provides parsing and normalization of headless runner output.
// This file defines normalized stream events.
package stream

import "encoding/json"

// schemaVersion is the current schema version for normalized events.
const schemaVersion = "1.0"

// eventKind represents the type of normalized event.
type eventKind string

const (
	// eventKindSessionStart indicates the start of a runner session.
	eventKindSessionStart eventKind = "session_start"

	// eventKindToolStart indicates the start of a tool/command execution.
	eventKindToolStart eventKind = "tool_start"

	// eventKindToolEnd indicates the end of a tool/command execution.
	eventKindToolEnd eventKind = "tool_end"

	// eventKindMessage indicates an assistant or user message.
	eventKindMessage eventKind = "message"

	// eventKindFinal indicates the final result/outcome of the session.
	eventKindFinal eventKind = "final"

	// eventKindError indicates an error during the session.
	eventKindError eventKind = "error"

	// eventKindUsage indicates token usage information.
	eventKindUsage eventKind = "usage"

	// eventKindParseError indicates a parsing error for a raw line.
	eventKindParseError eventKind = "parse_error"

	// eventKindUnknown is a parser diagnostic for a provider event shape we
	// could not classify. The raw bytes remain in raw.jsonl, while data carries
	// the provider type, reason, and a raw JSON preview.
	eventKindUnknown eventKind = "unknown"
)

// normalizedEvent represents a normalized event written to stream.jsonl.
// All events from different runners are normalized to this schema.
type normalizedEvent struct {
	// SchemaVersion is the schema version string (e.g., "1.0").
	SchemaVersion string `json:"schema_version"`

	// Seq is a monotonic sequence number starting at 1.
	// Provides stable ordering without relying on timestamps.
	Seq uint64 `json:"seq"`

	// Timestamp is when the event was processed (RFC3339 UTC).
	Timestamp string `json:"timestamp"`

	// InvocationID is the invocation this event belongs to.
	InvocationID string `json:"invocation_id"`

	// Runner is the canonical runner id for this event stream.
	Runner string `json:"runner"`

	// Kind is the event type.
	Kind eventKind `json:"kind"`

	// Data contains runner-specific payload.
	Data map[string]interface{} `json:"data"`
}

// Marshal serializes the event to JSON bytes with newline.
func (e *normalizedEvent) Marshal() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	// Append newline for JSONL format
	return append(data, '\n'), nil
}

// SessionStartNotification is emitted by the parser when a session_start event
// carries an explicit session/thread identity.
type SessionStartNotification struct {
	// SessionID is the runner session/thread identifier used for resume turns.
	SessionID string

	// Seq is the stream.jsonl sequence number of the session_start event.
	Seq uint64
}

// FinalNotification is emitted by the parser when a final event is persisted.
// Success indicates whether the final represented successful completion.
type FinalNotification struct {
	Success bool
	Seq     uint64
}

// CheckpointNotification is emitted by the parser when a mutating tool completes.
// The daemon wiring layer converts this to a checkpoint.TriggerEvent.
// Defined here to avoid a circular dependency between stream and checkpoint packages.
type CheckpointNotification struct {
	// ToolName is the tool that completed (e.g., "Edit", "Write", "Bash").
	ToolName string

	// ToolNames lists all mutating tools in a multi-tool message (may be empty).
	ToolNames []string

	// Seq is the stream.jsonl sequence number of the triggering event.
	Seq uint64
}

// mutatingToolNames is the set of stream tool names that modify the filesystem.
var mutatingToolNames = map[string]bool{
	"Edit":         true,
	"Write":        true,
	"MultiEdit":    true,
	"NotebookEdit": true,
	"Bash":         true,
	"FileChange":   true,
}

func isMutatingToolName(name string) bool {
	return mutatingToolNames[name]
}
