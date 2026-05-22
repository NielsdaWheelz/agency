package watch

import (
	"context"
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
)

type testAPIResponse struct {
	OK         bool   `json:"ok"`
	APIVersion int    `json:"api_version,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Message    string `json:"message,omitempty"`
	Hint       string `json:"hint,omitempty"`
	Data       any    `json:"data,omitempty"`
}

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
	require.NoError(t, json.NewEncoder(w).Encode(testAPIResponse{
		OK:         true,
		APIVersion: daemon.APIVersion,
		Data:       data,
	}))
}

func TestLoadWorkspaceSnapshot_UsesRepoAndWorktreeScope(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos":
			writeDaemonOK(t, w, daemon.ListReposData{
				Repos: []daemon.RepoDTO{{RepoID: "repo-1", RepoKey: "github.com/acme/one"}},
			})
		case "/worktrees":
			assert.Equal(t, "repo-1", r.URL.Query().Get("repo_id"))
			assert.Equal(t, "present", r.URL.Query().Get("state"))
			writeDaemonOK(t, w, daemon.ListWorktreesData{
				Worktrees: []daemon.WorktreeDTO{{WorktreeID: "wt-1", RepoID: "repo-1", WorktreeName: "auth"}},
			})
		case "/invocations":
			assert.Equal(t, "repo-1", r.URL.Query().Get("repo_id"))
			assert.Equal(t, "wt-1", r.URL.Query().Get("worktree_ref"))
			assert.Equal(t, "unresolved", r.URL.Query().Get("state"))
			writeDaemonOK(t, w, daemon.ListInvocationsData{
				Invocations: []daemon.InvocationDTO{{InvocationID: "inv-1", RepoID: "repo-1", WorktreeID: "wt-1"}},
			})
		default:
			http.NotFound(w, r)
		}
	})))

	snapshot, err := loadWorkspaceSnapshot(context.Background(), client, "repo-1", "wt-1", "", "")
	require.NoError(t, err)

	require.Len(t, snapshot.Worktrees, 1)
	require.Len(t, snapshot.Invocations, 1)
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
					{InvocationID: "inv-2", RepoID: "repo-1", SortKey: 70, StartedAt: "2026-02-01T10:00:00Z"},
					{InvocationID: "inv-1", RepoID: "repo-1", SortKey: 40, StartedAt: "2026-02-03T10:00:00Z"},
					{InvocationID: "inv-3", RepoID: "repo-1", SortKey: 70, StartedAt: "2026-02-05T10:00:00Z"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})))

	snapshot, err := loadWorkspaceSnapshot(context.Background(), client, "", "", "", "")
	require.NoError(t, err)

	require.Len(t, snapshot.Invocations, 3)
	assert.Equal(t, []string{"inv-1", "inv-3", "inv-2"}, []string{
		snapshot.Invocations[0].InvocationID,
		snapshot.Invocations[1].InvocationID,
		snapshot.Invocations[2].InvocationID,
	})
}
