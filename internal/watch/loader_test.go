package watch

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

type fakeSnapshotClient struct {
	reposResult         *daemon.Result[daemon.ListReposData]
	reposErr            error
	worktreePages       map[string]*daemon.Result[daemon.ListWorktreesData]
	worktreeErrByCursor map[string]error
	invocationPages     map[string]*daemon.Result[daemon.ListInvocationsData]
	invocationErrByPage map[string]error
	reviewsByInvocation map[string]*daemon.Result[daemon.InvocationReviewData]
	reviewErrByRef      map[string]error
}

func (f *fakeSnapshotClient) ListRepos(_ context.Context) (*daemon.Result[daemon.ListReposData], error) {
	if f.reposErr != nil {
		return nil, f.reposErr
	}
	if f.reposResult == nil {
		return &daemon.Result[daemon.ListReposData]{}, nil
	}
	return f.reposResult, nil
}

func (f *fakeSnapshotClient) ListWorktrees(_ context.Context, opts daemonclient.ListWorktreesOpts) (*daemon.Result[daemon.ListWorktreesData], error) {
	if opts.State != "all" {
		return nil, fmt.Errorf("unexpected state %q", opts.State)
	}
	if err := f.worktreeErrByCursor[opts.Cursor]; err != nil {
		return nil, err
	}
	if result := f.worktreePages[opts.Cursor]; result != nil {
		return result, nil
	}
	return &daemon.Result[daemon.ListWorktreesData]{}, nil
}

func (f *fakeSnapshotClient) ListInvocations(_ context.Context, opts daemonclient.ListInvocationsOpts) (*daemon.Result[daemon.ListInvocationsData], error) {
	if opts.State != "all" {
		return nil, fmt.Errorf("unexpected state %q", opts.State)
	}
	if err := f.invocationErrByPage[opts.Cursor]; err != nil {
		return nil, err
	}
	if result := f.invocationPages[opts.Cursor]; result != nil {
		return result, nil
	}
	return &daemon.Result[daemon.ListInvocationsData]{}, nil
}

func (f *fakeSnapshotClient) GetInvocationReview(_ context.Context, ref string, _ string) (*daemon.Result[daemon.InvocationReviewData], error) {
	if err := f.reviewErrByRef[ref]; err != nil {
		return nil, err
	}
	if result := f.reviewsByInvocation[ref]; result != nil {
		return result, nil
	}
	return nil, fmt.Errorf("missing review fixture for %s", ref)
}

func TestSnapshotLoader_Load_DrainsPaginationAndCollectsReviews(t *testing.T) {
	t.Parallel()

	client := &fakeSnapshotClient{
		reposResult: &daemon.Result[daemon.ListReposData]{
			Data: daemon.ListReposData{
				Repos: []daemon.RepoDTO{{RepoID: "repo-1", RepoKey: "github.com/acme/one"}},
			},
		},
		worktreePages: map[string]*daemon.Result[daemon.ListWorktreesData]{
			"": {
				Data: daemon.ListWorktreesData{
					Worktrees: []daemon.WorktreeDTO{
						{WorktreeID: "wt-1", RepoID: "repo-1", Name: "alpha"},
						{WorktreeID: "wt-2", RepoID: "repo-1", Name: "beta"},
					},
					NextCursor: "wt-next-1",
				},
			},
			"wt-next-1": {
				Data: daemon.ListWorktreesData{
					Worktrees: []daemon.WorktreeDTO{
						{WorktreeID: "wt-3", RepoID: "repo-1", Name: "gamma"},
					},
				},
			},
		},
		invocationPages: map[string]*daemon.Result[daemon.ListInvocationsData]{
			"": {
				Data: daemon.ListInvocationsData{
					Invocations: []daemon.InvocationDTO{
						{InvocationID: "inv-1", RepoID: "repo-1", WorktreeID: "wt-1"},
					},
					NextCursor: "inv-next-1",
				},
			},
			"inv-next-1": {
				Data: daemon.ListInvocationsData{
					Invocations: []daemon.InvocationDTO{
						{InvocationID: "inv-2", RepoID: "repo-1", WorktreeID: "wt-2"},
					},
				},
			},
		},
		reviewsByInvocation: map[string]*daemon.Result[daemon.InvocationReviewData]{
			"inv-1": {Data: daemon.InvocationReviewData{InvocationID: "inv-1", Readiness: "ready", Ready: true}},
			"inv-2": {Data: daemon.InvocationReviewData{InvocationID: "inv-2", Readiness: "blocked", Ready: false}},
		},
	}

	loader := NewSnapshotLoader(client)
	snapshot, err := loader.Load(context.Background())
	require.NoError(t, err)

	require.Len(t, snapshot.Repos, 1)
	require.Len(t, snapshot.Worktrees, 3)
	require.Len(t, snapshot.Invocations, 2)
	require.Len(t, snapshot.Reviews, 2)
	assert.Equal(t, "ready", snapshot.Reviews["inv-1"].Readiness)
	assert.Equal(t, "blocked", snapshot.Reviews["inv-2"].Readiness)
	assert.Empty(t, snapshot.Warnings)
}

