package daemon

import (
	"os"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
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
