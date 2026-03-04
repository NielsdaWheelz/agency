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
	Reviews     map[string]daemon.InvocationReviewData
	Warnings    []string
	UpdatedAt   time.Time
}

type snapshotClient interface {
	ListRepos(ctx context.Context) (*daemonclient.ListReposResult, error)
	ListWorktrees(ctx context.Context, opts daemonclient.ListWorktreesOpts) (*daemonclient.ListWorktreesResult, error)
	ListInvocations(ctx context.Context, opts daemonclient.ListInvocationsOpts) (*daemonclient.ListInvocationsResult, error)
	GetInvocationReview(ctx context.Context, ref string, repoID string) (*daemonclient.GetInvocationReviewResult, error)
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

// Load composes one snapshot of repos/worktrees/invocations/reviews.
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

	reviews, warnings := l.fetchReviews(ctx, invocations)

	return Snapshot{
		Repos:       reposResult.Repos,
		Worktrees:   worktrees,
		Invocations: invocations,
		Reviews:     reviews,
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

		worktrees = append(worktrees, result.Worktrees...)
		if result.NextCursor == "" {
			return worktrees, nil
		}
		if result.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "worktree pagination cursor did not advance")
		}
		cursor = result.NextCursor
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

		invocations = append(invocations, result.Invocations...)
		if result.NextCursor == "" {
			return invocations, nil
		}
		if result.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "invocation pagination cursor did not advance")
		}
		cursor = result.NextCursor
	}
}

func (l *SnapshotLoader) fetchReviews(ctx context.Context, invocations []daemon.InvocationDTO) (map[string]daemon.InvocationReviewData, []string) {
	reviews := make(map[string]daemon.InvocationReviewData, len(invocations))
	warnings := make([]string, 0)

	for _, inv := range invocations {
		result, err := l.client.GetInvocationReview(ctx, inv.InvocationID, inv.RepoID)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("review refresh failed for %s: %v", inv.InvocationID, err))
			continue
		}
		reviews[inv.InvocationID] = result.Review
	}

	return reviews, warnings
}
