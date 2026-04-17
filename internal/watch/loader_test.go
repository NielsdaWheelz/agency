package watch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

func startFakeDaemon(t *testing.T, handler http.Handler) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "watch")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath := fmt.Sprintf("%s/s.sock", dir)
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	return socketPath
}

func writeDaemonOK(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(daemon.APIResponse{
		OK:         true,
		APIVersion: daemon.APIVersion,
		Data:       data,
	}))
}

func writeDaemonError(t *testing.T, w http.ResponseWriter, code agencyerrors.Code, message string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(daemon.APIResponse{
		OK:         false,
		APIVersion: daemon.APIVersion,
		ErrorCode:  string(code),
		Message:    message,
	}))
}

func TestLoadWorkspaceSnapshot_DrainsPaginationAndCollectsChecks(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos":
			writeDaemonOK(t, w, daemon.ListReposData{
				Repos: []daemon.RepoDTO{{RepoID: "repo-1", RepoKey: "github.com/acme/one"}},
			})
		case r.URL.Path == "/worktrees" && r.URL.Query().Get("cursor") == "":
			writeDaemonOK(t, w, daemon.ListWorktreesData{
				Worktrees: []daemon.WorktreeDTO{
					{WorktreeID: "wt-1", RepoID: "repo-1", Name: "alpha"},
					{WorktreeID: "wt-2", RepoID: "repo-1", Name: "beta"},
				},
				NextCursor: "wt-next-1",
			})
		case r.URL.Path == "/worktrees" && r.URL.Query().Get("cursor") == "wt-next-1":
			writeDaemonOK(t, w, daemon.ListWorktreesData{
				Worktrees: []daemon.WorktreeDTO{
					{WorktreeID: "wt-3", RepoID: "repo-1", Name: "gamma"},
				},
			})
		case r.URL.Path == "/invocations" && r.URL.Query().Get("cursor") == "":
			writeDaemonOK(t, w, daemon.ListInvocationsData{
				Invocations: []daemon.InvocationDTO{
					{InvocationID: "inv-1", RepoID: "repo-1", WorktreeID: "wt-1", SortKey: daemon.SortKeyWorking},
				},
				NextCursor: "inv-next-1",
			})
		case r.URL.Path == "/invocations" && r.URL.Query().Get("cursor") == "inv-next-1":
			writeDaemonOK(t, w, daemon.ListInvocationsData{
				Invocations: []daemon.InvocationDTO{
					{InvocationID: "inv-2", RepoID: "repo-1", WorktreeID: "wt-2", SortKey: daemon.SortKeyBlocked},
				},
			})
		case r.URL.Path == "/invocations/inv-1/check":
			writeDaemonOK(t, w, daemon.InvocationCheckData{InvocationID: "inv-1", Readiness: "ready", Ready: true})
		case r.URL.Path == "/invocations/inv-2/check":
			writeDaemonOK(t, w, daemon.InvocationCheckData{InvocationID: "inv-2", Readiness: "blocked", Ready: false})
		default:
			http.NotFound(w, r)
		}
	})))

	snapshot, err := loadWorkspaceSnapshot(context.Background(), client)
	require.NoError(t, err)

	require.Len(t, snapshot.Repos, 1)
	require.Len(t, snapshot.Worktrees, 3)
	require.Len(t, snapshot.Invocations, 2)
	require.Len(t, snapshot.Checks, 2)
	assert.Equal(t, "ready", snapshot.Checks["inv-1"].Readiness)
	assert.Equal(t, "blocked", snapshot.Checks["inv-2"].Readiness)
	assert.Empty(t, snapshot.Warnings)
}

