package stream

import (
	"encoding/json"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
)

// CodexAdapter parses Codex CLI JSON output.
type CodexAdapter struct{}

// Name returns the runner name.
func (a *CodexAdapter) Name() string {
	return "codex"
}

// codexRawEvent represents a raw event from Codex JSON output.
type codexRawEvent struct {
	Type string `json:"type"`

	// For thread.started
	ThreadID string `json:"thread_id,omitempty"`

	// For item events
	Item *codexItem `json:"item,omitempty"`

	// For turn.completed
	Usage *codexUsage `json:"usage,omitempty"`
}

// codexItem represents an item in Codex output.
type codexItem struct {
	Type string `json:"type"`

	// For command_execution
	Command  string `json:"command,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`

	// For agent_message
	Content []codexContentBlock `json:"content,omitempty"`
}

// codexContentBlock represents a content block in Codex output.
type codexContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// codexUsage represents token usage in Codex output.
type codexUsage struct {
	InputTokens  int64 `json:"input_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
}

// ParseLine parses a single JSONL line from Codex output.
func (a *CodexAdapter) ParseLine(line []byte) (*ParseResult, error) {
	var raw codexRawEvent
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, err
	}

	result := &ParseResult{}

	switch raw.Type {
	case "thread.started":
		result.Events = a.parseThreadStarted(&raw)

	case "turn.started":
		// Ignore turn.started - no useful info
		return &ParseResult{}, nil

	case "item.started":
		if raw.Item != nil && raw.Item.Type == "command_execution" {
			events, status := a.parseCommandStart(&raw)
			result.Events = events
			result.SemanticStatus = status
		}

	case "item.completed":
		if raw.Item != nil {
			switch raw.Item.Type {
			case "command_execution":
				events, status := a.parseCommandEnd(&raw)
				result.Events = events
				result.SemanticStatus = status
			case "agent_message":
				events, status := a.parseAgentMessage(&raw)
				result.Events = events
				result.SemanticStatus = status
			case "reasoning":
				// Ignore reasoning items - internal model thought
				return &ParseResult{}, nil
			}
		}

	case "turn.completed":
		result.Events = a.parseTurnCompleted(&raw)

	default:
		// Unknown event type - ignore
		return &ParseResult{}, nil
	}

	return result, nil
}

// parseThreadStarted handles thread.started events.
func (a *CodexAdapter) parseThreadStarted(raw *codexRawEvent) []*NormalizedEvent {
	event := &NormalizedEvent{
		Kind: EventKindSessionStart,
		Data: make(map[string]interface{}),
	}

	if raw.ThreadID != "" {
		event.Data["thread_id"] = raw.ThreadID
	}

	return []*NormalizedEvent{event}
}

// parseCommandStart handles item.started command_execution events.
func (a *CodexAdapter) parseCommandStart(raw *codexRawEvent) ([]*NormalizedEvent, *runnerstatus.Status) {
	event := &NormalizedEvent{
		Kind: EventKindToolStart,
		Data: make(map[string]interface{}),
	}
	event.Data["name"] = "Bash"

	if raw.Item.Command != "" {
		event.Data["command"] = raw.Item.Command
	}

	// Command execution -> working status
	status := runnerstatus.StatusWorking
	return []*NormalizedEvent{event}, &status
}

// parseCommandEnd handles item.completed command_execution events.
func (a *CodexAdapter) parseCommandEnd(raw *codexRawEvent) ([]*NormalizedEvent, *runnerstatus.Status) {
	event := &NormalizedEvent{
		Kind: EventKindToolEnd,
		Data: make(map[string]interface{}),
	}
	event.Data["name"] = "Bash"

	if raw.Item.Command != "" {
		event.Data["command"] = raw.Item.Command
	}
	if raw.Item.ExitCode != nil {
		event.Data["exit_code"] = *raw.Item.ExitCode
	}

	// Keep working status after command ends
	status := runnerstatus.StatusWorking
	return []*NormalizedEvent{event}, &status
}

// parseAgentMessage handles item.completed agent_message events.
func (a *CodexAdapter) parseAgentMessage(raw *codexRawEvent) ([]*NormalizedEvent, *runnerstatus.Status) {
	event := &NormalizedEvent{
		Kind: EventKindMessage,
		Data: make(map[string]interface{}),
	}

	event.Data["role"] = "assistant"
	event.Data["has_tool_use"] = false

	// Extract text from content blocks
	if raw.Item != nil && len(raw.Item.Content) > 0 {
		var textParts []string
		// Codex agent_message items contain text blocks only; tool use is
		// expressed via separate command_execution items.
		var enrichedBlocks []map[string]interface{}
		for _, block := range raw.Item.Content {
			if block.Type == "" {
				continue
			}
			enriched := map[string]interface{}{
				"type": block.Type,
			}
			if block.Type == "text" && block.Text != "" {
				textParts = append(textParts, block.Text)
				enriched["text"] = block.Text
			}
			enrichedBlocks = append(enrichedBlocks, enriched)
		}
		if len(enrichedBlocks) > 0 {
			event.Data["content_blocks"] = enrichedBlocks
		}
		if len(textParts) > 0 {
			event.Data["text"] = strings.Join(textParts, "\n")
		}
	}

	// Agent message (final) -> ready_for_review
	status := runnerstatus.StatusReadyForReview
	return []*NormalizedEvent{event}, &status
}

// parseTurnCompleted handles turn.completed events.
func (a *CodexAdapter) parseTurnCompleted(raw *codexRawEvent) []*NormalizedEvent {
	event := &NormalizedEvent{
		Kind: EventKindUsage,
		Data: make(map[string]interface{}),
	}

	if raw.Usage != nil {
		event.Data["input_tokens"] = raw.Usage.InputTokens
		event.Data["output_tokens"] = raw.Usage.OutputTokens
	}

	return []*NormalizedEvent{event}
}
