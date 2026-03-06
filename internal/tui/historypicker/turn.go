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
}

// GroupTimelineIntoTurns converts a flat timeline into grouped conversation
// turns suitable for the interactive history picker.
func GroupTimelineIntoTurns(entries []TimelineEntry, checkpoints []CheckpointRef) []Turn {
	if len(entries) == 0 {
		return nil
	}

	checkpointSet := make(map[int]struct{}, len(checkpoints))
	for _, cp := range checkpoints {
		checkpointSet[cp.ID] = struct{}{}
	}

	var turns []Turn
	latestCheckpointID := 0

	for _, entry := range entries {
		switch entry.Kind {
		case "checkpoint_event":
			if cpID := extractCheckpointID(entry.Data); cpID > 0 {
				if _, ok := checkpointSet[cpID]; ok {
					latestCheckpointID = cpID
				}
			}
			// Absorb into the preceding turn only if it's an assistant turn.
			// Checkpoints represent code state snapshots produced by assistant
			// work — they should not retroactively update prompt or followup
			// turns which are user input, not work products.
			if len(turns) > 0 && turns[len(turns)-1].Kind == TurnAssistant {
				last := &turns[len(turns)-1]
				last.CheckpointID = latestCheckpointID
				last.Restorable = latestCheckpointID > 0
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
			// Assistant message starts a new turn
			turns = append(turns, Turn{
				EntryID:        entry.EntryID,
				Kind:           TurnAssistant,
				Timestamp:      entry.Timestamp,
				ShortTimestamp: shortTimestamp(entry.Timestamp),
				Summary:        strings.TrimSpace(dataString(entry.Data, "text")),
				CheckpointID:   latestCheckpointID,
				Restorable:     latestCheckpointID > 0,
			})
			continue

		case "prompt_seed":
			turns = append(turns, Turn{
				EntryID:        entry.EntryID,
				Kind:           TurnPrompt,
				Timestamp:      entry.Timestamp,
				ShortTimestamp: shortTimestamp(entry.Timestamp),
				Summary:        strings.TrimSpace(dataString(entry.Data, "text")),
				CheckpointID:   latestCheckpointID,
				Restorable:     latestCheckpointID > 0,
			})
			continue

		case "followup_prompt":
			turns = append(turns, Turn{
				EntryID:        entry.EntryID,
				Kind:           TurnFollowup,
				Timestamp:      entry.Timestamp,
				ShortTimestamp: shortTimestamp(entry.Timestamp),
				Summary:        strings.TrimSpace(dataString(entry.Data, "text")),
				CheckpointID:   latestCheckpointID,
				Restorable:     latestCheckpointID > 0,
			})
			continue

		default:
			continue
		}
	}

	return turns
}

func shortTimestamp(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Format("15:04:05")
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
