package daemon

import (
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/render"
)

// TurnKind classifies a conversation turn for display purposes.
type TurnKind string

const (
	TurnPrompt    TurnKind = "prompt"
	TurnAssistant TurnKind = "assistant"
	TurnFollowup  TurnKind = "followup"
)

// Turn represents one logical conversation turn in projected invocation history.
type Turn struct {
	EntryID        string
	Kind           TurnKind
	Timestamp      string
	ShortTimestamp string
	Summary        string
	ToolCalls      []ToolCall
	CheckpointID   int
	Restorable     bool

	CheckpointDescription  string
	CheckpointDiffstat     string
	CheckpointChangedPaths []string
	CheckpointChangedCount int
	CheckpointPathsTrimmed bool
}

// ToolCall is a tool invocation rendered within an assistant turn.
type ToolCall struct {
	ID       string
	Name     string
	Command  string
	ExitCode int
	HasExit  bool
}

// TimelineTurnEntry is the input type for grouping projected history turns.
type TimelineTurnEntry struct {
	EntryID   string
	Kind      string
	Timestamp string
	Data      map[string]interface{}
}

// TurnCheckpointRef identifies a valid checkpoint that can be restored.
type TurnCheckpointRef struct {
	ID int

	Description          string
	Diffstat             string
	ChangedPaths         []string
	ChangedPathCount     int
	ChangedPathTruncated bool
}

// GroupTimelineIntoTurns converts a flat timeline into grouped conversation turns.
func GroupTimelineIntoTurns(entries []TimelineTurnEntry, checkpoints []TurnCheckpointRef) []Turn {
	if len(entries) == 0 {
		return nil
	}

	checkpointSet := make(map[int]TurnCheckpointRef, len(checkpoints))
	for _, cp := range checkpoints {
		checkpointSet[cp.ID] = cp
	}

	var turns []Turn
	latestCheckpointID := 0
	var latestCheckpoint TurnCheckpointRef

	for _, entry := range entries {
		switch entry.Kind {
		case "checkpoint_event":
			payload := render.DecodeTimelinePayload(entry.Data)
			if payload.HasCheckpointID && payload.CheckpointID > 0 {
				cpID := payload.CheckpointID
				if cp, ok := checkpointSet[cpID]; ok {
					latestCheckpointID = cpID
					latestCheckpoint = cp
				}
			}
			if len(turns) > 0 && turns[len(turns)-1].Kind == TurnAssistant {
				last := &turns[len(turns)-1]
				applyCheckpointMetadata(last, latestCheckpointID, latestCheckpoint)
			}
			continue
		case "session_start", "final", "error", "raw_log_coverage", "invocation_event", "usage", "status":
			continue
		case "parse_error":
			payload := render.DecodeTimelinePayload(entry.Data)
			turns = appendDiagnosticTurn(
				turns,
				entry,
				payload.ParseErrorSummary(),
				latestCheckpointID,
				latestCheckpoint,
			)
			continue
		case "unknown":
			payload := render.DecodeTimelinePayload(entry.Data)
			turns = appendDiagnosticTurn(
				turns,
				entry,
				payload.UnknownRunnerEventSummary(),
				latestCheckpointID,
				latestCheckpoint,
			)
			continue
		case "tool_use":
			if len(turns) > 0 && turns[len(turns)-1].Kind == TurnAssistant {
				payload := render.DecodeTimelinePayload(entry.Data)
				tc := ToolCall{
					ID:      payload.ToolID,
					Name:    payload.Name,
					Command: payload.Command,
				}
				if payload.HasExitCode {
					tc.ExitCode = payload.ExitCode
					tc.HasExit = true
				}
				lastTurn := &turns[len(turns)-1]
				if tc.ID != "" {
					matched := false
					for i := len(lastTurn.ToolCalls) - 1; i >= 0; i-- {
						lastTool := &lastTurn.ToolCalls[i]
						if strings.TrimSpace(lastTool.ID) != tc.ID {
							continue
						}
						if strings.TrimSpace(lastTool.Name) == "" {
							lastTool.Name = tc.Name
						}
						if strings.TrimSpace(lastTool.Command) == "" {
							lastTool.Command = tc.Command
						}
						if tc.HasExit {
							lastTool.ExitCode = tc.ExitCode
							lastTool.HasExit = true
						}
						matched = true
						break
					}
					if matched {
						continue
					}
				}
				if tc.HasExit && len(lastTurn.ToolCalls) > 0 {
					lastTool := &lastTurn.ToolCalls[len(lastTurn.ToolCalls)-1]
					if !lastTool.HasExit && sameToolIdentity(*lastTool, tc) {
						if strings.TrimSpace(lastTool.Name) == "" {
							lastTool.Name = tc.Name
						}
						if strings.TrimSpace(lastTool.Command) == "" {
							lastTool.Command = tc.Command
						}
						lastTool.ExitCode = tc.ExitCode
						lastTool.HasExit = true
						continue
					}
				}
				lastTurn.ToolCalls = append(lastTurn.ToolCalls, tc)
			}
			continue
		case "message":
			payload := render.DecodeTimelinePayload(entry.Data)
			if payload.Role == "user" {
				if strings.EqualFold(strings.TrimSpace(payload.MessageFamily), "prompt") {
					kind := TurnFollowup
					if len(turns) == 0 {
						kind = TurnPrompt
					}
					turns = appendPromptLikeTurn(
						turns,
						entry,
						kind,
						payload.PromptLikeSummary(),
						latestCheckpointID,
						latestCheckpoint,
					)
				}
				continue
			}
			if payload.Role != "assistant" {
				continue
			}
			turns = append(turns, Turn{
				EntryID:        entry.EntryID,
				Kind:           TurnAssistant,
				Timestamp:      entry.Timestamp,
				ShortTimestamp: shortTimestamp(entry.Timestamp),
				Summary:        payload.AssistantSummary(),
			})
			applyCheckpointMetadata(&turns[len(turns)-1], latestCheckpointID, latestCheckpoint)
			continue
		case "prompt_seed":
			payload := render.DecodeTimelinePayload(entry.Data)
			turns = appendPromptLikeTurn(
				turns,
				entry,
				TurnPrompt,
				payload.PromptLikeSummary(),
				latestCheckpointID,
				latestCheckpoint,
			)
			continue
		case "followup_prompt":
			payload := render.DecodeTimelinePayload(entry.Data)
			turns = appendPromptLikeTurn(
				turns,
				entry,
				TurnFollowup,
				payload.PromptLikeSummary(),
				latestCheckpointID,
				latestCheckpoint,
			)
			continue
		default:
			payload := render.DecodeTimelinePayload(entry.Data)
			turns = appendDiagnosticTurn(
				turns,
				entry,
				payload.UnrecognizedEventSummary(entry.Kind),
				latestCheckpointID,
				latestCheckpoint,
			)
			continue
		}
	}

	return turns
}

