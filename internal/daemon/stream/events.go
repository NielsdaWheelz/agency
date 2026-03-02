// Package stream provides parsing and normalization of headless runner output.
// This implements PR-07: Stream Parsing, Normalized Events, and Semantic Status.
package stream

import (
	"encoding/json"
	"time"
)

// SchemaVersion is the current schema version for normalized events.
const SchemaVersion = "1.0"

// EventKind represents the type of normalized event.
type EventKind string

const (
	// EventKindSessionStart indicates the start of a runner session.
	EventKindSessionStart EventKind = "session_start"

	// EventKindToolStart indicates the start of a tool/command execution.
	EventKindToolStart EventKind = "tool_start"

	// EventKindToolEnd indicates the end of a tool/command execution.
	EventKindToolEnd EventKind = "tool_end"

	// EventKindMessage indicates an assistant or user message.
	EventKindMessage EventKind = "message"

	// EventKindFinal indicates the final result/outcome of the session.
	EventKindFinal EventKind = "final"

	// EventKindError indicates an error during the session.
	EventKindError EventKind = "error"

	// EventKindUsage indicates token usage information.
	EventKindUsage EventKind = "usage"

	// EventKindStatus indicates a status update (derived from runner signals).
	EventKindStatus EventKind = "status"

	// EventKindParseError indicates a parsing error for a raw line.
	EventKindParseError EventKind = "parse_error"
)

// NormalizedEvent represents a normalized event written to stream.jsonl.
// All events from different runners are normalized to this schema.
type NormalizedEvent struct {
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
	Kind EventKind `json:"kind"`

	// Data contains runner-specific payload.
	Data map[string]interface{} `json:"data"`
}

// NewNormalizedEvent creates a new normalized event with common fields filled in.
func NewNormalizedEvent(seq uint64, invocationID, runner string, kind EventKind, now time.Time) *NormalizedEvent {
	return &NormalizedEvent{
		SchemaVersion: SchemaVersion,
		Seq:           seq,
		Timestamp:     now.UTC().Format(time.RFC3339),
		InvocationID:  invocationID,
		Runner:        runner,
		Kind:          kind,
		Data:          make(map[string]interface{}),
	}
}

// Marshal serializes the event to JSON bytes with newline.
func (e *NormalizedEvent) Marshal() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	// Append newline for JSONL format
	return append(data, '\n'), nil
}

// SessionStartData contains data for session_start events.
type SessionStartData struct {
	CWD       string `json:"cwd,omitempty"`
	Model     string `json:"model,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// MessageData contains data for message events.
type MessageData struct {
	HasToolUse bool     `json:"has_tool_use"`
	ToolNames  []string `json:"tool_names,omitempty"`
	Text       string   `json:"text,omitempty"`
	Role       string   `json:"role,omitempty"`
}

// ToolStartData contains data for tool_start events.
type ToolStartData struct {
	Command string `json:"command,omitempty"`
	ToolID  string `json:"tool_id,omitempty"`
	Name    string `json:"name,omitempty"`
}

// ToolEndData contains data for tool_end events.
type ToolEndData struct {
	Command  string `json:"command,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	ToolID   string `json:"tool_id,omitempty"`
	Name     string `json:"name,omitempty"`
}

// FinalData contains data for final events.
type FinalData struct {
	DurationMS *int64           `json:"duration_ms,omitempty"`
	CostUSD    *float64         `json:"cost_usd,omitempty"`
	Usage      map[string]int64 `json:"usage,omitempty"`
	Success    bool             `json:"success"`
}

// ErrorData contains data for error events.
type ErrorData struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// UsageData contains data for usage events.
type UsageData struct {
	InputTokens  int64 `json:"input_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
}

// ParseErrorData contains data for parse_error events.
type ParseErrorData struct {
	ParseErrorCount int    `json:"parse_error_count"`
	Reason          string `json:"reason,omitempty"`
}
