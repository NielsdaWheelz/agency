package daemon

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

type resolvedTurnDiffRange struct {
	FromCommit  string
	ToCommit    string
	TurnContext DiffTurnContext
}

type timelineCheckpointMarker struct {
	EntryIndex   int
	CheckpointID int
}

func hasTurnSelector(params GetDiffParams) bool {
	return strings.TrimSpace(params.TurnID) != "" ||
		(strings.TrimSpace(params.TurnStartID) != "" && strings.TrimSpace(params.TurnEndID) != "")
}

func validateGetDiffParams(params GetDiffParams) error {
	turnID := strings.TrimSpace(params.TurnID)
	turnStart := strings.TrimSpace(params.TurnStartID)
	turnEnd := strings.TrimSpace(params.TurnEndID)

	if turnID != "" && (turnStart != "" || turnEnd != "") {
		return errors.NewWithDetails(
			errors.EInvalidArgument,
			"use either 'turn' or 'turn_start'/'turn_end', not both",
			map[string]string{
				"param": "turn",
			},
		)
	}
	if turnID == "" && (turnStart != "" || turnEnd != "") {
		if turnStart == "" || turnEnd == "" {
			return errors.NewWithDetails(
				errors.EInvalidArgument,
				"turn range requires both 'turn_start' and 'turn_end'",
				map[string]string{
					"param": "turn_range",
				},
			)
		}
	}
	return nil
}

func (s *Server) resolveTurnDiffContext(record *resolvedInvocation, params GetDiffParams) (*resolvedTurnDiffRange, error) {
	if !hasTurnSelector(params) {
		return nil, nil
	}
	if err := validateGetDiffParams(params); err != nil {
		return nil, err
	}

	entries, err := s.collectTimelineEntries(record)
	if err != nil {
		return nil, errors.Wrap(errors.EStoreCorrupt, "failed to read invocation timeline", err)
	}
	if len(entries) == 0 {
		return nil, errors.NewWithDetails(
			errors.ECheckpointNotFound,
			"turn-aware diff requires invocation timeline entries",
			map[string]string{
				"hint": "use 'agency agent <invocation> history' to inspect available turns",
			},
		)
	}

	selector, startTurnID, endTurnID := resolveTurnSelectorBounds(params)
	entryIndexByID := make(map[string]int, len(entries))
	for i, entry := range entries {
		entryIndexByID[entry.dto.EntryID] = i
	}

	checkpointsPath := s.store.InvocationCheckpointsPath(record.RepoID, record.InvocationID)
	cpFile, err := checkpoint.LoadCheckpointsFile(s.fsys, checkpointsPath)
	if err != nil {
		return nil, errors.Wrap(errors.EStoreCorrupt, "failed to load invocation checkpoints from checkpoints.json", err)
	}
	if cpFile == nil || len(cpFile.Checkpoints) == 0 {
		return nil, errors.NewWithDetails(
			errors.ECheckpointNotFound,
			"turn-aware diff requires checkpoints for deterministic mapping",
			map[string]string{
				"hint": "create checkpoints and retry, or request full invocation diff without turn selector",
			},
		)
	}

	checkpointCommitByID := make(map[int]string, len(cpFile.Checkpoints))
	for _, cp := range cpFile.Checkpoints {
		if cp.ID > 0 && cp.SnapshotCommit != "" {
			checkpointCommitByID[cp.ID] = cp.SnapshotCommit
		}
	}

	markers := make([]timelineCheckpointMarker, 0, len(entries))
	for i, entry := range entries {
		checkpointID, ok := checkpointIDFromTimelineEntryDTO(entry.dto)
		if !ok {
			continue
		}
		if _, exists := checkpointCommitByID[checkpointID]; !exists {
			continue
		}
		markers = append(markers, timelineCheckpointMarker{
			EntryIndex:   i,
			CheckpointID: checkpointID,
		})
	}
	if len(markers) == 0 {
		return nil, errors.NewWithDetails(
			errors.ECheckpointNotFound,
			"timeline has no checkpoint markers that map to available checkpoints",
			map[string]string{
				"hint": "verify checkpoint history and retry",
			},
		)
	}

	turns := ProjectTimelineTurns(timelineEntriesFromSortable(entries), checkpointDTOsFromCheckpoints(cpFile.Checkpoints))
	turnIndexByID := make(map[string]int, len(turns))
	for i, turn := range turns {
		turnIndexByID[turn.EntryID] = i
	}

	startTurnIndex, startCheckpointID, err := lookupTurnAndCheckpoint(turns, turnIndexByID, startTurnID, "selected turn", "select a later turn or use full invocation diff")
	if err != nil {
		return nil, err
	}
	endTurnIndex, endCheckpointID, err := lookupTurnAndCheckpoint(turns, turnIndexByID, endTurnID, "selected turn range end", "")
	if err != nil {
		return nil, err
	}
	if startTurnIndex > endTurnIndex {
		return nil, errors.NewWithDetails(
			errors.EInvalidArgument,
			"turn range start occurs after turn range end",
			map[string]string{
				"turn_start": startTurnID,
				"turn_end":   endTurnID,
			},
		)
	}

	// Single-turn and tight ranges can map to the same checkpoint. In that case,
	// expand to the next checkpoint boundary when available so turn-aware diff
	// context is meaningfully anchored to a stable interval. If no later boundary
	// exists (for example the latest assistant turn), anchor against the previous
	// checkpoint so the selected checkpoint's file changes remain visible. When the
	// selected checkpoint is the first and only checkpoint, anchor to base_commit.
	useBaseCommitBoundary := false
	if endCheckpointID == startCheckpointID {
		if endIndex, exists := entryIndexByID[endTurnID]; exists {
			if next, found := nextCheckpointAfter(markers, endIndex, endCheckpointID); found {
				endCheckpointID = next.CheckpointID
			}
		}
		if endCheckpointID == startCheckpointID {
			if prev, found := previousCheckpointBefore(cpFile.Checkpoints, startCheckpointID, checkpointCommitByID); found {
				startCheckpointID = prev
			} else if strings.TrimSpace(record.Meta.BaseCommit) != "" {
				useBaseCommitBoundary = true
				startCheckpointID = 0
			}
		}
	}

	fromCommit := ""
	if useBaseCommitBoundary {
		fromCommit = strings.TrimSpace(record.Meta.BaseCommit)
	} else {
		fromCommit = checkpointCommitByID[startCheckpointID]
	}
	toCommit := checkpointCommitByID[endCheckpointID]
	if fromCommit == "" || toCommit == "" {
		return nil, errors.NewWithDetails(
			errors.ECheckpointNotFound,
			"checkpoint mapping resolved to unavailable snapshot commits",
			map[string]string{
				"start_checkpoint_id": fmt.Sprintf("%d", startCheckpointID),
				"end_checkpoint_id":   fmt.Sprintf("%d", endCheckpointID),
			},
		)
	}

	return &resolvedTurnDiffRange{
		FromCommit: fromCommit,
		ToCommit:   toCommit,
		TurnContext: DiffTurnContext{
			Selector:          selector,
			StartCheckpointID: startCheckpointID,
			EndCheckpointID:   endCheckpointID,
			FromCommit:        fromCommit,
			ToCommit:          toCommit,
		},
	}, nil
}

