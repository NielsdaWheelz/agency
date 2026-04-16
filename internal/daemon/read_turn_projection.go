package daemon

import (
	"fmt"
	"os"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/tui/historypicker"
)

func (s *Server) collectCanonicalTurnsBestEffort(record *resolvedInvocation, entries []timelineSortableEntry) []historypicker.Turn {
	checkpointsDir := s.Store.InvocationDir(record.RepoID, record.InvocationID)
	cpFile, err := checkpoint.LoadCheckpointsFile(s.FS, checkpointsDir)
	if err != nil && !os.IsNotExist(err) {
		// Best-effort only: check/read surfaces should still work when
		// checkpoint metadata is unavailable or malformed.
		return ProjectTimelineTurns(timelineEntriesFromSortable(entries), nil)
	}
	if cpFile == nil {
		return ProjectTimelineTurns(timelineEntriesFromSortable(entries), nil)
	}
	return ProjectTimelineTurns(timelineEntriesFromSortable(entries), checkpointDTOsFromCheckpoints(cpFile.Checkpoints))
}

type invocationActivityProjection struct {
	StatusSummary  string
	LatestActivity *InvocationLatestActivity
	Navigation     *InvocationActivityNavigation
}

const maxActivitySummaryChars = 240

func timelineEntriesFromSortable(entries []timelineSortableEntry) []TimelineEntryDTO {
	if len(entries) == 0 {
		return nil
	}

	dtos := make([]TimelineEntryDTO, len(entries))
	for i, entry := range entries {
		dtos[i] = entry.dto
	}
	return dtos
}

func checkpointDTOsFromCheckpoints(checkpoints []checkpoint.Checkpoint) []CheckpointDTO {
	if len(checkpoints) == 0 {
		return nil
	}

	dtos := make([]CheckpointDTO, len(checkpoints))
	for i, cp := range checkpoints {
		dtos[i] = CheckpointDTO{
			ID:                   cp.ID,
			CreatedAt:            cp.CreatedAt,
			Diffstat:             cp.Diffstat,
			SnapshotCommit:       cp.SnapshotCommit,
			IncludesUntracked:    cp.IncludesUntracked,
			Trigger:              cp.Trigger,
			ToolName:             cp.ToolName,
			StreamSeq:            cp.StreamSeq,
			Description:          cp.Description,
			ChangedPaths:         append([]string(nil), cp.ChangedPaths...),
			ChangedPathCount:     cp.ChangedPathCount,
			ChangedPathTruncated: cp.ChangedPathTruncated,
		}
	}
	return dtos
}

// ProjectTimelineTurns converts daemon timeline and checkpoint DTOs into the
// grouped turn view used by history and restart flows.
func ProjectTimelineTurns(entries []TimelineEntryDTO, checkpoints []CheckpointDTO) []historypicker.Turn {
	if len(entries) == 0 {
		return nil
	}

	pickerEntries := make([]historypicker.TimelineEntry, len(entries))
	for i, entry := range entries {
		pickerEntries[i] = historypicker.TimelineEntry{
			EntryID:   entry.EntryID,
			Kind:      entry.Kind,
			Timestamp: entry.Timestamp,
			Data:      entry.Data,
		}
	}

	pickerCheckpoints := make([]historypicker.CheckpointRef, len(checkpoints))
	for i, cp := range checkpoints {
		pickerCheckpoints[i] = historypicker.CheckpointRef{
			ID:                   cp.ID,
			Description:          cp.Description,
			Diffstat:             cp.Diffstat,
			ChangedPaths:         cp.ChangedPaths,
			ChangedPathCount:     cp.ChangedPathCount,
			ChangedPathTruncated: cp.ChangedPathTruncated,
		}
	}

	return historypicker.GroupTimelineIntoTurns(pickerEntries, pickerCheckpoints)
}

// PaginateHistoryTurns returns a stable cursor page over projected turns.
func PaginateHistoryTurns(turns []historypicker.Turn, cursor string, limit int) ([]historypicker.Turn, string) {
	if len(turns) == 0 {
		return nil, ""
	}

	start := 0
	cursor = strings.TrimSpace(cursor)
	if cursor != "" {
		for i, turn := range turns {
			if turn.EntryID == cursor {
				start = i + 1
				break
			}
		}
	}

	if start >= len(turns) {
		return []historypicker.Turn{}, ""
	}

	end := start + limit
	if end > len(turns) {
		end = len(turns)
	}

	page := turns[start:end]
	nextCursor := ""
	if end < len(turns) && len(page) > 0 {
		nextCursor = page[len(page)-1].EntryID
	}
	return page, nextCursor
}

