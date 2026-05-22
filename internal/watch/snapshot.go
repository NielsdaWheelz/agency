package watch

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

const workspacePageLimit = 500

// snapshot is one full workspace state refresh composed from daemon read APIs.
type snapshot struct {
	Repos       []daemon.RepoDTO
	Worktrees   []daemon.WorktreeDTO
	Invocations []daemon.InvocationDTO
	UpdatedAt   time.Time
}

func loadWorkspaceSnapshot(ctx context.Context, client *daemonclient.Client, repoID, worktreeID, worktreeState, invocationState string) (snapshot, error) {
	if client == nil {
		return snapshot{}, errors.New(errors.EInternal, "watch runtime requires a daemon client")
	}
	if worktreeID != "" && repoID == "" {
		return snapshot{}, errors.New(errors.EInternal, "worktree-scoped workspace load requires repo scope")
	}
	if worktreeState == "" {
		worktreeState = "present"
	}
	if invocationState == "" {
		invocationState = "unresolved"
	}

	reposResult, err := client.ListRepos(ctx)
	if err != nil {
		return snapshot{}, err
	}

	worktrees, err := loadAllWorkspaceWorktrees(ctx, client, repoID, worktreeState)
	if err != nil {
		return snapshot{}, err
	}

	invocations, err := loadAllWorkspaceInvocations(ctx, client, repoID, worktreeID, invocationState)
	if err != nil {
		return snapshot{}, err
	}

	return snapshot{
		Repos:       reposResult.Data.Repos,
		Worktrees:   worktrees,
		Invocations: invocations,
		UpdatedAt:   time.Now().UTC(),
	}, nil
}

func loadAllWorkspaceWorktrees(ctx context.Context, client *daemonclient.Client, repoID, state string) ([]daemon.WorktreeDTO, error) {
	return client.DrainWorktrees(ctx, daemon.ListWorktreesParams{
		RepoID: repoID,
		State:  state,
		Limit:  workspacePageLimit,
	})
}

func loadAllWorkspaceInvocations(ctx context.Context, client *daemonclient.Client, repoID, worktreeID, state string) ([]daemon.InvocationDTO, error) {
	invocations, err := client.DrainInvocations(ctx, daemon.ListInvocationsParams{
		RepoID:      repoID,
		WorktreeRef: worktreeID,
		State:       state,
		Limit:       workspacePageLimit,
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(invocations, func(a, b daemon.InvocationDTO) int {
		if a.SortKey != b.SortKey {
			return cmp.Compare(a.SortKey, b.SortKey)
		}
		if a.StartedAt != b.StartedAt {
			return cmp.Compare(b.StartedAt, a.StartedAt)
		}
		return cmp.Compare(a.InvocationID, b.InvocationID)
	})

	return invocations, nil
}
