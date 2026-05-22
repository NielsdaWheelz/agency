package daemonclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func writeDrainDaemonOK(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(testAPIResponse{OK: true, Data: data}))
}

func TestDaemonClient_DrainWorktrees_DrainsCursorPages(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/worktrees", r.URL.Path)
		require.Equal(t, "repo-1", r.URL.Query().Get("repo_id"))
		require.Equal(t, "all", r.URL.Query().Get("state"))
		require.Equal(t, "500", r.URL.Query().Get("limit"))

		switch r.URL.Query().Get("cursor") {
		case "":
			writeDrainDaemonOK(t, w, daemon.ListWorktreesData{
				Worktrees:  []daemon.WorktreeDTO{{WorktreeID: "wt-1"}},
				NextCursor: "next-page",
			})
		case "next-page":
			writeDrainDaemonOK(t, w, daemon.ListWorktreesData{
				Worktrees: []daemon.WorktreeDTO{{WorktreeID: "wt-2"}},
			})
		default:
			http.NotFound(w, r)
		}
	})))

	worktrees, err := client.DrainWorktrees(context.Background(), daemon.ListWorktreesParams{
		RepoID: "repo-1",
		State:  "all",
	})
	require.NoError(t, err)
	require.Len(t, worktrees, 2)
	assert.Equal(t, "wt-1", worktrees[0].WorktreeID)
	assert.Equal(t, "wt-2", worktrees[1].WorktreeID)
}

func TestDaemonClient_DrainWorktrees_CursorMustAdvance(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeDrainDaemonOK(t, w, daemon.ListWorktreesData{NextCursor: r.URL.Query().Get("cursor")})
	})))

	_, err := client.DrainWorktrees(context.Background(), daemon.ListWorktreesParams{Cursor: "stuck"})
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
	assert.Contains(t, err.Error(), "worktree pagination cursor did not advance")
}

func TestDaemonClient_DrainWorktrees_CursorCycleMustAdvance(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			writeDrainDaemonOK(t, w, daemon.ListWorktreesData{NextCursor: "a"})
		case "a":
			writeDrainDaemonOK(t, w, daemon.ListWorktreesData{NextCursor: "b"})
		case "b":
			writeDrainDaemonOK(t, w, daemon.ListWorktreesData{NextCursor: "a"})
		default:
			http.NotFound(w, r)
		}
	})))

	_, err := client.DrainWorktrees(context.Background(), daemon.ListWorktreesParams{})
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
	assert.Contains(t, err.Error(), "worktree pagination cursor did not advance")
}

func TestDaemonClient_DrainInvocations_DrainsCursorPages(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/invocations", r.URL.Path)
		require.Equal(t, "repo-1", r.URL.Query().Get("repo_id"))
		require.Equal(t, "wt-1", r.URL.Query().Get("worktree_ref"))
		require.Equal(t, "all", r.URL.Query().Get("state"))
		require.Equal(t, "headless", r.URL.Query().Get("mode"))
		require.Equal(t, "500", r.URL.Query().Get("limit"))

		switch r.URL.Query().Get("cursor") {
		case "":
			writeDrainDaemonOK(t, w, daemon.ListInvocationsData{
				Invocations: []daemon.InvocationDTO{{InvocationID: "inv-1"}},
				NextCursor:  "next-page",
			})
		case "next-page":
			writeDrainDaemonOK(t, w, daemon.ListInvocationsData{
				Invocations: []daemon.InvocationDTO{{InvocationID: "inv-2"}},
			})
		default:
			http.NotFound(w, r)
		}
	})))

	invocations, err := client.DrainInvocations(context.Background(), daemon.ListInvocationsParams{
		RepoID:      "repo-1",
		WorktreeRef: "wt-1",
		State:       "all",
		Mode:        "headless",
	})
	require.NoError(t, err)
	require.Len(t, invocations, 2)
	assert.Equal(t, "inv-1", invocations[0].InvocationID)
	assert.Equal(t, "inv-2", invocations[1].InvocationID)
}

func TestDaemonClient_DrainInvocations_CursorMustAdvance(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeDrainDaemonOK(t, w, daemon.ListInvocationsData{NextCursor: r.URL.Query().Get("cursor")})
	})))

	_, err := client.DrainInvocations(context.Background(), daemon.ListInvocationsParams{Cursor: "stuck"})
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
	assert.Contains(t, err.Error(), "invocation pagination cursor did not advance")
}

func TestDaemonClient_DrainWorktrees_InvalidLimitDoesNotRequestDaemon(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	})))

	_, err := client.DrainWorktrees(context.Background(), daemon.ListWorktreesParams{Limit: 501})
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
	assert.Equal(t, int32(0), requests.Load())
}

func TestDaemonClient_DrainInvocations_InvalidLimitDoesNotRequestDaemon(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	})))

	_, err := client.DrainInvocations(context.Background(), daemon.ListInvocationsParams{Limit: -1})
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
	assert.Equal(t, int32(0), requests.Load())
}

func TestDaemonClient_DrainInvocationTimeline_DrainsCursorPages(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/invocations/inv-1/timeline", r.URL.Path)
		require.Equal(t, "repo-1", r.URL.Query().Get("repo_id"))
		require.Equal(t, "500", r.URL.Query().Get("limit"))

		switch r.URL.Query().Get("cursor") {
		case "":
			writeDrainDaemonOK(t, w, daemon.InvocationTimelineData{
				Entries:    []daemon.TimelineEntryDTO{{EntryID: "stream:1"}},
				NextCursor: "next-page",
			})
		case "next-page":
			writeDrainDaemonOK(t, w, daemon.InvocationTimelineData{
				Entries: []daemon.TimelineEntryDTO{{EntryID: "stream:2"}},
			})
		default:
			http.NotFound(w, r)
		}
	})))

	entries, err := client.DrainInvocationTimeline(context.Background(), "inv-1", "repo-1", daemon.GetTimelineParams{})
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "stream:1", entries[0].EntryID)
	assert.Equal(t, "stream:2", entries[1].EntryID)
}

