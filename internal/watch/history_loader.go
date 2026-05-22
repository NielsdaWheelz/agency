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

	entries, err := client.DrainInvocationTimeline(ctx, invocationID, repoID, daemon.GetTimelineParams{Limit: 500})
	if err != nil {
		return nil, err
	}
	if len(entries) > maxHistoryEntries {
		return nil, errors.NewWithDetails(
			errors.EInvalidArgument,
			fmt.Sprintf("interactive %s view supports at most %d timeline entries", viewName, maxHistoryEntries),
			map[string]string{
				"hint": "narrow invocation scope or use non-interactive history output",
			},
		)
	}
	return entries, nil
}

func loadAllCheckpoints(ctx context.Context, client *daemonclient.Client, invocationID, repoID string) ([]daemon.CheckpointDTO, error) {
	checkpoints, err := client.DrainInvocationCheckpoints(ctx, invocationID, repoID, daemon.ListCheckpointsParams{Limit: 500})
	if err != nil {
		return nil, err
	}
	if len(checkpoints) > maxHistoryEntries {
		return nil, errors.NewWithDetails(
			errors.EInvalidArgument,
			fmt.Sprintf("interactive history view supports at most %d checkpoints", maxHistoryEntries),
			map[string]string{
				"hint": "use explicit --checkpoint <id> for very large histories",
			},
		)
	}
	return checkpoints, nil
}
