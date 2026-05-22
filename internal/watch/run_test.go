package watch

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestRun_NilClient_ReturnsEInternal(t *testing.T) {
	t.Parallel()

	_, err := Run(context.Background(), nil, RunOptions{})
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
}

func TestRun_HistoryInitialPageRequiresInvocationAndRepo(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, nil))

	_, err := Run(context.Background(), client, RunOptions{InitialPage: InitialPageHistory})
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
	assert.Contains(t, err.Error(), "history page requires an invocation and repo")
}

func TestRun_UnknownInitialPageReturnsEInternal(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, nil))

	_, err := Run(context.Background(), client, RunOptions{InitialPage: "bogus"})
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
	assert.Contains(t, err.Error(), "unknown watch initial page")
}

func TestRun_HistoryInitialPagePropagatesWorktreeReadError(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/invocations/inv-1":
			assert.Equal(t, "repo-1", r.URL.Query().Get("repo_id"))
			writeDaemonOK(t, w, daemon.InvocationDTO{
				InvocationID: "inv-1",
				RepoID:       "repo-1",
				WorktreeID:   "wt-1",
			})
		case "/worktrees/wt-1":
			assert.Equal(t, "repo-1", r.URL.Query().Get("repo_id"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			require.NoError(t, json.NewEncoder(w).Encode(testAPIResponse{
				OK:         false,
				APIVersion: daemon.APIVersion,
				ErrorCode:  string(errors.EWorktreeNotFound),
				Message:    "worktree not found: wt-1",
				Hint:       "list worktrees",
			}))
		case "/invocations/inv-1/timeline", "/invocations/inv-1/checkpoints":
			t.Fatalf("history setup must stop after worktree read failure; unexpected request: %s", r.URL.Path)
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	})))

	_, err := Run(context.Background(), client, RunOptions{
		InitialPage:  InitialPageHistory,
		InvocationID: "inv-1",
		RepoID:       "repo-1",
	})
	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeNotFound, errors.GetCode(err))

	var readErr *daemonclient.DaemonReadError
	require.True(t, stderrors.As(err, &readErr))
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "list worktrees", ae.Details["hint"])
}

func TestModel_RequestedAttachRequiresBothIDs(t *testing.T) {
	t.Parallel()

	invocationID, repoID, ok := (model{
		attachInvocationID:  "inv-1",
		attachRequestedRepo: "repo-1",
	}).requestedAttach()
	require.True(t, ok)
	assert.Equal(t, "inv-1", invocationID)
	assert.Equal(t, "repo-1", repoID)

	_, _, ok = (model{attachInvocationID: "inv-1"}).requestedAttach()
	assert.False(t, ok)
}
