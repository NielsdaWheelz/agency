package historypicker

import (
	"encoding/json"
	"strings"
	"time"
)

// TurnKind classifies a conversation turn for display purposes.
type TurnKind string

const (
	TurnPrompt    TurnKind = "prompt"
	TurnAssistant TurnKind = "assistant"
	TurnFollowup  TurnKind = "followup"
)

// Turn represents one logical conversation turn in the history picker.
// Timeline entries are grouped: an assistant message together with its
// subsequent tool calls and tool results form a single Turn.
type Turn struct {
	EntryID        string
	Kind           TurnKind
	Timestamp      string // full RFC3339
	ShortTimestamp string // HH:MM:SS only
	Summary        string
	ToolCalls      []ToolCall
	CheckpointID   int
	Restorable     bool // true when a valid checkpoint is mapped

	CheckpointDescription  string
	CheckpointDiffstat     string
	CheckpointChangedPaths []string
	CheckpointChangedCount int
	CheckpointPathsTrimmed bool
}

// ToolCall is a tool invocation rendered within an assistant turn.
type ToolCall struct {
	Name     string
	Command  string
	ExitCode int
	HasExit  bool
}

// TimelineEntry is the input type for grouping. Callers convert from their
// DTO types into this to avoid coupling the picker to daemon types.
type TimelineEntry struct {
	EntryID   string
	Kind      string
	Timestamp string
	Data      map[string]interface{}
}

// CheckpointRef identifies a valid checkpoint that can be restored.
type CheckpointRef struct {
	ID int

	Description          string
	Diffstat             string
	ChangedPaths         []string
	ChangedPathCount     int
	ChangedPathTruncated bool
}

// GroupTimelineIntoTurns converts a flat timeline into grouped conversation
// turns suitable for the interactive history picker.
func GroupTimelineIntoTurns(entries []TimelineEntry, checkpoints []CheckpointRef) []Turn {
	if len(entries) == 0 {
		return nil
	}

	checkpointSet := make(map[int]CheckpointRef, len(checkpoints))
	for _, cp := range checkpoints {
		checkpointSet[cp.ID] = cp
	}

	var turns []Turn
	latestCheckpointID := 0
	var latestCheckpoint CheckpointRef

	for _, entry := range entries {
		switch entry.Kind {
		case "checkpoint_event":
			if cpID := extractCheckpointID(entry.Data); cpID > 0 {
				if cp, ok := checkpointSet[cpID]; ok {
					latestCheckpointID = cpID
					latestCheckpoint = cp
				}
			}
			// Absorb into the preceding turn only if it's an assistant turn.
			// Checkpoints represent code state snapshots produced by assistant
			// work — they should not retroactively update prompt or followup
			// turns which are user input, not work products.
			if len(turns) > 0 && turns[len(turns)-1].Kind == TurnAssistant {
				last := &turns[len(turns)-1]
				applyCheckpointMetadata(last, latestCheckpointID, latestCheckpoint)
			}
			continue

		case "session_start", "final", "error", "raw_log_coverage", "invocation_event", "usage", "status", "parse_error":
			// Filtered out — not displayed as turns
			continue

		case "tool_use":
			// Attach to the current (last) assistant turn
			if len(turns) > 0 && turns[len(turns)-1].Kind == TurnAssistant {
				tc := ToolCall{
					Name:    dataString(entry.Data, "name"),
					Command: dataString(entry.Data, "command"),
				}
				if exitCode, ok := dataFloat(entry.Data, "exit_code"); ok {
					tc.ExitCode = int(exitCode)
					tc.HasExit = true
				}
				turns[len(turns)-1].ToolCalls = append(turns[len(turns)-1].ToolCalls, tc)
			}
			continue

		case "message":
			role := dataString(entry.Data, "role")
			if role == "user" {
				// Tool result — absorbed into the current assistant turn
				continue
			}
			if role != "assistant" {
				continue
			}
			// Assistant message starts a new turn
			turns = append(turns, Turn{
				EntryID:        entry.EntryID,
				Kind:           TurnAssistant,
				Timestamp:      entry.Timestamp,
				ShortTimestamp: shortTimestamp(entry.Timestamp),
				Summary:        assistantMessageSummary(entry.Data),
			})
			applyCheckpointMetadata(&turns[len(turns)-1], latestCheckpointID, latestCheckpoint)
			continue

		case "prompt_seed":
			turns = append(turns, Turn{
				EntryID:        entry.EntryID,
				Kind:           TurnPrompt,
				Timestamp:      entry.Timestamp,
				ShortTimestamp: shortTimestamp(entry.Timestamp),
				Summary:        strings.TrimSpace(dataString(entry.Data, "text")),
			})
			applyCheckpointMetadata(&turns[len(turns)-1], latestCheckpointID, latestCheckpoint)
			continue

		case "followup_prompt":
			turns = append(turns, Turn{
				EntryID:        entry.EntryID,
				Kind:           TurnFollowup,
				Timestamp:      entry.Timestamp,
				ShortTimestamp: shortTimestamp(entry.Timestamp),
				Summary:        strings.TrimSpace(dataString(entry.Data, "text")),
			})
			applyCheckpointMetadata(&turns[len(turns)-1], latestCheckpointID, latestCheckpoint)
			continue

		default:
			continue
		}
	}

	return turns
}