func TestSnapshotLoader_Load_CursorMustAdvance(t *testing.T) {
	t.Parallel()

	client := &fakeSnapshotClient{
		reposResult: &daemon.Result[daemon.ListReposData]{
			Data: daemon.ListReposData{
				Repos: []daemon.RepoDTO{{RepoID: "repo-1"}},
			},
		},
		worktreePages: map[string]*daemon.Result[daemon.ListWorktreesData]{
			"": {
				Data: daemon.ListWorktreesData{
					Worktrees:  []daemon.WorktreeDTO{{WorktreeID: "wt-1", RepoID: "repo-1"}},
					NextCursor: "cursor-1",
				},
			},
			"cursor-1": {
				Data: daemon.ListWorktreesData{
					Worktrees:  []daemon.WorktreeDTO{{WorktreeID: "wt-2", RepoID: "repo-1"}},
					NextCursor: "cursor-1",
				},
			},
		},
		invocationPages: map[string]*daemon.Result[daemon.ListInvocationsData]{
			"": {},
		},
		reviewsByInvocation: map[string]*daemon.Result[daemon.InvocationReviewData]{},
	}

	loader := NewSnapshotLoader(client)
	_, err := loader.Load(context.Background())
	require.Error(t, err)

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, agencyerrors.EInternal, ae.Code)
	assert.Contains(t, ae.Msg, "worktree pagination cursor did not advance")
}

func TestSnapshotLoader_Load_ReviewReadFailuresAreRecoverable(t *testing.T) {
	t.Parallel()

	client := &fakeSnapshotClient{
		reposResult: &daemon.Result[daemon.ListReposData]{
			Data: daemon.ListReposData{
				Repos: []daemon.RepoDTO{{RepoID: "repo-1"}},
			},
		},
		worktreePages: map[string]*daemon.Result[daemon.ListWorktreesData]{
			"": {
				Data: daemon.ListWorktreesData{
					Worktrees: []daemon.WorktreeDTO{
						{WorktreeID: "wt-1", RepoID: "repo-1", Name: "alpha"},
					},
				},
			},
		},
		invocationPages: map[string]*daemon.Result[daemon.ListInvocationsData]{
			"": {
				Data: daemon.ListInvocationsData{
					Invocations: []daemon.InvocationDTO{
						{InvocationID: "inv-good", RepoID: "repo-1", WorktreeID: "wt-1"},
						{InvocationID: "inv-bad", RepoID: "repo-1", WorktreeID: "wt-1"},
					},
				},
			},
		},
		reviewsByInvocation: map[string]*daemon.Result[daemon.InvocationReviewData]{
			"inv-good": {Data: daemon.InvocationReviewData{InvocationID: "inv-good", Readiness: "ready", Ready: true}},
		},
		reviewErrByRef: map[string]error{
			"inv-bad": fmt.Errorf("temporary daemon timeout"),
		},
	}

	loader := NewSnapshotLoader(client)
	snapshot, err := loader.Load(context.Background())
	require.NoError(t, err)

	assert.Contains(t, snapshot.Reviews, "inv-good")
	assert.NotContains(t, snapshot.Reviews, "inv-bad")
	require.Len(t, snapshot.Warnings, 1)
	assert.Contains(t, snapshot.Warnings[0], "inv-bad")
	assert.Contains(t, snapshot.Warnings[0], "temporary daemon timeout")
}
