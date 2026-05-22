package commands

import (
	"context"
	"fmt"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

const maxHistoryProjectionEntries = 2000

func fetchAllTimelineEntries(ctx context.Context, client *daemonclient.Client, invocationRef, repoID string) ([]daemon.TimelineEntryDTO, error) {
	entries, err := client.DrainInvocationTimeline(ctx, invocationRef, repoID, daemon.GetTimelineParams{Limit: 500})
	if err != nil {
		return nil, err
	}
	if len(entries) > maxHistoryProjectionEntries {
		return nil, errors.NewWithDetails(
			errors.EInvalidArgument,
			fmt.Sprintf("history projection supports at most %d timeline entries", maxHistoryProjectionEntries),
			map[string]string{
				"hint": "narrow invocation scope or use explicit --checkpoint <id>",
			},
		)
	}
	return entries, nil
}

func fetchAllCheckpoints(ctx context.Context, client *daemonclient.Client, invocationRef, repoID string) ([]daemon.CheckpointDTO, error) {
	checkpoints, err := client.DrainInvocationCheckpoints(ctx, invocationRef, repoID, daemon.ListCheckpointsParams{Limit: 500})
	if err != nil {
		return nil, err
	}
	if len(checkpoints) > maxHistoryProjectionEntries {
		return nil, errors.NewWithDetails(
			errors.EInvalidArgument,
			fmt.Sprintf("history projection supports at most %d checkpoints", maxHistoryProjectionEntries),
			map[string]string{
				"hint": "use explicit --checkpoint <id> for very large histories",
			},
		)
	}
	return checkpoints, nil
}
