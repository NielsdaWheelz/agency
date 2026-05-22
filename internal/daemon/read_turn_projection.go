package daemon

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/render"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Server) collectCanonicalTurnsBestEffort(record *resolvedInvocation, entries []timelineSortableEntry) []Turn {
	checkpointsPath := s.store.InvocationCheckpointsPath(record.RepoID, record.InvocationID)
	cpFile, err := checkpoint.LoadCheckpointsFile(s.fsys, checkpointsPath)
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

func checkpointToDTO(cp checkpoint.Checkpoint) CheckpointDTO {
	return CheckpointDTO{
		ID:                   cp.ID,
		CreatedAt:            cp.CreatedAt,
		Diffstat:             cp.Diffstat,
		SnapshotCommit:       cp.SnapshotCommit,
		IncludesUntracked:    cp.IncludesUntracked,
		Degraded:             !cp.IncludesUntracked,
		Trigger:              cp.Trigger,
		ToolName:             cp.ToolName,
		StreamSeq:            cp.StreamSeq,
		Description:          cp.Description,
		ChangedPaths:         slices.Clone(cp.ChangedPaths),
		ChangedPathCount:     cp.ChangedPathCount,
		ChangedPathTruncated: cp.ChangedPathTruncated,
	}
}

func checkpointDTOsFromCheckpoints(checkpoints []checkpoint.Checkpoint) []CheckpointDTO {
	if len(checkpoints) == 0 {
		return nil
	}

	dtos := make([]CheckpointDTO, len(checkpoints))
	for i, cp := range checkpoints {
		dtos[i] = checkpointToDTO(cp)
	}
	return dtos
}

func turnCheckpointRefsFromDTOs(checkpoints []CheckpointDTO) []turnCheckpointRef {
	if len(checkpoints) == 0 {
		return nil
	}

	refs := make([]turnCheckpointRef, len(checkpoints))
	for i, cp := range checkpoints {
		refs[i] = turnCheckpointRef{
			ID:                   cp.ID,
			Description:          cp.Description,
			Diffstat:             cp.Diffstat,
			ChangedPaths:         cp.ChangedPaths,
			ChangedPathCount:     cp.ChangedPathCount,
			ChangedPathTruncated: cp.ChangedPathTruncated,
		}
	}
	return refs
}

// ProjectTimelineTurns converts daemon timeline and checkpoint DTOs into the
// grouped turn view used by history and restore flows.
func ProjectTimelineTurns(entries []TimelineEntryDTO, checkpoints []CheckpointDTO) []Turn {
	if len(entries) == 0 {
		return nil
	}

	pickerEntries := make([]timelineTurnEntry, len(entries))
	for i, entry := range entries {
		pickerEntries[i] = timelineTurnEntry{
			EntryID:   entry.EntryID,
			Kind:      entry.Kind,
			Timestamp: entry.Timestamp,
			Data:      entry.Data,
		}
	}

	return groupTimelineIntoTurns(pickerEntries, turnCheckpointRefsFromDTOs(checkpoints))
}

// PaginateHistoryTurns returns a stable cursor page over projected turns.
func PaginateHistoryTurns(turns []Turn, cursor string, limit int) ([]Turn, string) {
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
		return []Turn{}, ""
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
func TimelineEntriesForTurn(entries []TimelineEntryDTO, turns []Turn, turnEntryID string) []TimelineEntryDTO {
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

	segment := slices.Clone(entries[startIdx:endIdx])
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

func (s *Server) buildInvocationActivityProjection(
	record *resolvedInvocation,
	state string,
	runnerSummary string,
	entries []timelineSortableEntry,
) (invocationActivityProjection, error) {
	if record == nil || record.Meta == nil {
		return invocationActivityProjection{}, nil
	}

	if entries == nil {
		var err error
		entries, err = s.collectTimelineEntries(record)
		if err != nil {
			return invocationActivityProjection{}, err
		}
	}
	turns := s.collectCanonicalTurnsBestEffort(record, entries)

	projection := invocationActivityProjection{
		Navigation: &InvocationActivityNavigation{
			HistoryCommand: fmt.Sprintf("agency agent %s history --repo %s", record.InvocationID, record.RepoID),
			DiffCommand:    fmt.Sprintf("agency agent %s diff --repo %s", record.InvocationID, record.RepoID),
		},
	}
	if record.Meta.Mode == store.RunnerModeHeaded {
		switch record.Meta.Status {
		case store.InvocationStatusStarting, store.InvocationStatusRunning, store.InvocationStatusStopping:
			projection.Navigation.AttachCommand = fmt.Sprintf(
				"agency agent %s attach --repo %s",
				record.InvocationID,
				record.RepoID,
			)
		}
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
			CheckpointChangedPaths: slices.Clone(latest.CheckpointChangedPaths),
			CheckpointChangedCount: latest.CheckpointChangedCount,
			CheckpointPathsTrimmed: latest.CheckpointPathsTrimmed,
		}
		projection.Navigation.LatestTurnID = latest.EntryID
		if restorableTurnID := latestRestorableTurnID(turns); restorableTurnID != "" {
			projection.Navigation.DiffCommand = fmt.Sprintf(
				"agency agent %s diff --repo %s --turn %s",
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
		statusSummary = strings.TrimSpace(state)
	}
	projection.StatusSummary = truncateActivitySummary(statusSummary)
	return projection, nil
}

func latestRestorableTurnID(turns []Turn) string {
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		if turn.Restorable && strings.TrimSpace(turn.EntryID) != "" {
			return turn.EntryID
		}
	}
	return ""
}

func normalizeLatestTurnSummary(turn Turn) string {
	summary := strings.TrimSpace(turn.Summary)
	if summary != "" {
		return truncateActivitySummary(summary)
	}
	switch turn.Kind {
	case TurnPrompt:
		return truncateActivitySummary("prompt")
	case TurnFollowup:
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
	return truncateActivitySummary(render.TimelineEntrySummary(entry.Kind, render.DecodeTimelinePayload(entry.Data)))
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

func projectLatestActivityToolCalls(calls []ToolCall) []InvocationActivityToolCall {
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
