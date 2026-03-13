package daemon

import (
	"fmt"
	"os"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/tui/historypicker"
)

func timelineEntriesToPickerEntries(entries []timelineSortableEntry) []historypicker.TimelineEntry {
	if len(entries) == 0 {
		return nil
	}

	pickerEntries := make([]historypicker.TimelineEntry, len(entries))
	for i, entry := range entries {
		pickerEntries[i] = historypicker.TimelineEntry{
			EntryID:   entry.dto.EntryID,
			Kind:      entry.dto.Kind,
			Timestamp: entry.dto.Timestamp,
			Data:      entry.dto.Data,
		}
	}
	return pickerEntries
}

func checkpointRefsFromCheckpoints(checkpoints []checkpoint.Checkpoint) []historypicker.CheckpointRef {
	if len(checkpoints) == 0 {
		return nil
	}

	refs := make([]historypicker.CheckpointRef, len(checkpoints))
	for i, cp := range checkpoints {
		refs[i] = historypicker.CheckpointRef{
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

func groupTimelineEntriesIntoTurns(entries []timelineSortableEntry, checkpoints []checkpoint.Checkpoint) []historypicker.Turn {
	return historypicker.GroupTimelineIntoTurns(
		timelineEntriesToPickerEntries(entries),
		checkpointRefsFromCheckpoints(checkpoints),
	)
}

func (s *Server) collectCanonicalTurnsBestEffort(record *resolvedInvocation, entries []timelineSortableEntry) []historypicker.Turn {
	checkpointsDir := s.Store.SandboxDir(record.RepoID, record.InvocationID)
	cpFile, err := checkpoint.LoadCheckpointsFile(s.FS, checkpointsDir)
	if err != nil && !os.IsNotExist(err) {
		// Best-effort only: review/read surfaces should still work when
		// checkpoint metadata is unavailable or malformed.
		return groupTimelineEntriesIntoTurns(entries, nil)
	}
	if cpFile == nil {
		return groupTimelineEntriesIntoTurns(entries, nil)
	}
	return groupTimelineEntriesIntoTurns(entries, cpFile.Checkpoints)
}

type invocationActivityProjection struct {
	StatusSummary  string
	LatestActivity *InvocationLatestActivity
	Navigation     *InvocationActivityNavigation
}

const maxActivitySummaryChars = 240

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
			TurnID:    latest.EntryID,
			Kind:      string(latest.Kind),
			Timestamp: latest.Timestamp,
			Summary:   latestSummary,
		}
		projection.Navigation.LatestTurnID = latest.EntryID
		projection.Navigation.DiffCommand = fmt.Sprintf(
			"agency agent diff %s --repo %s --turn %s",
			record.InvocationID,
			record.RepoID,
			latest.EntryID,
		)
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
			projection.Navigation.DiffCommand = fmt.Sprintf(
				"agency agent diff %s --repo %s --turn %s",
				record.InvocationID,
				record.RepoID,
				latestEntry.dto.EntryID,
			)
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

func loadRunnerSummaryBestEffort(sandboxPath string) string {
	sandboxPath = strings.TrimSpace(sandboxPath)
	if sandboxPath == "" {
		return ""
	}
	statusMeta, _, err := runnerstatus.LoadWithModTime(sandboxPath)
	if err != nil || statusMeta == nil {
		return ""
	}
	if statusMeta.SchemaVersion != runnerstatus.SchemaVersion {
		return ""
	}
	if err := statusMeta.Validate(); err != nil {
		return ""
	}
	return strings.TrimSpace(statusMeta.Summary)
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