// TimelineEntriesForTurn returns the raw timeline entries that belong to a turn.
func TimelineEntriesForTurn(entries []TimelineEntryDTO, turns []historypicker.Turn, turnEntryID string) []TimelineEntryDTO {
	if len(entries) == 0 || len(turns) == 0 || strings.TrimSpace(turnEntryID) == "" {
		return nil
	}

	turnIdx := -1
	for i, turn := range turns {
		if turn.EntryID == turnEntryID {
			turnIdx = i
			break
		}
	}
	if turnIdx < 0 {
		return nil
	}

	startIdx := -1
	for i, entry := range entries {
		if entry.EntryID == turnEntryID {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		return nil
	}

	endIdx := len(entries)
	if turnIdx+1 < len(turns) {
		nextTurnEntryID := turns[turnIdx+1].EntryID
		for i := startIdx + 1; i < len(entries); i++ {
			if entries[i].EntryID == nextTurnEntryID {
				endIdx = i
				break
			}
		}
	}
	if startIdx >= endIdx {
		return nil
	}

	segment := make([]TimelineEntryDTO, endIdx-startIdx)
	copy(segment, entries[startIdx:endIdx])
	filtered := make([]TimelineEntryDTO, 0, len(segment))
	for _, entry := range segment {
		if includeInLastTurnJSON(entry.Kind) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func includeInLastTurnJSON(kind string) bool {
	switch kind {
	case "session_start", "final", "error", "raw_log_coverage", "checkpoint_event", "invocation_event", "usage", "status":
		return false
	default:
		return true
	}
}

// HistoryTurnExists reports whether a projected turn ID is present.
func HistoryTurnExists(turns []historypicker.Turn, entryID string) bool {
	for _, turn := range turns {
		if turn.EntryID == entryID {
			return true
		}
	}
	return false
}

func (s *Server) buildInvocationActivityProjection(
	record *resolvedInvocation,
	displayStatus string,
	runnerSummary string,
	entries []timelineSortableEntry,
) invocationActivityProjection {
	if record == nil || record.Meta == nil {
		return invocationActivityProjection{}
	}

	if entries == nil {
		entries = s.collectTimelineEntries(record)
	}
	turns := s.collectCanonicalTurnsBestEffort(record, entries)

	projection := invocationActivityProjection{
		Navigation: &InvocationActivityNavigation{
			HistoryCommand: fmt.Sprintf("agency agent history %s --repo %s", record.InvocationID, record.RepoID),
			DiffCommand:    fmt.Sprintf("agency agent diff %s --repo %s", record.InvocationID, record.RepoID),
		},
	}

	if len(turns) > 0 {
		latest := turns[len(turns)-1]
		latestSummary := normalizeLatestTurnSummary(latest)
		projection.LatestActivity = &InvocationLatestActivity{
			TurnID:                 latest.EntryID,
			Kind:                   string(latest.Kind),
			Timestamp:              latest.Timestamp,
			Summary:                latestSummary,
			ToolCallCount:          len(latest.ToolCalls),
			ToolCalls:              projectLatestActivityToolCalls(latest.ToolCalls),
			CheckpointID:           latest.CheckpointID,
			Restorable:             latest.Restorable,
			CheckpointDescription:  latest.CheckpointDescription,
			CheckpointDiffstat:     latest.CheckpointDiffstat,
			CheckpointChangedPaths: append([]string(nil), latest.CheckpointChangedPaths...),
			CheckpointChangedCount: latest.CheckpointChangedCount,
			CheckpointPathsTrimmed: latest.CheckpointPathsTrimmed,
		}
		projection.Navigation.LatestTurnID = latest.EntryID
		if restorableTurnID := latestRestorableTurnID(turns); restorableTurnID != "" {
			projection.Navigation.DiffCommand = fmt.Sprintf(
				"agency agent diff %s --repo %s --turn %s",
				record.InvocationID,
				record.RepoID,
				restorableTurnID,
			)
		}
	} else if latestEntry, ok := latestMeaningfulTimelineEntry(entries); ok {
		latestSummary := summarizeTimelineEntryDTO(latestEntry.dto)
		if latestSummary != "" {
			projection.LatestActivity = &InvocationLatestActivity{
				TurnID:    latestEntry.dto.EntryID,
				Kind:      latestEntry.dto.Kind,
				Timestamp: latestEntry.dto.Timestamp,
				Summary:   latestSummary,
			}
			projection.Navigation.LatestTurnID = latestEntry.dto.EntryID
		}
	}

	statusSummary := strings.TrimSpace(runnerSummary)
	if statusSummary == "" && projection.LatestActivity != nil {
		statusSummary = strings.TrimSpace(projection.LatestActivity.Summary)
	}
	if statusSummary == "" {
		statusSummary = strings.TrimSpace(displayStatus)
	}
	projection.StatusSummary = truncateActivitySummary(statusSummary)
	return projection
}

func latestRestorableTurnID(turns []historypicker.Turn) string {
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		if turn.Restorable && strings.TrimSpace(turn.EntryID) != "" {
			return turn.EntryID
		}
	}
	return ""
}

func normalizeLatestTurnSummary(turn historypicker.Turn) string {
	summary := strings.TrimSpace(turn.Summary)
	if summary != "" {
		return truncateActivitySummary(summary)
	}
	switch turn.Kind {
	case historypicker.TurnPrompt:
		return truncateActivitySummary("prompt")
	case historypicker.TurnFollowup:
		return truncateActivitySummary("follow-up prompt")
	default:
		return truncateActivitySummary("assistant turn")
	}
}

func latestMeaningfulTimelineEntry(entries []timelineSortableEntry) (timelineSortableEntry, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if isMeaningfulTimelineKind(entries[i].dto.Kind) {
			return entries[i], true
		}
	}
	return timelineSortableEntry{}, false
}

func isMeaningfulTimelineKind(kind string) bool {
	switch kind {
	case "session_start", "final", "raw_log_coverage", "checkpoint_event", "invocation_event", "usage", "status", "parse_error":
		return false
	default:
		return true
	}
}

func summarizeTimelineEntryDTO(entry TimelineEntryDTO) string {
	switch entry.Kind {
	case "message":
		if text := strings.TrimSpace(timelineDataString(entry.Data, "text")); text != "" {
			return truncateActivitySummary(text)
		}
		if role := strings.TrimSpace(timelineDataString(entry.Data, "role")); role == "user" {
			return "user message"
		}
		return "assistant message"
	case "prompt_seed":
		if text := strings.TrimSpace(timelineDataString(entry.Data, "text")); text != "" {
			return truncateActivitySummary(text)
		}
		return "prompt"
	case "followup_prompt":
		if text := strings.TrimSpace(timelineDataString(entry.Data, "text")); text != "" {
			return truncateActivitySummary(text)
		}
		return "follow-up prompt"
	case "tool_use":
		if cmd := strings.TrimSpace(timelineDataString(entry.Data, "command")); cmd != "" {
			return truncateActivitySummary(cmd)
		}
		if name := strings.TrimSpace(timelineDataString(entry.Data, "name")); name != "" {
			return "tool: " + name
		}
		return "tool activity"
	default:
		if text := strings.TrimSpace(timelineDataString(entry.Data, "text")); text != "" {
			return truncateActivitySummary(text)
		}
		return truncateActivitySummary(strings.TrimSpace(strings.ReplaceAll(entry.Kind, "_", " ")))
	}
}

func timelineDataString(data map[string]interface{}, key string) string {
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

func applyInvocationActivityProjection(dto *InvocationDTO, projection invocationActivityProjection) {
	if dto == nil {
		return
	}
	dto.StatusSummary = projection.StatusSummary
	dto.LatestActivity = projection.LatestActivity
	dto.Navigation = projection.Navigation
}

func truncateActivitySummary(summary string) string {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= maxActivitySummaryChars {
		return trimmed
	}
	return strings.TrimSpace(string(runes[:maxActivitySummaryChars])) + "..."
}

func projectLatestActivityToolCalls(calls []historypicker.ToolCall) []InvocationActivityToolCall {
	if len(calls) == 0 {
		return nil
	}
	projected := make([]InvocationActivityToolCall, 0, len(calls))
	for _, call := range calls {
		projected = append(projected, InvocationActivityToolCall{
			Name:     strings.TrimSpace(call.Name),
			Command:  strings.TrimSpace(call.Command),
			HasExit:  call.HasExit,
			ExitCode: call.ExitCode,
		})
	}
	return projected
}