func TestLoadWorkspaceSnapshot_CursorMustAdvance(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos":
			writeDaemonOK(t, w, daemon.ListReposData{
				Repos: []daemon.RepoDTO{{RepoID: "repo-1"}},
			})
		case r.URL.Path == "/worktrees" && r.URL.Query().Get("cursor") == "":
			writeDaemonOK(t, w, daemon.ListWorktreesData{
				Worktrees:  []daemon.WorktreeDTO{{WorktreeID: "wt-1", RepoID: "repo-1"}},
				NextCursor: "cursor-1",
			})
		case r.URL.Path == "/worktrees" && r.URL.Query().Get("cursor") == "cursor-1":
			writeDaemonOK(t, w, daemon.ListWorktreesData{
				Worktrees:  []daemon.WorktreeDTO{{WorktreeID: "wt-2", RepoID: "repo-1"}},
				NextCursor: "cursor-1",
			})
		case r.URL.Path == "/invocations":
			writeDaemonOK(t, w, daemon.ListInvocationsData{})
		default:
			http.NotFound(w, r)
		}
	})))

	_, err := loadWorkspaceSnapshot(context.Background(), client)
	require.Error(t, err)

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, agencyerrors.EInternal, ae.Code)
	assert.Contains(t, ae.Msg, "worktree pagination cursor did not advance")
}

func TestLoadWorkspaceSnapshot_CheckReadFailuresAreRecoverable(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos":
			writeDaemonOK(t, w, daemon.ListReposData{
				Repos: []daemon.RepoDTO{{RepoID: "repo-1"}},
			})
		case "/worktrees":
			writeDaemonOK(t, w, daemon.ListWorktreesData{
				Worktrees: []daemon.WorktreeDTO{{WorktreeID: "wt-1", RepoID: "repo-1", Name: "alpha"}},
			})
		case "/invocations":
			writeDaemonOK(t, w, daemon.ListInvocationsData{
				Invocations: []daemon.InvocationDTO{
					{InvocationID: "inv-good", RepoID: "repo-1", WorktreeID: "wt-1", SortKey: daemon.SortKeyReady},
					{InvocationID: "inv-bad", RepoID: "repo-1", WorktreeID: "wt-1", SortKey: daemon.SortKeyBlocked},
				},
			})
		case "/invocations/inv-good/check":
			writeDaemonOK(t, w, daemon.InvocationCheckData{InvocationID: "inv-good", Readiness: "ready", Ready: true})
		case "/invocations/inv-bad/check":
			writeDaemonError(t, w, agencyerrors.EInternal, "temporary daemon timeout")
		default:
			http.NotFound(w, r)
		}
	})))

	snapshot, err := loadWorkspaceSnapshot(context.Background(), client)
	require.NoError(t, err)

	assert.Contains(t, snapshot.Checks, "inv-good")
	assert.NotContains(t, snapshot.Checks, "inv-bad")
	require.Len(t, snapshot.Warnings, 1)
	assert.Contains(t, snapshot.Warnings[0], "inv-bad")
	assert.Contains(t, snapshot.Warnings[0], "temporary daemon timeout")
}

func TestLoadWorkspaceSnapshot_SortsBySortKeyThenStartedAt(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos":
			writeDaemonOK(t, w, daemon.ListReposData{
				Repos: []daemon.RepoDTO{{RepoID: "repo-1"}},
			})
		case "/worktrees":
			writeDaemonOK(t, w, daemon.ListWorktreesData{})
		case "/invocations":
			writeDaemonOK(t, w, daemon.ListInvocationsData{
				Invocations: []daemon.InvocationDTO{
					{InvocationID: "inv-2", RepoID: "repo-1", SortKey: daemon.SortKeyReady, StartedAt: "2026-02-01T10:00:00Z"},
					{InvocationID: "inv-1", RepoID: "repo-1", SortKey: daemon.SortKeyBlocked, StartedAt: "2026-02-03T10:00:00Z"},
					{InvocationID: "inv-3", RepoID: "repo-1", SortKey: daemon.SortKeyReady, StartedAt: "2026-02-05T10:00:00Z"},
				},
			})
		case "/invocations/inv-1/check":
			writeDaemonOK(t, w, daemon.InvocationCheckData{InvocationID: "inv-1", Readiness: "blocked", Ready: false})
		case "/invocations/inv-2/check":
			writeDaemonOK(t, w, daemon.InvocationCheckData{InvocationID: "inv-2", Readiness: "ready", Ready: true})
		case "/invocations/inv-3/check":
			writeDaemonOK(t, w, daemon.InvocationCheckData{InvocationID: "inv-3", Readiness: "ready", Ready: true})
		default:
			http.NotFound(w, r)
		}
	})))

	snapshot, err := loadWorkspaceSnapshot(context.Background(), client)
	require.NoError(t, err)

	require.Len(t, snapshot.Invocations, 3)
	assert.Equal(t, []string{"inv-1", "inv-3", "inv-2"}, []string{
		snapshot.Invocations[0].InvocationID,
		snapshot.Invocations[1].InvocationID,
		snapshot.Invocations[2].InvocationID,
	})
}

