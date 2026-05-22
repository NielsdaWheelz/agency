package stream

import (
	"github.com/NielsdaWheelz/agency/internal/runners"
)

// parseResult contains the result of parsing a single line.
type parseResult struct {
	// events are the normalized events derived from this line.
	events []*normalizedEvent
}

// adapter defines the interface for runner-specific stream parsers.
type adapter interface {
	// ParseLine parses a single JSONL line and returns normalized events.
	// Returns nil events and nil error for unknown/ignored event types.
	// Returns error only for malformed JSON.
	ParseLine(line []byte) (*parseResult, error)
}

// getAdapter returns the appropriate adapter for a runner type.
func getAdapter(runner string) adapter {
	canonical, err := runners.Canonicalize(runner)
	if err != nil {
		return nil
	}

	switch canonical {
	case runners.RunnerClaudeCode:
		return &claudeAdapter{}
	case runners.RunnerCursor:
		return &cursorAdapter{}
	case runners.RunnerCodex:
		return &codexAdapter{}
	default:
		return nil
	}
}
