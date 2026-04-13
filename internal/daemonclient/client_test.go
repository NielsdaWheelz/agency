package daemonclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startFakeDaemon starts an HTTP server on a Unix socket that responds with
// the provided handler. Uses a short path to avoid macOS 104-byte socket limit.
func startFakeDaemon(t *testing.T, handler http.Handler) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "dc")
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

func TestDaemonClient_ReadAPIErrorPassthrough_PreservesDetails(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := daemon.APIResponse{
			OK:        false,
			ErrorCode: "E_WORKTREE_ID_AMBIGUOUS",
			Message:   "worktree ref 'alpha' is ambiguous",
			Hint:      "specify the full worktree ID to disambiguate",
			Details:   daemon.AmbiguousDetails{Candidates: []string{"wt-001", "wt-002", "wt-003"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	socketPath := startFakeDaemon(t, handler)
	client := NewClient(socketPath)

	_, err := client.GetWorktreeRich(context.Background(), "alpha", "repo-1")
	require.Error(t, err)

	// DaemonReadError must be extractable
	dre, ok := AsDaemonReadError(err)
	require.True(t, ok, "error should be a DaemonReadError")

	// error_code and message preserved exactly
	assert.Equal(t, errors.EWorktreeIDAmbiguous, dre.AgencyErr.Code)
	assert.Equal(t, "worktree ref 'alpha' is ambiguous", dre.AgencyErr.Msg)

	// hint preserved
	assert.Equal(t, "specify the full worktree ID to disambiguate", dre.Hint)

	// candidates recoverable as machine-readable data (not parsed from message)
	candidates := dre.Candidates()
	require.Len(t, candidates, 3)
	assert.Equal(t, []string{"wt-001", "wt-002", "wt-003"}, candidates)

	// raw details available for other structured access patterns
	require.NotEmpty(t, dre.RawDetails)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(dre.RawDetails, &raw))
	assert.Contains(t, raw, "candidates")

	// AgencyError extractable for backward-compat code paths
	code := errors.GetCode(err)
	assert.Equal(t, errors.EWorktreeIDAmbiguous, code)

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EWorktreeIDAmbiguous, ae.Code)
}

func TestDaemonClient_ReadAPIErrorPassthrough_InvocationRich(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := daemon.APIResponse{
			OK:        false,
			ErrorCode: "E_INVOCATION_ID_AMBIGUOUS",
			Message:   "invocation ref 'run' is ambiguous",
			Hint:      "use the full invocation ID",
			Details:   daemon.AmbiguousDetails{Candidates: []string{"inv-a", "inv-b"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	socketPath := startFakeDaemon(t, handler)
	client := NewClient(socketPath)

	_, err := client.GetInvocationRich(context.Background(), "run", "repo-1")
	require.Error(t, err)

	dre, ok := AsDaemonReadError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EInvocationIDAmbiguous, dre.AgencyErr.Code)
	assert.Equal(t, "use the full invocation ID", dre.Hint)

	candidates := dre.Candidates()
	assert.Equal(t, []string{"inv-a", "inv-b"}, candidates)
}

func TestDaemonClient_ExistingGetWorktree_DoesNotReturnDaemonReadError(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := daemon.APIResponse{
			OK:        false,
			ErrorCode: "E_WORKTREE_NOT_FOUND",
			Message:   "worktree not found",
			Hint:      "this hint is dropped by existing method",
			Details:   daemon.AmbiguousDetails{Candidates: []string{"wt-1"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	socketPath := startFakeDaemon(t, handler)
	client := NewClient(socketPath)

	_, err := client.GetWorktree(context.Background(), "alpha", "repo-1")
	require.Error(t, err)

	// Existing method must NOT return DaemonReadError
	_, ok := AsDaemonReadError(err)
	assert.False(t, ok, "existing GetWorktree must not return DaemonReadError")

	// Error code still works
	assert.Equal(t, errors.EWorktreeNotFound, errors.GetCode(err))
}

func TestDaemonClient_ReadAPIErrorPassthrough_NoDetails(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := daemon.APIResponse{
			OK:        false,
			ErrorCode: "E_WORKTREE_NOT_FOUND",
			Message:   "no worktree matches ref",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	socketPath := startFakeDaemon(t, handler)
	client := NewClient(socketPath)

	_, err := client.GetWorktreeRich(context.Background(), "missing", "repo-1")
	require.Error(t, err)

	dre, ok := AsDaemonReadError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EWorktreeNotFound, dre.AgencyErr.Code)
	assert.Empty(t, dre.Hint)
	assert.Nil(t, dre.Candidates())
}

func TestDaemonClient_GetInvocationTimeline_OrderParamSentInURL(t *testing.T) {
	t.Parallel()

	var capturedPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.String()
		resp := daemon.APIResponse{
			OK:   true,
			Data: daemon.InvocationTimelineData{Entries: []daemon.TimelineEntryDTO{}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	socketPath := startFakeDaemon(t, handler)
	client := NewClient(socketPath)

	_, err := client.GetInvocationTimeline(context.Background(), "inv-1", "repo-1", GetInvocationTimelineOpts{
		Limit: 1,
		Order: "desc",
	})
	require.NoError(t, err)

	assert.Contains(t, capturedPath, "order=desc", "Order param must be sent in URL")
	assert.Contains(t, capturedPath, "limit=1")
}

func TestDaemonClient_GetInvocationTimeline_OrderOmittedWhenEmpty(t *testing.T) {
	t.Parallel()

	var capturedPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.String()
		resp := daemon.APIResponse{
			OK:   true,
			Data: daemon.InvocationTimelineData{Entries: []daemon.TimelineEntryDTO{}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	socketPath := startFakeDaemon(t, handler)
	client := NewClient(socketPath)

	_, err := client.GetInvocationTimeline(context.Background(), "inv-1", "repo-1", GetInvocationTimelineOpts{
		Limit: 10,
	})
	require.NoError(t, err)

	assert.NotContains(t, capturedPath, "order=", "Order param must not be sent when empty")
}
