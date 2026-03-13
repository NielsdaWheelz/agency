package stream

import (
	"encoding/json"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
)

// ClaudeAdapter parses Claude CLI stream-json output.
type ClaudeAdapter struct{}

// Name returns the runner name.
func (a *ClaudeAdapter) Name() string {
	return "claude"
}

// claudeRawEvent represents a raw event from Claude stream-json output.
type claudeRawEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// For system:init
	CWD       string `json:"cwd,omitempty"`
	Model     string `json:"model,omitempty"`
	SessionID string `json:"session_id,omitempty"`

	// For assistant/user messages
	Message *claudeMessage `json:"message,omitempty"`

	// Some tool results are also surfaced as a top-level structured field.
	ToolUseResult interface{} `json:"tool_use_result,omitempty"`

	// For result events
	Result        string       `json:"result,omitempty"`
	ErrorMessage  string       `json:"error,omitempty"`
	ErrorCode     string       `json:"error_code,omitempty"`
	DurationMS    *int64       `json:"duration_ms,omitempty"`
	DurationAPIMS *int64       `json:"duration_api_ms,omitempty"`
	CostUSD       *float64     `json:"cost_usd,omitempty"`
	TotalCostUSD  *float64     `json:"total_cost_usd,omitempty"`
	Usage         *claudeUsage `json:"usage,omitempty"`
}

