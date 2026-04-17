package commands

import (
	"context"
	"fmt"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

const maxHistoryTUIEntries = 2000

func fetchAllTimelineEntries(ctx context.Context, client *daemonclient.Client, invocationRef, repoID string) ([]daemon.TimelineEntryDTO, error) {
	entries := make([]daemon.TimelineEntryDTO, 0, 128)
	cursor := ""

	for {
		result, err := client.GetInvocationTimeline(ctx, invocationRef, repoID, daemonclient.GetInvocationTimelineOpts{
			Limit:  500,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}

		entries = append(entries, result.Data.Entries...)
		if len(entries) > maxHistoryTUIEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive history view supports at most %d timeline entries", maxHistoryTUIEntries),
				map[string]string{
					"hint": "narrow invocation scope or use explicit --checkpoint <id>",
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

func fetchAllCheckpoints(ctx context.Context, client *daemonclient.Client, invocationRef, repoID string) ([]daemon.CheckpointDTO, error) {
	checkpoints := make([]daemon.CheckpointDTO, 0, 32)
	cursor := ""

	for {
		result, err := client.ListCheckpoints(ctx, invocationRef, repoID, daemonclient.ListCheckpointsOpts{
			Limit:  500,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}

		checkpoints = append(checkpoints, result.Data.Checkpoints...)
		if len(checkpoints) > maxHistoryTUIEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive history view supports at most %d checkpoints", maxHistoryTUIEntries),
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
