package render

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NormalizeActivityKind maps surface-specific kind tokens to one shared
// human-readable activity language.
func NormalizeActivityKind(kind string) string {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch normalized {
	case "":
		return "activity"
	case "prompt-seed":
		return "prompt"
	case "followup":
		return "follow-up"
	}
	return normalized
}

// ActivitySummaryText returns summary text with a stable fallback per kind.
func ActivitySummaryText(kind, summary string) string {
	if trimmed := strings.TrimSpace(summary); trimmed != "" {
		return trimmed
	}
	switch NormalizeActivityKind(kind) {
	case "prompt":
		return "prompt"
	case "follow-up":
		return "follow-up prompt"
	case "assistant":
		return "assistant turn"
	default:
		return "activity"
	}
}

// FormatActivityLabel returns canonical "[kind] summary" presentation.
func FormatActivityLabel(kind, summary string) string {
	return fmt.Sprintf("[%s] %s", NormalizeActivityKind(kind), ActivitySummaryText(kind, summary))
}

// FormatActivityWithExtras returns activity label with tool/checkpoint suffix.
func FormatActivityWithExtras(kind, summary string, toolCount int, checkpointID int, restorable bool) string {
	return FormatActivityLabel(kind, summary) + FormatTurnExtras(toolCount, checkpointID, restorable)
}