func appendPromptLikeTurn(
	turns []Turn,
	entry TimelineTurnEntry,
	kind TurnKind,
	summary string,
	latestCheckpointID int,
	latestCheckpoint TurnCheckpointRef,
) []Turn {
	trimmedSummary := strings.TrimSpace(summary)
	if trimmedSummary == "" {
		if kind == TurnPrompt {
			trimmedSummary = "prompt"
		} else {
			trimmedSummary = "follow-up prompt"
		}
	}

	if len(turns) > 0 {
		last := &turns[len(turns)-1]
		if (last.Kind == TurnPrompt || last.Kind == TurnFollowup) && promptSummaryEqual(last.Summary, trimmedSummary) {
			if entry.Kind == "followup_prompt" {
				last.EntryID = entry.EntryID
				last.Kind = kind
				last.Timestamp = entry.Timestamp
				last.ShortTimestamp = shortTimestamp(entry.Timestamp)
			}
			applyCheckpointMetadata(last, latestCheckpointID, latestCheckpoint)
			return turns
		}
	}

	turn := Turn{
		EntryID:        entry.EntryID,
		Kind:           kind,
		Timestamp:      entry.Timestamp,
		ShortTimestamp: shortTimestamp(entry.Timestamp),
		Summary:        trimmedSummary,
	}
	applyCheckpointMetadata(&turn, latestCheckpointID, latestCheckpoint)
	return append(turns, turn)
}

func appendDiagnosticTurn(
	turns []Turn,
	entry TimelineTurnEntry,
	summary string,
	latestCheckpointID int,
	latestCheckpoint TurnCheckpointRef,
) []Turn {
	turns = append(turns, Turn{
		EntryID:        entry.EntryID,
		Kind:           TurnAssistant,
		Timestamp:      entry.Timestamp,
		ShortTimestamp: shortTimestamp(entry.Timestamp),
		Summary:        strings.TrimSpace(summary),
	})
	applyCheckpointMetadata(&turns[len(turns)-1], latestCheckpointID, latestCheckpoint)
	return turns
}

func promptSummaryEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func applyCheckpointMetadata(turn *Turn, checkpointID int, checkpoint TurnCheckpointRef) {
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
	return render.ShouldEnrichActivitySummary(summary)
}

func enrichTurnSummary(summary string, checkpoint TurnCheckpointRef) string {
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

func sameToolIdentity(a, b ToolCall) bool {
	aID := strings.TrimSpace(a.ID)
	bID := strings.TrimSpace(b.ID)
	if aID != "" && bID != "" {
		return aID == bID
	}
	aCommand := strings.TrimSpace(a.Command)
	bCommand := strings.TrimSpace(b.Command)
	if aCommand != "" && bCommand != "" {
		return aCommand == bCommand
	}
	aName := strings.TrimSpace(a.Name)
	bName := strings.TrimSpace(b.Name)
	return aName != "" && aName == bName
}