func applyCheckpointMetadata(turn *Turn, checkpointID int, checkpoint CheckpointRef) {
	if turn == nil {
		return
	}
	turn.CheckpointID = checkpointID
	turn.Restorable = checkpointID > 0
	if !turn.Restorable {
		return
	}

	turn.CheckpointDescription = strings.TrimSpace(checkpoint.Description)
	turn.CheckpointDiffstat = strings.TrimSpace(checkpoint.Diffstat)
	turn.CheckpointChangedPaths = append([]string(nil), checkpoint.ChangedPaths...)
	turn.CheckpointChangedCount = checkpoint.ChangedPathCount
	turn.CheckpointPathsTrimmed = checkpoint.ChangedPathTruncated

	if shouldEnrichTurnSummary(turn.Summary) {
		turn.Summary = enrichTurnSummary(turn.Summary, checkpoint)
	}
}

func shouldEnrichTurnSummary(summary string) bool {
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

func enrichTurnSummary(summary string, checkpoint CheckpointRef) string {
	parts := make([]string, 0, 3)

	base := strings.TrimSpace(summary)
	if base != "" {
		parts = append(parts, base)
	}
	if desc := strings.TrimSpace(checkpoint.Description); desc != "" {
		parts = append(parts, desc)
	}
	if diff := strings.TrimSpace(checkpoint.Diffstat); diff != "" {
		parts = append(parts, diff)
	}
	return strings.Join(parts, " - ")
}

func shortTimestamp(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Format("15:04:05")
}

func assistantMessageSummary(data map[string]interface{}) string {
	base := strings.TrimSpace(dataString(data, "text"))
	textHints, toolHints := summaryHintsFromContentBlocks(data)
	toolNames := normalizeSummaryHints(append(dataStringSlice(data, "tool_names"), toolHints...))

	// Prefer natural-language content from content_blocks when top-level text is absent.
	if base == "" && len(textHints) > 0 {
		base = textHints[0]
	}

	if shouldEnrichTurnSummary(base) && len(toolNames) > 0 {
		toolSummary := "tools: " + strings.Join(toolNames, ", ")
		if base == "" {
			return toolSummary
		}
		return base + " (" + toolSummary + ")"
	}

	return base
}

func summaryHintsFromContentBlocks(data map[string]interface{}) ([]string, []string) {
	if data == nil {
		return nil, nil
	}
	rawBlocks, ok := data["content_blocks"]
	if !ok {
		return nil, nil
	}

	var blocks []interface{}
	switch typed := rawBlocks.(type) {
	case []interface{}:
		blocks = typed
	case []map[string]interface{}:
		blocks = make([]interface{}, 0, len(typed))
		for _, block := range typed {
			blocks = append(blocks, block)
		}
	default:
		return nil, nil
	}

	textHints := make([]string, 0, len(blocks))
	toolHints := make([]string, 0, len(blocks))
	for _, rawBlock := range blocks {
		block, ok := rawBlock.(map[string]interface{})
		if !ok {
			continue
		}

		switch strings.TrimSpace(dataString(block, "type")) {
		case "text":
			if txt := strings.TrimSpace(dataString(block, "text")); txt != "" {
				textHints = append(textHints, txt)
			}
		case "tool_use":
			if toolName := strings.TrimSpace(dataString(block, "name")); toolName != "" {
				toolHints = append(toolHints, toolName)
			}
		}
	}
	return textHints, toolHints
}

func dataStringSlice(data map[string]interface{}, key string) []string {
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

func normalizeSummaryHints(hints []string) []string {
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

func dataString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func dataFloat(data map[string]interface{}, key string) (float64, bool) {
	if data == nil {
		return 0, false
	}
	v, ok := data[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func extractCheckpointID(data map[string]interface{}) int {
	f, ok := dataFloat(data, "checkpoint_id")
	if !ok || f <= 0 {
		return 0
	}
	return int(f)
}
