package daemon

import (
	"slices"
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

// timelineTurnEntry is the input type for grouping projected history turns.
type timelineTurnEntry struct {
	EntryID   string
	Kind      string
	Timestamp string
	Data      map[string]interface{}
}

// turnCheckpointRef identifies a valid checkpoint that can be restored.
type turnCheckpointRef struct {
	ID int

	Description          string
	Diffstat             string
	ChangedPaths         []string
	ChangedPathCount     int
	ChangedPathTruncated bool
}

// groupTimelineIntoTurns converts a flat timeline into grouped conversation turns.
func groupTimelineIntoTurns(entries []timelineTurnEntry, checkpoints []turnCheckpointRef) []Turn {
	if len(entries) == 0 {
		return nil
	}

	checkpointSet := make(map[int]turnCheckpointRef, len(checkpoints))
	for _, cp := range checkpoints {
		checkpointSet[cp.ID] = cp
	}

	var turns []Turn
	latestCheckpointID := 0
	var latestCheckpoint turnCheckpointRef

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
				mergeToolUseIntoAssistantTurn(&turns[len(turns)-1], entry)
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

// mergeToolUseIntoAssistantTurn folds a tool_use timeline entry into the
// trailing assistant turn: matches by tool ID when set, falls back to
// completing the most recent unfinished tool when an exit code arrives, and
// otherwise appends a fresh ToolCall.
func mergeToolUseIntoAssistantTurn(turn *Turn, entry timelineTurnEntry) {
	payload := render.DecodeTimelinePayload(entry.Data)
	tc := ToolCall{ID: payload.ToolID, Name: payload.Name, Command: payload.Command}
	if payload.HasExitCode {
		tc.ExitCode = payload.ExitCode
		tc.HasExit = true
	}
	if tc.ID != "" {
		for i := len(turn.ToolCalls) - 1; i >= 0; i-- {
			lastTool := &turn.ToolCalls[i]
			if strings.TrimSpace(lastTool.ID) != tc.ID {
				continue
			}
			fillToolCall(lastTool, tc)
			return
		}
	}
	if tc.HasExit && len(turn.ToolCalls) > 0 {
		lastTool := &turn.ToolCalls[len(turn.ToolCalls)-1]
		if !lastTool.HasExit && sameToolIdentity(*lastTool, tc) {
			fillToolCall(lastTool, tc)
			return
		}
	}
	turn.ToolCalls = append(turn.ToolCalls, tc)
}

// fillToolCall populates dst's empty Name/Command fields from src and copies
// the exit code if src carries one. Used to merge partial tool_use entries
// (e.g. tool_start followed by tool_end) into the same ToolCall slot.
func fillToolCall(dst *ToolCall, src ToolCall) {
	if strings.TrimSpace(dst.Name) == "" {
		dst.Name = src.Name
	}
	if strings.TrimSpace(dst.Command) == "" {
		dst.Command = src.Command
	}
	if src.HasExit {
		dst.ExitCode = src.ExitCode
		dst.HasExit = true
	}
}

func appendPromptLikeTurn(
	turns []Turn,
	entry timelineTurnEntry,
	kind TurnKind,
	summary string,
	latestCheckpointID int,
	latestCheckpoint turnCheckpointRef,
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
	entry timelineTurnEntry,
	summary string,
	latestCheckpointID int,
	latestCheckpoint turnCheckpointRef,
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

func applyCheckpointMetadata(turn *Turn, checkpointID int, checkpoint turnCheckpointRef) {
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
	turn.CheckpointChangedPaths = slices.Clone(checkpoint.ChangedPaths)
	turn.CheckpointChangedCount = checkpoint.ChangedPathCount
	turn.CheckpointPathsTrimmed = checkpoint.ChangedPathTruncated

	if shouldEnrichTurnSummary(turn.Summary) {
		turn.Summary = enrichTurnSummary(turn.Summary, checkpoint)
	}
}

func shouldEnrichTurnSummary(summary string) bool {
	return render.ShouldEnrichActivitySummary(summary)
}

func enrichTurnSummary(summary string, checkpoint turnCheckpointRef) string {
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