func TestDaemonClient_DrainInvocationTimeline_CursorMustAdvance(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeDrainDaemonOK(t, w, daemon.InvocationTimelineData{NextCursor: r.URL.Query().Get("cursor")})
	})))

	_, err := client.DrainInvocationTimeline(context.Background(), "inv-1", "repo-1", daemon.GetTimelineParams{Cursor: "stuck"})
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
	assert.Contains(t, err.Error(), "timeline pagination cursor did not advance")
}

func TestDaemonClient_DrainInvocationTimeline_RejectsDescendingOrder(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	})))

	_, err := client.DrainInvocationTimeline(context.Background(), "inv-1", "repo-1", daemon.GetTimelineParams{Order: "desc"})
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
	assert.Equal(t, int32(0), requests.Load())
}

func TestDaemonClient_DrainInvocationCheckpoints_DrainsCursorPages(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/invocations/inv-1/checkpoints", r.URL.Path)
		require.Equal(t, "repo-1", r.URL.Query().Get("repo_id"))
		require.Equal(t, "500", r.URL.Query().Get("limit"))

		switch r.URL.Query().Get("cursor") {
		case "":
			writeDrainDaemonOK(t, w, daemon.ListCheckpointsData{
				Checkpoints: []daemon.CheckpointDTO{{ID: 2}},
				NextCursor:  "next-page",
			})
		case "next-page":
			writeDrainDaemonOK(t, w, daemon.ListCheckpointsData{
				Checkpoints: []daemon.CheckpointDTO{{ID: 1}},
			})
		default:
			http.NotFound(w, r)
		}
	})))

	checkpoints, err := client.DrainInvocationCheckpoints(context.Background(), "inv-1", "repo-1", daemon.ListCheckpointsParams{})
	require.NoError(t, err)
	require.Len(t, checkpoints, 2)
	assert.Equal(t, 2, checkpoints[0].ID)
	assert.Equal(t, 1, checkpoints[1].ID)
}

func TestDaemonClient_DrainInvocationCheckpoints_CursorMustAdvance(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeDrainDaemonOK(t, w, daemon.ListCheckpointsData{NextCursor: r.URL.Query().Get("cursor")})
	})))

	_, err := client.DrainInvocationCheckpoints(context.Background(), "inv-1", "repo-1", daemon.ListCheckpointsParams{Cursor: "stuck"})
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
	assert.Contains(t, err.Error(), "checkpoint pagination cursor did not advance")
}

func TestDaemonClient_DrainInvocationLogs_DrainsOffsetPages(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/invocations/inv-1/logs", r.URL.Path)
		require.Equal(t, "repo-1", r.URL.Query().Get("repo_id"))
		require.Equal(t, daemon.InvocationLogKindRaw, r.URL.Query().Get("kind"))
		require.Equal(t, "65536", r.URL.Query().Get("limit"))

		switch r.URL.Query().Get("offset") {
		case "0":
			writeDrainDaemonOK(t, w, daemon.InvocationLogsOffsetData{
				Kind:       daemon.InvocationLogKindRaw,
				DataB64:    base64.StdEncoding.EncodeToString([]byte("hello ")),
				NextOffset: 6,
				TotalBytes: 11,
			})
		case "6":
			writeDrainDaemonOK(t, w, daemon.InvocationLogsOffsetData{
				Kind:       daemon.InvocationLogKindRaw,
				DataB64:    base64.StdEncoding.EncodeToString([]byte("world")),
				NextOffset: 11,
				TotalBytes: 11,
			})
		default:
			http.NotFound(w, r)
		}
	})))

	var out bytes.Buffer
	nextOffset, err := client.DrainInvocationLogs(context.Background(), "inv-1", "repo-1", daemon.GetLogsParams{Kind: daemon.InvocationLogKindRaw}, &out)
	require.NoError(t, err)
	assert.Equal(t, "hello world", out.String())
	assert.Equal(t, int64(11), nextOffset)
}

func TestDaemonClient_DrainInvocationLogs_InvalidBase64ReturnsInternal(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeDrainDaemonOK(t, w, daemon.InvocationLogsOffsetData{
			DataB64:    "not-base64",
			NextOffset: 10,
			TotalBytes: 10,
		})
	})))

	var out bytes.Buffer
	_, err := client.DrainInvocationLogs(context.Background(), "inv-1", "repo-1", daemon.GetLogsParams{}, &out)
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
	assert.Contains(t, err.Error(), "failed to decode log data")
	assert.Empty(t, out.String())
}

func TestDaemonClient_DrainInvocationLogs_OffsetMustAdvanceBeforeEOF(t *testing.T) {
	t.Parallel()

	client := NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeDrainDaemonOK(t, w, daemon.InvocationLogsOffsetData{
			DataB64:    base64.StdEncoding.EncodeToString([]byte("partial")),
			NextOffset: 0,
			TotalBytes: 100,
		})
	})))

	var out bytes.Buffer
	_, err := client.DrainInvocationLogs(context.Background(), "inv-1", "repo-1", daemon.GetLogsParams{}, &out)
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
	assert.Contains(t, err.Error(), "log pagination offset did not advance")
	assert.Empty(t, out.String())
}