func TestLoadHistoryTurns_DrainsTimelineAndCheckpoints(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/invocations/inv-1/timeline" && r.URL.Query().Get("cursor") == "":
			writeDaemonOK(t, w, daemon.InvocationTimelineData{
				Entries: []daemon.TimelineEntryDTO{
					{
						EntryID:   "stream:1",
						Kind:      "message",
						Source:    "stream",
						Timestamp: "2026-02-05T11:50:10Z",
						Data: map[string]interface{}{
							"role": "assistant",
							"text": "Done.",
						},
					},
				},
				NextCursor: "timeline-next-1",
			})
		case r.URL.Path == "/invocations/inv-1/timeline" && r.URL.Query().Get("cursor") == "timeline-next-1":
			writeDaemonOK(t, w, daemon.InvocationTimelineData{
				Entries: []daemon.TimelineEntryDTO{
					{
						EntryID:   "inv_event:2:agency.checkpoint_created",
						Kind:      "checkpoint_event",
						Source:    "invocation_event",
						Timestamp: "2026-02-05T11:50:11Z",
						Data: map[string]interface{}{
							"event_kind":    "agency.checkpoint_created",
							"checkpoint_id": float64(1),
						},
					},
				},
			})
		case r.URL.Path == "/invocations/inv-1/checkpoints":
			writeDaemonOK(t, w, daemon.ListCheckpointsData{
				Checkpoints: []daemon.CheckpointDTO{
					{ID: 1, Description: "After Edit", Diffstat: "+12 -3 in 2 files"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})))

	turns, err := loadHistoryTurns(context.Background(), client, "inv-1", "repo-1")
	require.NoError(t, err)

	require.Len(t, turns, 1)
	assert.Equal(t, daemon.TurnAssistant, turns[0].Kind)
	assert.Equal(t, 1, turns[0].CheckpointID)
	assert.True(t, turns[0].Restorable)
	assert.Contains(t, turns[0].Summary, "After Edit")
	assert.Contains(t, turns[0].Summary, "+12 -3 in 2 files")
}

func TestLoadInvocationLogs_DrainsOffsetPages(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/invocations/inv-1/logs" && r.URL.Query().Get("offset") == "0":
			writeDaemonOK(t, w, daemon.InvocationLogsOffsetData{
				Kind:       "raw",
				DataB64:    base64.StdEncoding.EncodeToString([]byte("hello ")),
				NextOffset: 6,
				TotalBytes: 11,
			})
		case r.URL.Path == "/invocations/inv-1/logs" && r.URL.Query().Get("offset") == "6":
			writeDaemonOK(t, w, daemon.InvocationLogsOffsetData{
				Kind:       "raw",
				DataB64:    base64.StdEncoding.EncodeToString([]byte("world")),
				NextOffset: 11,
				TotalBytes: 11,
			})
		default:
			http.NotFound(w, r)
		}
	})))

	content, err := loadInvocationLogs(context.Background(), client, "inv-1", "repo-1")
	require.NoError(t, err)
	assert.Equal(t, "hello world", content)
}