// FormatTurnExtras returns concise turn metadata suffix, e.g.
// "(tools=2, checkpoint=4)".
func FormatTurnExtras(toolCount int, checkpointID int, restorable bool) string {
	parts := make([]string, 0, 2)
	if toolCount > 0 {
		parts = append(parts, fmt.Sprintf("tools=%d", toolCount))
	}
	if restorable && checkpointID > 0 {
		parts = append(parts, fmt.Sprintf("checkpoint=%d", checkpointID))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// FormatToolCallSummary renders tool calls in one stable style.
func FormatToolCallSummary(name, command string, hasExit bool, exitCode int) string {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		trimmedName = "tool"
	}
	parts := []string{"▶", trimmedName}
	if trimmedCommand := strings.TrimSpace(command); trimmedCommand != "" {
		parts = append(parts, trimmedCommand)
	}
	result := strings.Join(parts, " ")
	if hasExit {
		result += fmt.Sprintf(" (exit=%d)", exitCode)
	}
	return result
}

// FormatChangedPathSummary renders checkpoint changed-path previews consistently.
func FormatChangedPathSummary(paths []string, totalCount int, trimmed bool) string {
	if len(paths) == 0 {
		return ""
	}
	nonEmpty := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmedPath := strings.TrimSpace(path)
		if trimmedPath == "" {
			continue
		}
		nonEmpty = append(nonEmpty, trimmedPath)
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	joined := strings.Join(nonEmpty, ", ")
	if !trimmed {
		return joined
	}
	remaining := totalCount - len(nonEmpty)
	if remaining <= 0 {
		return joined
	}
	return fmt.Sprintf("%s, ... (+%d more)", joined, remaining)
}

// TimelineContentBlock is the typed shape used by transcript and daemon
// projections after decoding raw timeline payload content blocks.
type TimelineContentBlock struct {
	Type    string
	Text    string
	Content string
	Name    string
	Input   interface{}
}

// TimelineUsage is the typed nested usage payload when a timeline event embeds
// token accounting inside a final event.
type TimelineUsage struct {
	InputTokens     int64
	OutputTokens    int64
	HasInputTokens  bool
	HasOutputTokens bool
}

// TimelinePayload is the concrete decoded view of raw timeline event data.
// It is shared by daemon turn projection and transcript rendering so raw map
// interpretation lives in one place.
type TimelinePayload struct {
	Role            string
	MessageFamily   string
	Text            string
	Model           string
	CWD             string
	ToolID          string
	Name            string
	Command         string
	Message         string
	Reason          string
	Error           string
	EventKind       string
	RunnerEventType string

	ToolNames []string
	Blocks    []TimelineContentBlock

	ExitCode    int
	HasExitCode bool

	DurationMS    float64
	HasDurationMS bool
	CostUSD       float64
	HasCostUSD    bool

	InputTokens     int64
	HasInputTokens  bool
	OutputTokens    int64
	HasOutputTokens bool

	ParseErrorCount    int
	HasParseErrorCount bool
	CheckpointID       int
	HasCheckpointID    bool
	Line               int
	HasLine            bool

	Usage TimelineUsage
}

// DecodeTimelinePayload converts raw timeline JSON payload maps into one typed
// shape that callers can consume directly.
func DecodeTimelinePayload(data map[string]interface{}) TimelinePayload {
	payload := TimelinePayload{
		Role:            timelineString(data, "role"),
		MessageFamily:   timelineString(data, "message_family"),
		Text:            timelineString(data, "text"),
		Model:           timelineString(data, "model"),
		CWD:             timelineString(data, "cwd"),
		ToolID:          timelineString(data, "tool_id"),
		Name:            timelineString(data, "name"),
		Command:         timelineString(data, "command"),
		Message:         timelineString(data, "message"),
		Reason:          timelineString(data, "reason"),
		Error:           timelineString(data, "error"),
		EventKind:       timelineString(data, "event_kind"),
		RunnerEventType: timelineString(data, "runner_event_type"),
		ToolNames:       timelineStringSlice(data, "tool_names"),
		Blocks:          timelineContentBlocks(data),
	}

	if exitCode, ok := timelineFloat(data, "exit_code"); ok {
		payload.ExitCode = int(exitCode)
		payload.HasExitCode = true
	}
	if durationMS, ok := timelineFloat(data, "duration_ms"); ok {
		payload.DurationMS = durationMS
		payload.HasDurationMS = true
	}
	if costUSD, ok := timelineFloat(data, "cost_usd"); ok {
		payload.CostUSD = costUSD
		payload.HasCostUSD = true
	}
	if inputTokens, ok := timelineFloat(data, "input_tokens"); ok {
		payload.InputTokens = int64(inputTokens)
		payload.HasInputTokens = true
	}
	if outputTokens, ok := timelineFloat(data, "output_tokens"); ok {
		payload.OutputTokens = int64(outputTokens)
		payload.HasOutputTokens = true
	}
	if parseErrorCount, ok := timelineFloat(data, "parse_error_count"); ok {
		payload.ParseErrorCount = int(parseErrorCount)
		payload.HasParseErrorCount = true
	}
	if checkpointID, ok := timelineFloat(data, "checkpoint_id"); ok && checkpointID > 0 {
		payload.CheckpointID = int(checkpointID)
		payload.HasCheckpointID = true
	}
	if line, ok := timelineFloat(data, "line"); ok && line > 0 {
		payload.Line = int(line)
		payload.HasLine = true
	}

	if usage, ok := data["usage"].(map[string]interface{}); ok {
		if inputTokens, ok := timelineFloat(usage, "input_tokens"); ok {
			payload.Usage.InputTokens = int64(inputTokens)
			payload.Usage.HasInputTokens = true
		}
		if outputTokens, ok := timelineFloat(usage, "output_tokens"); ok {
			payload.Usage.OutputTokens = int64(outputTokens)
			payload.Usage.HasOutputTokens = true
		}
	}

	return payload
}

// PromptLikeSummary returns the best prompt/follow-up summary extracted from a
// decoded timeline payload.
func (p TimelinePayload) PromptLikeSummary() string {
	if text := strings.TrimSpace(p.Text); text != "" {
		return text
	}
	for _, block := range p.Blocks {
		blockType := strings.TrimSpace(block.Type)
		if blockType != "" && blockType != "text" {
			continue
		}
		if text := strings.TrimSpace(block.Text); text != "" {
			return text
		}
		if content := strings.TrimSpace(block.Content); content != "" {
			return content
		}
	}
	return ""
}

// AssistantSummary returns the canonical assistant-turn summary used by daemon
// history and latest-activity projections.
func (p TimelinePayload) AssistantSummary() string {
	base := strings.TrimSpace(p.Text)
	textHints, toolHints := p.summaryHints()
	if base == "" && len(textHints) > 0 {
		base = textHints[0]
	}

	toolNames := normalizeTimelineSummaryHints(append(append([]string(nil), p.ToolNames...), toolHints...))
	if ShouldEnrichActivitySummary(base) && len(toolNames) > 0 {
		toolSummary := "tools: " + strings.Join(toolNames, ", ")
		if base == "" {
			return toolSummary
		}
		return base + " (" + toolSummary + ")"
	}
	return base
}

// PromptMessageText returns the rendered text for a user prompt message.
func (p TimelinePayload) PromptMessageText() string {
	if text := strings.TrimSpace(p.Text); text != "" {
		return text
	}
	parts := make([]string, 0, len(p.Blocks))
	for _, block := range p.Blocks {
		blockType := strings.TrimSpace(block.Type)
		if blockType != "" && blockType != "text" {
			continue
		}
		if text := strings.TrimSpace(block.Text); text != "" {
			parts = append(parts, text)
			continue
		}
		if content := strings.TrimSpace(block.Content); content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}

// IsToolResultMessage reports whether a decoded user message is a tool result.
func (p TimelinePayload) IsToolResultMessage() bool {
	switch strings.ToLower(strings.TrimSpace(p.MessageFamily)) {
	case "prompt":
		return false
	case "tool_result":
		return true
	}
	for _, block := range p.Blocks {
		if strings.EqualFold(strings.TrimSpace(block.Type), "tool_result") {
			return true
		}
	}
	return false
}

// ParseErrorSummary returns the canonical parse diagnostic summary.
func (p TimelinePayload) ParseErrorSummary() string {
	reason := strings.TrimSpace(p.Reason)
	if reason == "" {
		reason = "unknown"
	}
	summary := "parse error: " + strings.ReplaceAll(reason, "_", " ")
	if p.HasLine && p.Line > 0 {
		summary = fmt.Sprintf("%s (line %d)", summary, p.Line)
	}
	if detail := strings.TrimSpace(p.Error); detail != "" {
		summary += " - " + detail
	}
	return summary
}

// UnknownRunnerEventSummary returns the canonical unknown-runner-event summary.
func (p TimelinePayload) UnknownRunnerEventSummary() string {
	summary := "unknown runner event"
	if eventType := strings.TrimSpace(p.RunnerEventType); eventType != "" {
		summary += ": " + eventType
	}
	if reason := strings.TrimSpace(p.Reason); reason != "" {
		summary += " (" + strings.ReplaceAll(reason, "_", " ") + ")"
	}
	if detail := strings.TrimSpace(p.Error); detail != "" {
		summary += " - " + detail
	}
	return summary
}

// UnrecognizedEventSummary returns the canonical fallback summary for unknown
// timeline kinds.
func (p TimelinePayload) UnrecognizedEventSummary(kind string) string {
	normalizedKind := strings.TrimSpace(strings.ReplaceAll(kind, "_", " "))
	if normalizedKind == "" {
		normalizedKind = "unknown"
	}
	summary := "unrecognized timeline event: " + normalizedKind
	if reason := strings.TrimSpace(p.Reason); reason != "" {
		summary += " (" + strings.ReplaceAll(reason, "_", " ") + ")"
	}
	if text := strings.TrimSpace(p.Text); text != "" {
		summary += " - " + text
	}
	return summary
}

// TimelineEntrySummary returns the stable one-line summary for a timeline entry.
func TimelineEntrySummary(kind string, payload TimelinePayload) string {
	switch kind {
	case "message":
		if text := strings.TrimSpace(payload.Text); text != "" {
			return text
		}
		if strings.EqualFold(strings.TrimSpace(payload.Role), "user") {
			if text := strings.TrimSpace(payload.PromptMessageText()); text != "" {
				return text
			}
			return "user message"
		}
		return "assistant message"
	case "prompt_seed":
		if text := strings.TrimSpace(payload.PromptLikeSummary()); text != "" {
			return text
		}
		return "prompt"
	case "followup_prompt":
		if text := strings.TrimSpace(payload.PromptLikeSummary()); text != "" {
			return text
		}
		return "follow-up prompt"
	case "tool_use":
		if cmd := strings.TrimSpace(payload.Command); cmd != "" {
			return cmd
		}
		if name := strings.TrimSpace(payload.Name); name != "" {
			return "tool: " + name
		}
		return "tool activity"
	case "parse_error":
		return payload.ParseErrorSummary()
	case "unknown":
		return payload.UnknownRunnerEventSummary()
	default:
		if text := strings.TrimSpace(payload.Text); text != "" {
			return text
		}
		return strings.TrimSpace(strings.ReplaceAll(kind, "_", " "))
	}
}

// ShouldEnrichActivitySummary reports whether a short generic summary should be
// expanded with available tool hints.
func ShouldEnrichActivitySummary(summary string) bool {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	prefixes := []string{"done", "completed", "complete", "finished", "ok", "okay"}
	for _, prefix := range prefixes {
		if lower == prefix ||
			strings.HasPrefix(lower, prefix+".") ||
			strings.HasPrefix(lower, prefix+" ") ||
			strings.HasPrefix(lower, prefix+" -") ||
			strings.HasPrefix(lower, prefix+" (") {
			return true
		}
	}
	return false
}

func (p TimelinePayload) summaryHints() ([]string, []string) {
	if len(p.Blocks) == 0 {
		return nil, nil
	}
	textHints := make([]string, 0, len(p.Blocks))
	toolHints := make([]string, 0, len(p.Blocks))
	for _, block := range p.Blocks {
		switch strings.TrimSpace(block.Type) {
		case "text":
			if txt := strings.TrimSpace(block.Text); txt != "" {
				textHints = append(textHints, txt)
			}
		case "tool_use":
			if toolName := strings.TrimSpace(block.Name); toolName != "" {
				toolHints = append(toolHints, toolName)
			}
		}
	}
	return textHints, toolHints
}

func normalizeTimelineSummaryHints(hints []string) []string {
	if len(hints) == 0 {
		return nil
	}
	const maxHints = 4
	seen := make(map[string]struct{}, len(hints))
	out := make([]string, 0, maxHints)
	for _, hint := range hints {
		trimmed := strings.TrimSpace(hint)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, trimmed)
		if len(out) >= maxHints {
			break
		}
	}
	return out
}

func timelineContentBlocks(data map[string]interface{}) []TimelineContentBlock {
	if data == nil {
		return nil
	}
	rawBlocks, ok := data["content_blocks"]
	if !ok {
		return nil
	}

	var rawItems []interface{}
	switch typed := rawBlocks.(type) {
	case []interface{}:
		rawItems = typed
	case []map[string]interface{}:
		rawItems = make([]interface{}, 0, len(typed))
		for _, block := range typed {
			rawItems = append(rawItems, block)
		}
	default:
		return nil
	}

	blocks := make([]TimelineContentBlock, 0, len(rawItems))
	for _, rawItem := range rawItems {
		block, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		blocks = append(blocks, TimelineContentBlock{
			Type:    timelineString(block, "type"),
			Text:    timelineString(block, "text"),
			Content: timelineString(block, "content"),
			Name:    timelineString(block, "name"),
			Input:   block["input"],
		})
	}
	return blocks
}

func timelineStringSlice(data map[string]interface{}, key string) []string {
	if data == nil {
		return nil
	}
	raw, ok := data[key]
	if !ok {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if str, ok := value.(string); ok {
				if trimmed := strings.TrimSpace(str); trimmed != "" {
					out = append(out, trimmed)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func timelineString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	raw, ok := data[key]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}

func timelineFloat(data map[string]interface{}, key string) (float64, bool) {
	if data == nil {
		return 0, false
	}
	raw, ok := data[key]
	if !ok {
		return 0, false
	}
	switch value := raw.(type) {
	case float64:
		return value, true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