// lookupTurnAndCheckpoint resolves turnID to its index within turns and its
// associated CheckpointID. desc is interpolated into the not-found error
// message; checkpointHint, when non-empty, is attached to the
// no-checkpoint-mapping error to guide the caller.
func lookupTurnAndCheckpoint(turns []Turn, turnIndexByID map[string]int, turnID, desc, checkpointHint string) (int, int, error) {
	idx, ok := turnIndexByID[turnID]
	if !ok {
		return 0, 0, errors.NewWithDetails(
			errors.EInvalidArgument,
			desc+" was not found in invocation timeline",
			map[string]string{"turn_id": turnID},
		)
	}
	cpID := turns[idx].CheckpointID
	if cpID <= 0 {
		details := map[string]string{"turn_id": turnID}
		if checkpointHint != "" {
			details["hint"] = checkpointHint
		}
		return 0, 0, errors.NewWithDetails(
			errors.ECheckpointNotFound,
			"no checkpoint mapping exists for "+desc,
			details,
		)
	}
	return idx, cpID, nil
}

func resolveTurnSelectorBounds(params GetDiffParams) (DiffTurnSelector, string, string) {
	turnID := strings.TrimSpace(params.TurnID)
	if turnID != "" {
		return DiffTurnSelector{
			Kind:   "single",
			TurnID: turnID,
		}, turnID, turnID
	}
	return DiffTurnSelector{
		Kind:        "range",
		StartTurnID: strings.TrimSpace(params.TurnStartID),
		EndTurnID:   strings.TrimSpace(params.TurnEndID),
	}, strings.TrimSpace(params.TurnStartID), strings.TrimSpace(params.TurnEndID)
}

func nextCheckpointAfter(markers []timelineCheckpointMarker, entryIndex int, currentCheckpointID int) (timelineCheckpointMarker, bool) {
	for _, marker := range markers {
		if marker.EntryIndex <= entryIndex {
			continue
		}
		if marker.CheckpointID == currentCheckpointID {
			continue
		}
		return marker, true
	}
	return timelineCheckpointMarker{}, false
}

func previousCheckpointBefore(checkpoints []checkpoint.Checkpoint, checkpointID int, checkpointCommitByID map[int]string) (int, bool) {
	bestID := 0
	for _, cp := range checkpoints {
		if cp.ID <= 0 || cp.ID >= checkpointID {
			continue
		}
		if _, ok := checkpointCommitByID[cp.ID]; !ok {
			continue
		}
		if cp.ID > bestID {
			bestID = cp.ID
		}
	}
	if bestID <= 0 {
		return 0, false
	}
	return bestID, true
}

func checkpointIDFromTimelineEntryDTO(entry TimelineEntryDTO) (int, bool) {
	if entry.Kind != "checkpoint_event" {
		return 0, false
	}
	value, ok := timelineInt64(entry.Data, "checkpoint_id")
	if !ok || value <= 0 {
		return 0, false
	}
	return int(value), true
}

func timelineInt64(data map[string]interface{}, key string) (int64, bool) {
	if data == nil {
		return 0, false
	}
	value, ok := data[key]
	if !ok {
		return 0, false
	}
	switch n := value.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}
