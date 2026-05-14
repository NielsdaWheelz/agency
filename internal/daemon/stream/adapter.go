package stream

import (
	"github.com/NielsdaWheelz/agency/internal/runners"
)

// ParseResult contains the result of parsing a single line.
type ParseResult struct {
	// Events are the normalized events derived from this line.
	Events []*NormalizedEvent
}

// Adapter defines the interface for runner-specific stream parsers.
type Adapter interface {
	// Name returns the canonical runner name (e.g., "claude-code", "codex").
	Name() string

	// ParseLine parses a single JSONL line and returns normalized events.
	// Returns nil events and nil error for unknown/ignored event types.
	// Returns error only for malformed JSON.
	ParseLine(line []byte) (*ParseResult, error)
}

// GetAdapter returns the appropriate adapter for a runner type.
func GetAdapter(runner string) Adapter {
	capability, err := runners.Resolve(runner)
	if err != nil {
		return nil
	}

	switch capability.ID {
	case runners.RunnerClaudeCode:
		return &ClaudeAdapter{}
	case runners.RunnerCursor:
		return &CursorAdapter{}
	case runners.RunnerCodex:
		return &CodexAdapter{}
	default:
		return nil
	}
}
