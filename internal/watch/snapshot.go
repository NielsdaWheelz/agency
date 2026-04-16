package watch

import (
	"context"
	"fmt"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

const defaultPageLimit = 500

// Snapshot is one full workspace state refresh composed from daemon read APIs.
type Snapshot struct {
	Repos       []daemon.RepoDTO
	Worktrees   []daemon.WorktreeDTO
	Invocations []daemon.InvocationDTO
	Checks      map[string]daemon.InvocationCheckData
	Warnings    []string
	UpdatedAt   time.Time
}

type snapshotClient interface {
	ListRepos(ctx context.Context) (*daemon.Result[daemon.ListReposData], error)
	ListWorktrees(ctx context.Context, opts daemonclient.ListWorktreesOpts) (*daemon.Result[daemon.ListWorktreesData], error)
	ListInvocations(ctx context.Context, opts daemonclient.ListInvocationsOpts) (*daemon.Result[daemon.ListInvocationsData], error)
	GetInvocationCheck(ctx context.Context, ref string, repoID string) (*daemon.Result[daemon.InvocationCheckData], error)
}

// SnapshotLoader composes watch workspace state from canonical daemon reads.
type SnapshotLoader struct {
	client    snapshotClient
	pageLimit int
}

// NewSnapshotLoader creates a loader for watch snapshot composition.
func NewSnapshotLoader(client snapshotClient) *SnapshotLoader {
	return &SnapshotLoader{
		client:    client,
		pageLimit: defaultPageLimit,
	}
}

// Load composes one snapshot of repos/worktrees/invocations/checks.
func (l *SnapshotLoader) Load(ctx context.Context) (Snapshot, error) {
	if l == nil || l.client == nil {
		return Snapshot{}, errors.New(errors.EInternal, "watch snapshot loader is not configured")
	}

	reposResult, err := l.client.ListRepos(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	worktrees, err := l.fetchAllWorktrees(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	invocations, err := l.fetchAllInvocations(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	checks, warnings := l.fetchChecks(ctx, invocations)

	return Snapshot{
		Repos:       reposResult.Data.Repos,
		Worktrees:   worktrees,
		Invocations: invocations,
		Checks:      checks,
		Warnings:    warnings,
		UpdatedAt:   time.Now().UTC(),
	}, nil
}

func (l *SnapshotLoader) fetchAllWorktrees(ctx context.Context) ([]daemon.WorktreeDTO, error) {
	worktrees := make([]daemon.WorktreeDTO, 0, 128)
	cursor := ""

	for {
		result, err := l.client.ListWorktrees(ctx, daemonclient.ListWorktreesOpts{
			State:  "all",
			Limit:  l.pageLimit,
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

func (l *SnapshotLoader) fetchAllInvocations(ctx context.Context) ([]daemon.InvocationDTO, error) {
	invocations := make([]daemon.InvocationDTO, 0, 128)
	cursor := ""

	for {
		result, err := l.client.ListInvocations(ctx, daemonclient.ListInvocationsOpts{
			State:  "all",
			Limit:  l.pageLimit,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}

		invocations = append(invocations, result.Data.Invocations...)
		if result.Data.NextCursor == "" {
			return invocations, nil
		}
		if result.Data.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "invocation pagination cursor did not advance")
		}
		cursor = result.Data.NextCursor
	}
}

func (l *SnapshotLoader) fetchChecks(ctx context.Context, invocations []daemon.InvocationDTO) (map[string]daemon.InvocationCheckData, []string) {
	checks := make(map[string]daemon.InvocationCheckData, len(invocations))
	warnings := make([]string, 0)

	for _, inv := range invocations {
		result, err := l.client.GetInvocationCheck(ctx, inv.InvocationID, inv.RepoID)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("check refresh failed for %s: %v", inv.InvocationID, err))
			continue
		}
		checks[inv.InvocationID] = result.Data
	}

	return checks, warnings
}
