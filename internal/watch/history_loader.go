package watch

import (
	"context"
	"fmt"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func loadAllTimelineEntries(ctx context.Context, client *daemonclient.Client, invocationID, repoID, viewName string) ([]daemon.TimelineEntryDTO, error) {
	if client == nil {
		return nil, errors.New(errors.EInternal, "watch runtime requires a daemon client")
	}
	if strings.TrimSpace(invocationID) == "" || strings.TrimSpace(repoID) == "" {
		return nil, errors.New(errors.EInvalidArgument, viewName+" page requires an invocation and repo")
	}

	entries := make([]daemon.TimelineEntryDTO, 0, 128)
	cursor := ""
	for {
		result, err := client.GetInvocationTimeline(ctx, invocationID, repoID, daemonclient.GetInvocationTimelineOpts{
			Limit:  500,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		entries = append(entries, result.Data.Entries...)
		if len(entries) > maxHistoryEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive %s view supports at most %d timeline entries", viewName, maxHistoryEntries),
				map[string]string{
					"hint": "narrow invocation scope or use non-interactive history output",
				},
			)
		}
		if result.Data.NextCursor == "" {
			return entries, nil
		}
		if result.Data.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "timeline pagination cursor did not advance")
		}
		cursor = result.Data.NextCursor
	}
}

func loadAllCheckpoints(ctx context.Context, client *daemonclient.Client, invocationID, repoID string) ([]daemon.CheckpointDTO, error) {
	checkpoints := make([]daemon.CheckpointDTO, 0, 32)
	cursor := ""
	for {
		result, err := client.ListCheckpoints(ctx, invocationID, repoID, daemonclient.ListCheckpointsOpts{
			Limit:  500,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, result.Data.Checkpoints...)
		if len(checkpoints) > maxHistoryEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive history view supports at most %d checkpoints", maxHistoryEntries),
				map[string]string{
					"hint": "use explicit --checkpoint <id> for very large histories",
				},
			)
		}
		if result.Data.NextCursor == "" {
			return checkpoints, nil
		}
		if result.Data.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "checkpoint pagination cursor did not advance")
		}
		cursor = result.Data.NextCursor
	}
}
