package commands

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func successRepo(repoID string) func(context.Context) (*RepoContextResult, error) {
	return func(context.Context) (*RepoContextResult, error) {
		return &RepoContextResult{RepoID: repoID}, nil
	}
}

func worktreeResult(repoID, id, path string) *NavigationResult {
	return &NavigationResult{
		TargetKind:     TargetWorktree,
		ResolvedRepoID: repoID,
		ResolvedID:     id,
		ResolvedPath:   path,
	}
}

func invocationResult(repoID, id, path string) *NavigationResult {
	return &NavigationResult{
		TargetKind:     TargetInvocation,
		ResolvedRepoID: repoID,
		ResolvedID:     id,
		ResolvedPath:   path,
	}
}

func ambiguousDaemonErr(code errors.Code, msg, hint string, candidates []string) *daemonclient.DaemonReadError {
	rawDetails, _ := json.Marshal(map[string]interface{}{"candidates": candidates})
	return &daemonclient.DaemonReadError{
		AgencyErr:  &errors.AgencyError{Code: code, Msg: msg},
		Hint:       hint,
		RawDetails: rawDetails,
	}
}

func TestNavigationKernel_ResolvesWorktree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var order []string
	expected := worktreeResult("repo-1", "wt-1", "/abs/path")

	deps := NavigationDeps{
		ResolveRepo: func(ctx context.Context) (*RepoContextResult, error) {
			order = append(order, "resolve_repo")
			return &RepoContextResult{RepoID: "repo-1"}, nil
		},
		GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			order = append(order, "get_worktree")
			assert.Equal(t, "alpha", ref)
			assert.Equal(t, "repo-1", repoID)
			return expected, nil
		},
	}

	result, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetWorktree,
			Ref:        "alpha",
		},
	}, deps)
	require.NoError(t, err)
	assert.Equal(t, []string{"resolve_repo", "get_worktree"}, order)
	assert.Equal(t, "repo-1", result.ResolvedRepoID)
	assert.Equal(t, "wt-1", result.ResolvedID)
	assert.Equal(t, "/abs/path", result.ResolvedPath)
}

func TestNavigationKernel_RequiresTTY(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repoCalled := false

	_, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetInvocation,
			Ref:        "inv-1",
		},
		RequiresTTY: true,
	}, NavigationDeps{
		ResolveRepo: func(ctx context.Context) (*RepoContextResult, error) {
			repoCalled = true
			return &RepoContextResult{RepoID: "repo-1"}, nil
		},
		GetInvocation: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			return nil, nil
		},
		IsInteractive: func() bool { return false },
	})
	require.Error(t, err)
	assert.Equal(t, errors.ENotInteractive, errors.GetCode(err))
	assert.False(t, repoCalled)
}

func TestNavigationKernel_ResolvesInvocation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	expected := invocationResult("repo-1", "inv-1", "/abs/sandbox")

	result, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetInvocation,
			Ref:        "inv-ref",
		},
	}, NavigationDeps{
		ResolveRepo: successRepo("repo-1"),
		GetInvocation: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			assert.Equal(t, "inv-ref", ref)
			assert.Equal(t, "repo-1", repoID)
			return expected, nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "repo-1", result.ResolvedRepoID)
	assert.Equal(t, "inv-1", result.ResolvedID)
	assert.Equal(t, "/abs/sandbox", result.ResolvedPath)
}

func TestNavigationKernel_ResolveWorktree_AmbiguousPreservesCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	_, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetWorktree,
			Ref:        "a",
		},
	}, NavigationDeps{
		ResolveRepo: successRepo("repo-1"),
		GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			return nil, ambiguousDaemonErr(
				errors.EWorktreeIDAmbiguous,
				"worktree ref 'a' is ambiguous",
				"use full worktree ID",
				[]string{"wt-alpha", "wt-apex"},
			)
		},
	})
	require.Error(t, err)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	require.NotNil(t, ae.Details)
	assert.Equal(t, "worktree", ae.Details["target_kind"])
	assert.Equal(t, "2", ae.Details["candidate_count"])

	var candidates []string
	require.NoError(t, json.Unmarshal([]byte(ae.Details["candidates"]), &candidates))
	assert.Equal(t, []string{"wt-alpha", "wt-apex"}, candidates)
	assert.Equal(t, "use full worktree ID", ae.Details["hint"])
}

func TestNavigationKernel_ResolveInvocation_AmbiguousPreservesCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	_, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetInvocation,
			Ref:        "run",
		},
	}, NavigationDeps{
		ResolveRepo: successRepo("repo-1"),
		GetInvocation: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			return nil, ambiguousDaemonErr(
				errors.EInvocationIDAmbiguous,
				"invocation ref 'run' is ambiguous",
				"use full invocation ID",
				[]string{"inv-runA", "inv-runB", "inv-runC"},
			)
		},
	})
	require.Error(t, err)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	require.NotNil(t, ae.Details)
	assert.Equal(t, "invocation", ae.Details["target_kind"])
	assert.Equal(t, "3", ae.Details["candidate_count"])

	var candidates []string
	require.NoError(t, json.Unmarshal([]byte(ae.Details["candidates"]), &candidates))
	assert.Equal(t, []string{"inv-runA", "inv-runB", "inv-runC"}, candidates)
	assert.Equal(t, "use full invocation ID", ae.Details["hint"])
}
