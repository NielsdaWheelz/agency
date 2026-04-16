package stream

import (
	"encoding/json"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
)

// CodexAdapter parses Codex CLI JSON output.
type CodexAdapter struct {
	// commandOutputByItemID accumulates command output fragments by item id.
	// Codex may surface partial output on started/updated/completed events.
	commandOutputByItemID map[string]string
}

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
	ID   string `json:"id,omitempty"`
	Type string `json:"type"`

	// For command_execution
	Command          string `json:"command,omitempty"`
	AggregatedOutput string `json:"aggregated_output,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	Status           string `json:"status,omitempty"`

	// For agent_message
	Text    string              `json:"text,omitempty"`
	Content []codexContentBlock `json:"content,omitempty"`

	// For file_change
	Changes []codexFileChange `json:"changes,omitempty"`
}

// codexContentBlock represents a content block in Codex output.
type codexContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type codexFileChange struct {
	Path string `json:"path,omitempty"`
	Kind string `json:"kind,omitempty"`
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
		if raw.Item != nil {
			switch raw.Item.Type {
			case "command_execution":
				events, status := a.parseCommandStart(&raw)
				result.Events = events
				result.SemanticStatus = status
			case "reasoning":
				return &ParseResult{}, nil
			default:
				unknown := newUnknownRunnerEvent(raw.Type, "unsupported_item_type", line)
				if raw.Item.Type != "" {
					unknown.Data["runner_item_type"] = raw.Item.Type
				}
				result.Events = []*NormalizedEvent{unknown}
			}
		} else {
			result.Events = []*NormalizedEvent{
				newUnknownRunnerEvent(raw.Type, "missing_item", line),
			}
		}

	case "item.updated":
		if raw.Item != nil {
			switch raw.Item.Type {
			case "command_execution":
				events, status := a.parseCommandStart(&raw)
				result.Events = events
				result.SemanticStatus = status
			case "reasoning":
				return &ParseResult{}, nil
			default:
				unknown := newUnknownRunnerEvent(raw.Type, "unsupported_item_type", line)
				if raw.Item.Type != "" {
					unknown.Data["runner_item_type"] = raw.Item.Type
				}
				result.Events = []*NormalizedEvent{unknown}
			}
		} else {
			result.Events = []*NormalizedEvent{
				newUnknownRunnerEvent(raw.Type, "missing_item", line),
			}
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
			case "file_change":
				events, status := a.parseFileChange(&raw)
				result.Events = events
				result.SemanticStatus = status
			case "reasoning":
				// Ignore reasoning items - internal model thought
				return &ParseResult{}, nil
			default:
				unknown := newUnknownRunnerEvent(raw.Type, "unsupported_item_type", line)
				if raw.Item.Type != "" {
					unknown.Data["runner_item_type"] = raw.Item.Type
				}
				result.Events = []*NormalizedEvent{unknown}
			}
		} else {
			result.Events = []*NormalizedEvent{
				newUnknownRunnerEvent(raw.Type, "missing_item", line),
			}
		}

	case "turn.completed":
		result.Events = a.parseTurnCompleted(&raw)

	default:
		result.Events = []*NormalizedEvent{
			newUnknownRunnerEvent(raw.Type, "unsupported_event_type", line),
		}
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
	event.Data["action_family"] = ActionFamilyCommandExecution

	if raw.Item.Command != "" {
		event.Data["command"] = raw.Item.Command
	}
	if itemID := strings.TrimSpace(raw.Item.ID); itemID != "" {
		event.Data["tool_id"] = itemID
	}
	output := raw.Item.AggregatedOutput
	if strings.TrimSpace(raw.Item.ID) != "" {
		a.mergeCommandOutput(raw.Item.ID, raw.Item.AggregatedOutput)
		output = a.commandOutput(raw.Item.ID)
	}
	setOutputPreview(event.Data, output)

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
	event.Data["action_family"] = ActionFamilyCommandExecution

	if raw.Item.Command != "" {
		event.Data["command"] = raw.Item.Command
	}
	if raw.Item.ExitCode != nil {
		event.Data["exit_code"] = *raw.Item.ExitCode
	}
	if itemID := strings.TrimSpace(raw.Item.ID); itemID != "" {
		event.Data["tool_id"] = itemID
	}
	output := raw.Item.AggregatedOutput
	if strings.TrimSpace(raw.Item.ID) != "" {
		a.mergeCommandOutput(raw.Item.ID, raw.Item.AggregatedOutput)
		output = a.commandOutput(raw.Item.ID)
		a.clearCommandOutput(raw.Item.ID)
	}
	setOutputPreview(event.Data, output)

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
	event.Data["message_family"] = MessageFamilyAssistant

	// Extract text from content blocks
	if raw.Item != nil {
		var textParts []string
		if text := strings.TrimSpace(raw.Item.Text); text != "" {
			textParts = append(textParts, text)
		}
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

	// Agent message (final) -> ready
	status := runnerstatus.StatusReady
	return []*NormalizedEvent{event}, &status
}

// parseFileChange handles item.completed file_change events.
func (a *CodexAdapter) parseFileChange(raw *codexRawEvent) ([]*NormalizedEvent, *runnerstatus.Status) {
	event := &NormalizedEvent{
		Kind: EventKindToolEnd,
		Data: make(map[string]interface{}),
	}
	event.Data["name"] = "FileChange"
	event.Data["action_family"] = ActionFamilyFileChange

	if raw.Item != nil && len(raw.Item.Changes) > 0 {
		paths := make([]string, 0, len(raw.Item.Changes))
		kinds := make([]string, 0, len(raw.Item.Changes))
		for _, change := range raw.Item.Changes {
			if path := strings.TrimSpace(change.Path); path != "" {
				paths = append(paths, path)
			}
			if kind := strings.TrimSpace(change.Kind); kind != "" {
				kinds = append(kinds, kind)
			}
		}
		if len(paths) > 0 {
			event.Data["changed_paths"] = paths
			event.Data["change_count"] = len(paths)
		}
		if len(kinds) > 0 {
			event.Data["change_kinds"] = kinds
		}
	}

	status := runnerstatus.StatusWorking
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

func (a *CodexAdapter) ensureCommandOutputState() {
	if a.commandOutputByItemID == nil {
		a.commandOutputByItemID = make(map[string]string)
	}
}

func (a *CodexAdapter) mergeCommandOutput(itemID, output string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" || output == "" {
		return
	}
	a.ensureCommandOutputState()
	a.commandOutputByItemID[itemID] = mergeCodexCommandOutput(a.commandOutputByItemID[itemID], output)
}

func (a *CodexAdapter) commandOutput(itemID string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return ""
	}
	return a.commandOutputByItemID[itemID]
}

func (a *CodexAdapter) clearCommandOutput(itemID string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" || a.commandOutputByItemID == nil {
		return
	}
	delete(a.commandOutputByItemID, itemID)
}

func mergeCodexCommandOutput(previous, current string) string {
	if previous == "" {
		return current
	}
	if current == "" {
		return previous
	}
	if strings.HasPrefix(current, previous) {
		return current
	}
	if strings.HasPrefix(previous, current) {
		return previous
	}
	return previous + current
}

func setOutputPreview(data map[string]interface{}, output string) {
	if data == nil || output == "" {
		return
	}
	preview := output
	if len(preview) > 2048 {
		preview = preview[:2048]
		data["output_truncated"] = true
	}
	data["output_preview"] = preview
}
