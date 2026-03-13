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

	entries := s.collectTimelineEntries(record)
	if len(entries) == 0 {
		return nil, errors.NewWithDetails(
			errors.ECheckpointNotFound,
			"turn-aware diff requires invocation timeline entries",
			map[string]string{
				"hint": "use 'agency agent history <invocation>' to inspect available turns",
			},
		)
	}

	selector, startTurnID, endTurnID := resolveTurnSelectorBounds(params)
	entryIndexByID := make(map[string]int, len(entries))
	for i, entry := range entries {
		entryIndexByID[entry.dto.EntryID] = i
	}

	checkpointsDir := s.Store.ResolveInvocationCheckpointsDir(record.RepoID, record.InvocationID)
	cpFile, err := checkpoint.LoadCheckpointsFile(s.FS, checkpointsDir)
	if err != nil || len(cpFile.Checkpoints) == 0 {
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

	turns := groupTimelineEntriesIntoTurns(entries, cpFile.Checkpoints)
	turnIndexByID := make(map[string]int, len(turns))
	for i, turn := range turns {
		turnIndexByID[turn.EntryID] = i
	}

	startTurnIndex, ok := turnIndexByID[startTurnID]
	if !ok {
		return nil, errors.NewWithDetails(
			errors.EInvalidArgument,
			"selected turn was not found in invocation timeline",
			map[string]string{
				"turn_id": startTurnID,
			},
		)
	}
	endTurnIndex, ok := turnIndexByID[endTurnID]
	if !ok {
		return nil, errors.NewWithDetails(
			errors.EInvalidArgument,
			"selected turn was not found in invocation timeline",
			map[string]string{
				"turn_id": endTurnID,
			},
		)
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

	startCheckpointID := turns[startTurnIndex].CheckpointID
	if startCheckpointID <= 0 {
		return nil, errors.NewWithDetails(
			errors.ECheckpointNotFound,
			"no checkpoint mapping exists for selected turn",
			map[string]string{
				"turn_id": startTurnID,
				"hint":    "select a later turn or use full invocation diff",
			},
		)
	}
	endCheckpointID := turns[endTurnIndex].CheckpointID
	if endCheckpointID <= 0 {
		return nil, errors.NewWithDetails(
			errors.ECheckpointNotFound,
			"no checkpoint mapping exists for selected turn range end",
			map[string]string{
				"turn_id": endTurnID,
			},
		)
	}

	// Single-turn and tight ranges can map to the same checkpoint. In that case,
	// expand to the next checkpoint boundary when available so turn-aware diff
	// context is meaningfully anchored to a stable interval.
	if endCheckpointID == startCheckpointID {
		if endIndex, exists := entryIndexByID[endTurnID]; exists {
			if next, found := nextCheckpointAfter(markers, endIndex, endCheckpointID); found {
				endCheckpointID = next.CheckpointID
			}
		}
	}

	fromCommit := checkpointCommitByID[startCheckpointID]
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