// claudeMessage represents a message in Claude output.
type claudeMessage struct {
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

// claudeUsage represents token usage in Claude output.
type claudeUsage struct {
	InputTokens  int64 `json:"input_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
}

// claudeContentBlock represents a content block (text or tool_use).
type claudeContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// ParseLine parses a single JSONL line from Claude output.
func (a *ClaudeAdapter) ParseLine(line []byte) (*ParseResult, error) {
	var raw claudeRawEvent
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, err
	}

	result := &ParseResult{}

	switch raw.Type {
	case "system":
		if raw.Subtype == "init" {
			result.Events = a.parseSystemInit(&raw)
		} else {
			result.Events = []*NormalizedEvent{
				newUnknownRunnerEvent(raw.Type, "unsupported_system_subtype", line),
			}
		}

	case "assistant":
		events, status := a.parseAssistant(&raw)
		result.Events = events
		result.SemanticStatus = status

	case "user":
		result.Events = a.parseUser(&raw)

	case "result":
		events, status := a.parseResult(&raw)
		result.Events = events
		result.SemanticStatus = status

	default:
		result.Events = []*NormalizedEvent{
			newUnknownRunnerEvent(raw.Type, "unsupported_event_type", line),
		}
	}

	return result, nil
}

// parseSystemInit handles system:init events.
func (a *ClaudeAdapter) parseSystemInit(raw *claudeRawEvent) []*NormalizedEvent {
	event := &NormalizedEvent{
		Kind: EventKindSessionStart,
		Data: make(map[string]interface{}),
	}

	if raw.CWD != "" {
		event.Data["cwd"] = raw.CWD
	}
	if raw.Model != "" {
		event.Data["model"] = raw.Model
	}
	if raw.SessionID != "" {
		event.Data["session_id"] = raw.SessionID
	}

	return []*NormalizedEvent{event}
}

// parseAssistant handles assistant message events.
func (a *ClaudeAdapter) parseAssistant(raw *claudeRawEvent) ([]*NormalizedEvent, *runnerstatus.Status) {
	event := &NormalizedEvent{
		Kind: EventKindMessage,
		Data: make(map[string]interface{}),
	}

	event.Data["role"] = "assistant"
	event.Data["has_tool_use"] = false
	event.Data["message_family"] = MessageFamilyAssistant

	if raw.Message != nil && len(raw.Message.Content) > 0 {
		// Parse content blocks
		var contentBlocks []claudeContentBlock
		if err := json.Unmarshal(raw.Message.Content, &contentBlocks); err != nil {
			// Content might be a string instead of array
			var textContent string
			if err := json.Unmarshal(raw.Message.Content, &textContent); err == nil {
				event.Data["text"] = textContent
			}
		} else {
			// Process content blocks
			var textParts []string
			var toolNames []string
			hasToolUse := false
			var enrichedBlocks []map[string]interface{}

			for _, block := range contentBlocks {
				if block.Type == "" {
					continue
				}
				enriched := map[string]interface{}{
					"type": block.Type,
				}
				switch block.Type {
				case "text":
					if block.Text != "" {
						textParts = append(textParts, block.Text)
						enriched["text"] = block.Text
					}
				case "tool_use":
					hasToolUse = true
					if block.Name != "" {
						toolNames = append(toolNames, block.Name)
						enriched["name"] = block.Name
					}
					if block.ID != "" {
						enriched["id"] = block.ID
					}
					if len(block.Input) > 0 {
						// Pre-parse to interface{} so the value round-trips
						// correctly through JSON marshal/unmarshal (json.RawMessage
						// becomes map[string]interface{} after unmarshal).
						var inputParsed interface{}
						if json.Unmarshal(block.Input, &inputParsed) == nil {
							enriched["input"] = inputParsed
						}
					}
				}
				enrichedBlocks = append(enrichedBlocks, enriched)
			}

			if len(enrichedBlocks) > 0 {
				event.Data["content_blocks"] = enrichedBlocks
			}

			if len(textParts) > 0 {
				event.Data["text"] = strings.Join(textParts, "\n")
			}

			if hasToolUse {
				event.Data["has_tool_use"] = true
				if len(toolNames) > 0 {
					event.Data["tool_names"] = toolNames
				}
			}
		}
	}

	// Any assistant event -> working status
	status := runnerstatus.StatusWorking
	return []*NormalizedEvent{event}, &status
}

// parseUser handles user message events (tool results).
func (a *ClaudeAdapter) parseUser(raw *claudeRawEvent) []*NormalizedEvent {
	event := &NormalizedEvent{
		Kind: EventKindMessage,
		Data: make(map[string]interface{}),
	}

	event.Data["role"] = "user"
	event.Data["has_tool_use"] = false
	messageFamily := MessageFamilyPrompt
	if raw.ToolUseResult != nil {
		messageFamily = MessageFamilyToolResult
	}

	if raw.Message != nil && len(raw.Message.Content) > 0 {
		// Try to extract text from content
		var textContent string
		if err := json.Unmarshal(raw.Message.Content, &textContent); err == nil {
			event.Data["text"] = textContent
		} else {
			// Content might be a complex structure, store as-is
			var content interface{}
			if err := json.Unmarshal(raw.Message.Content, &content); err == nil {
				// Try to extract text from array of blocks
				if blocks, ok := content.([]interface{}); ok {
					var textParts []string
					var enrichedBlocks []map[string]interface{}
					hasToolResult := false
					for _, block := range blocks {
						if blockMap, ok := block.(map[string]interface{}); ok {
							enriched := map[string]interface{}{}
							if t, ok := blockMap["type"].(string); ok {
								enriched["type"] = t
								if t == "tool_result" {
									hasToolResult = true
								}
							}
							if text, ok := blockMap["text"].(string); ok {
								textParts = append(textParts, text)
								enriched["text"] = text
							}
							if c, ok := blockMap["content"].(string); ok {
								textParts = append(textParts, c)
								enriched["content"] = c
							}
							if toolUseID, ok := blockMap["tool_use_id"].(string); ok {
								enriched["tool_use_id"] = toolUseID
							}
							enrichedBlocks = append(enrichedBlocks, enriched)
						}
					}
					if hasToolResult {
						messageFamily = MessageFamilyToolResult
					}
					if len(enrichedBlocks) > 0 {
						event.Data["content_blocks"] = enrichedBlocks
					}
					if len(textParts) > 0 {
						event.Data["text"] = strings.Join(textParts, "\n")
					}
				}
			}
		}
	}
	if messageFamily == MessageFamilyToolResult {
		if _, hasText := event.Data["text"]; !hasText && raw.ToolUseResult != nil {
			switch v := raw.ToolUseResult.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					event.Data["text"] = v
				}
			default:
				if encoded, err := json.Marshal(v); err == nil {
					text := strings.TrimSpace(string(encoded))
					if text != "" && text != "null" {
						event.Data["text"] = text
					}
				}
			}
		}
	}
	event.Data["message_family"] = messageFamily

	return []*NormalizedEvent{event}
}

// parseResult handles result events (success/error).
func (a *ClaudeAdapter) parseResult(raw *claudeRawEvent) ([]*NormalizedEvent, *runnerstatus.Status) {
	success, failureReason := claudeResultSucceeded(raw)
	if !success {
		event := &NormalizedEvent{
			Kind: EventKindError,
			Data: make(map[string]interface{}),
		}

		message := strings.TrimSpace(raw.ErrorMessage)
		if message == "" {
			message = "runner reported non-success result"
			if failureReason != "" {
				message += ": " + failureReason
			}
		}
		event.Data["message"] = message
		if raw.ErrorCode != "" {
			event.Data["code"] = raw.ErrorCode
		}
		if subtype := strings.TrimSpace(raw.Subtype); subtype != "" {
			event.Data["result_subtype"] = subtype
		}
		if failureReason != "" {
			event.Data["result_state"] = failureReason
		}

		// Error -> no semantic status (lifecycle handles it)
		return []*NormalizedEvent{event}, nil
	}

	// Success result
	event := &NormalizedEvent{
		Kind: EventKindFinal,
		Data: make(map[string]interface{}),
	}

	event.Data["success"] = true

	if raw.DurationMS != nil {
		event.Data["duration_ms"] = *raw.DurationMS
	} else if raw.DurationAPIMS != nil {
		event.Data["duration_ms"] = *raw.DurationAPIMS
	}

	if raw.CostUSD != nil {
		event.Data["cost_usd"] = *raw.CostUSD
	} else if raw.TotalCostUSD != nil {
		event.Data["cost_usd"] = *raw.TotalCostUSD
	}

	if raw.Usage != nil {
		usage := map[string]int64{
			"input_tokens":  raw.Usage.InputTokens,
			"output_tokens": raw.Usage.OutputTokens,
		}
		event.Data["usage"] = usage
	}

	// Success -> ready_for_review
	status := runnerstatus.StatusReadyForReview
	return []*NormalizedEvent{event}, &status
}

func claudeResultSucceeded(raw *claudeRawEvent) (bool, string) {
	subtype := strings.ToLower(strings.TrimSpace(raw.Subtype))
	switch subtype {
	case "":
		// Fall back to result status token checks below.
	case "success":
		return true, ""
	case "error", "failed", "failure", "canceled", "cancelled", "timeout", "timed_out", "interrupted":
		return false, subtype
	default:
		// Fail closed for unknown explicit subtypes to avoid misreporting success.
		return false, subtype
	}

	resultState := strings.ToLower(strings.TrimSpace(raw.Result))
	switch resultState {
	case "error", "failed", "failure", "canceled", "cancelled", "timeout", "timed_out", "interrupted":
		return false, resultState
	default:
		return true, ""
	}
}
