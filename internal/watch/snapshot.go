package watch

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

const workspacePageLimit = 500

// Snapshot is one full workspace state refresh composed from daemon read APIs.
type Snapshot struct {
	Repos       []daemon.RepoDTO
	Worktrees   []daemon.WorktreeDTO
	Invocations []daemon.InvocationDTO
	Checks      map[string]daemon.InvocationCheckData
	Warnings    []string
	UpdatedAt   time.Time
}

func loadWorkspaceSnapshot(ctx context.Context, client *daemonclient.Client) (Snapshot, error) {
	if client == nil {
		return Snapshot{}, errors.New(errors.EInternal, "watch runtime requires a daemon client")
	}

	reposResult, err := client.ListRepos(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	worktrees, err := loadAllWorkspaceWorktrees(ctx, client)
	if err != nil {
		return Snapshot{}, err
	}

	invocations, err := loadAllWorkspaceInvocations(ctx, client)
	if err != nil {
		return Snapshot{}, err
	}

	checks, warnings := loadWorkspaceChecks(ctx, client, invocations)

	return Snapshot{
		Repos:       reposResult.Data.Repos,
		Worktrees:   worktrees,
		Invocations: invocations,
		Checks:      checks,
		Warnings:    warnings,
		UpdatedAt:   time.Now().UTC(),
	}, nil
}

func loadAllWorkspaceWorktrees(ctx context.Context, client *daemonclient.Client) ([]daemon.WorktreeDTO, error) {
	worktrees := make([]daemon.WorktreeDTO, 0, 128)
	cursor := ""

	for {
		result, err := client.ListWorktrees(ctx, daemonclient.ListWorktreesOpts{
			State:  "all",
			Limit:  workspacePageLimit,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}

		worktrees = append(worktrees, result.Data.Worktrees...)
		if result.Data.NextCursor == "" {
			return worktrees, nil
		}
		if result.Data.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "worktree pagination cursor did not advance")
		}
		cursor = result.Data.NextCursor
	}
}

func loadAllWorkspaceInvocations(ctx context.Context, client *daemonclient.Client) ([]daemon.InvocationDTO, error) {
	invocations := make([]daemon.InvocationDTO, 0, 128)
	cursor := ""

	for {
		result, err := client.ListInvocations(ctx, daemonclient.ListInvocationsOpts{
			State:  "all",
			Limit:  workspacePageLimit,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}

		invocations = append(invocations, result.Data.Invocations...)
		if result.Data.NextCursor == "" {
			break
		}
		if result.Data.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "invocation pagination cursor did not advance")
		}
		cursor = result.Data.NextCursor
	}

	sort.Slice(invocations, func(i, j int) bool {
		if invocations[i].SortKey != invocations[j].SortKey {
			return invocations[i].SortKey < invocations[j].SortKey
		}
		if invocations[i].StartedAt != invocations[j].StartedAt {
			return invocations[i].StartedAt > invocations[j].StartedAt
		}
		return invocations[i].InvocationID < invocations[j].InvocationID
	})

	return invocations, nil
}

func loadWorkspaceChecks(ctx context.Context, client *daemonclient.Client, invocations []daemon.InvocationDTO) (map[string]daemon.InvocationCheckData, []string) {
	checks := make(map[string]daemon.InvocationCheckData, len(invocations))
	warnings := make([]string, 0)

	for _, inv := range invocations {
		result, err := client.GetInvocationCheck(ctx, inv.InvocationID, inv.RepoID)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("check refresh failed for %s: %v", inv.InvocationID, err))
			continue
		}
		checks[inv.InvocationID] = result.Data
	}

	return checks, warnings
}
