package stream

import (
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
)

// ParseResult contains the result of parsing a single line.
type ParseResult struct {
	// Events are the normalized events derived from this line.
	Events []*NormalizedEvent

	// SemanticStatus is the inferred semantic status from this line.
	// May be nil if no status inference is applicable.
	SemanticStatus *runnerstatus.Status
}

// Adapter defines the interface for runner-specific stream parsers.
type Adapter interface {
	// Name returns the runner name (e.g., "claude", "codex").
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
		return &ClaudeAdapter{}
	case runners.RunnerCodex:
		return &CodexAdapter{}
	default:
		return nil
	}
}
