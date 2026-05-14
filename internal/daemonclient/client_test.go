package daemonclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync/atomic"
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

	_, err := client.GetWorktree(context.Background(), "alpha", "repo-1")
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

	// AgencyError extractable from the canonical read method.
	code := errors.GetCode(err)
	assert.Equal(t, errors.EWorktreeIDAmbiguous, code)

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EWorktreeIDAmbiguous, ae.Code)
}

func TestDaemonClient_ReadAPIErrorPassthrough_Invocation(t *testing.T) {
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

	_, err := client.GetInvocation(context.Background(), "run", "repo-1")
	require.Error(t, err)

	dre, ok := AsDaemonReadError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EInvocationIDAmbiguous, dre.AgencyErr.Code)
	assert.Equal(t, "use the full invocation ID", dre.Hint)

	candidates := dre.Candidates()
	assert.Equal(t, []string{"inv-a", "inv-b"}, candidates)
}

func TestDaemonClient_GetWorktree_ReturnsDaemonReadError(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := daemon.APIResponse{
			OK:        false,
			ErrorCode: "E_WORKTREE_NOT_FOUND",
			Message:   "worktree not found",
			Hint:      "canonical read method should preserve this hint",
			Details:   daemon.AmbiguousDetails{Candidates: []string{"wt-1"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	socketPath := startFakeDaemon(t, handler)
	client := NewClient(socketPath)

	_, err := client.GetWorktree(context.Background(), "alpha", "repo-1")
	require.Error(t, err)

	dre, ok := AsDaemonReadError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EWorktreeNotFound, dre.AgencyErr.Code)
	assert.Equal(t, "canonical read method should preserve this hint", dre.Hint)
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

	_, err := client.GetWorktree(context.Background(), "missing", "repo-1")
	require.Error(t, err)

	dre, ok := AsDaemonReadError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EWorktreeNotFound, dre.AgencyErr.Code)
	assert.Empty(t, dre.Hint)
	assert.Nil(t, dre.Candidates())
}

func TestDaemonClient_ReadMethodsPreserveRichErrors(t *testing.T) {
	t.Parallel()

	expectedDetails := daemon.InvalidQueryArgumentDetails{
		Param:         "state",
		Value:         "bogus",
		AllowedValues: []string{"present", "archived", "all"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := daemon.APIResponse{
			OK:        false,
			ErrorCode: string(errors.EInvalidArgument),
			Message:   "invalid argument",
			Hint:      "preserve the structured read error",
			Details:   expectedDetails,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	socketPath := startFakeDaemon(t, handler)
	client := NewClient(socketPath)

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "ListWorktrees",
			call: func() error {
				_, err := client.ListWorktrees(context.Background(), ListWorktreesOpts{State: "bogus"})
				return err
			},
		},
		{
			name: "ListInvocations",
			call: func() error {
				_, err := client.ListInvocations(context.Background(), ListInvocationsOpts{State: "bogus"})
				return err
			},
		},
		{
			name: "GetInvocationDiff",
			call: func() error {
				_, err := client.GetInvocationDiff(context.Background(), "inv-1", "repo-1", GetInvocationDiffOpts{})
				return err
			},
		},
		{
			name: "GetInvocationCheck",
			call: func() error {
				_, err := client.GetInvocationCheck(context.Background(), "inv-1", "repo-1")
				return err
			},
		},
		{
			name: "GetInvocationLogsOffset",
			call: func() error {
				_, err := client.GetInvocationLogsOffset(context.Background(), "inv-1", "repo-1", GetInvocationLogsOffsetOpts{})
				return err
			},
		},
		{
			name: "GetInvocationTimeline",
			call: func() error {
				_, err := client.GetInvocationTimeline(context.Background(), "inv-1", "repo-1", GetInvocationTimelineOpts{})
				return err
			},
		},
		{
			name: "ListCheckpoints",
			call: func() error {
				_, err := client.ListCheckpoints(context.Background(), "inv-1", "repo-1", ListCheckpointsOpts{})
				return err
			},
		},
		{
			name: "ListRepos",
			call: func() error {
				_, err := client.ListRepos(context.Background())
				return err
			},
		},
		{
			name: "GetRepo",
			call: func() error {
				_, err := client.GetRepo(context.Background(), "repo-1")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)

			dre, ok := AsDaemonReadError(err)
			require.True(t, ok, "error should preserve the daemon read envelope")
			assert.Equal(t, errors.EInvalidArgument, dre.AgencyErr.Code)
			assert.Equal(t, "invalid argument", dre.AgencyErr.Msg)
			assert.Equal(t, "preserve the structured read error", dre.Hint)

			var details daemon.InvalidQueryArgumentDetails
			require.NoError(t, json.Unmarshal(dre.RawDetails, &details))
			assert.Equal(t, expectedDetails, details)
		})
	}
}

func TestDaemonClient_ControlPlaneStartPreservesClientRequestID(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(daemon.HealthResponse{OK: true, APIVersion: daemon.APIVersion})
			return
		}
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		seen[r.URL.Path] = body["client_request_id"].(string)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/invocations/start_headless":
			_ = json.NewEncoder(w).Encode(daemon.ControlPlaneStartResponse{OK: true, APIVersion: daemon.APIVersion, ClientRequestID: seen[r.URL.Path]})
		case "/invocations/start_headed":
			_ = json.NewEncoder(w).Encode(daemon.ControlPlaneStartHeadedResponse{OK: true, APIVersion: daemon.APIVersion, ClientRequestID: seen[r.URL.Path]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	client := NewClient(startFakeDaemon(t, handler))
	_, err := client.ControlPlaneStartHeadless(context.Background(), ControlPlaneStartOpts{ClientRequestID: "headless-req"})
	require.NoError(t, err)
	_, err = client.ControlPlaneStartHeaded(context.Background(), ControlPlaneStartHeadedOpts{ClientRequestID: "headed-req"})
	require.NoError(t, err)

	assert.Equal(t, "headless-req", seen["/invocations/start_headless"])
	assert.Equal(t, "headed-req", seen["/invocations/start_headed"])
}

func TestDaemonClient_SubmitFollowUpPreservesClientRequestID(t *testing.T) {
	t.Parallel()

	var seen string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(daemon.HealthResponse{OK: true, APIVersion: daemon.APIVersion})
			return
		}
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		seen = body["client_request_id"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(daemon.ControlPlaneFollowUpResponse{OK: true, APIVersion: daemon.APIVersion, ClientRequestID: seen})
	})

	client := NewClient(startFakeDaemon(t, handler))
	_, err := client.SubmitFollowUp(context.Background(), "inv-1", "repo-1", SubmitFollowUpOpts{ClientRequestID: "followup-req"})
	require.NoError(t, err)

	assert.Equal(t, "followup-req", seen)
}

func TestDaemonClient_MutationsCheckAPIVersionBeforeRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(*Client) error
	}{
		{name: "ControlPlaneStartHeadless", call: func(c *Client) error {
			_, err := c.ControlPlaneStartHeadless(context.Background(), ControlPlaneStartOpts{})
			return err
		}},
		{name: "ControlPlaneStartHeaded", call: func(c *Client) error {
			_, err := c.ControlPlaneStartHeaded(context.Background(), ControlPlaneStartHeadedOpts{})
			return err
		}},
		{name: "IngestHeadedHook", call: func(c *Client) error {
			_, err := c.IngestHeadedHook(context.Background(), "repo-1", "inv-1", "codex", []byte(`{}`))
			return err
		}},
		{name: "SubmitFollowUp", call: func(c *Client) error {
			_, err := c.SubmitFollowUp(context.Background(), "inv-1", "repo-1", SubmitFollowUpOpts{})
			return err
		}},
		{name: "Stop", call: func(c *Client) error {
			_, err := c.Stop(context.Background(), "repo-1", "inv-1")
			return err
		}},
		{name: "Kill", call: func(c *Client) error {
			_, err := c.Kill(context.Background(), "repo-1", "inv-1")
			return err
		}},
		{name: "Shutdown", call: func(c *Client) error {
			_, err := c.Shutdown(context.Background(), false)
			return err
		}},
		{name: "TaskStart", call: func(c *Client) error {
			_, err := c.TaskStart(context.Background(), TaskStartOpts{})
			return err
		}},
		{name: "ArchiveTask", call: func(c *Client) error {
			_, err := c.ArchiveTask(context.Background(), "task-1", "repo-1")
			return err
		}},
		{name: "RetryTask", call: func(c *Client) error {
			_, err := c.RetryTask(context.Background(), "task-1", "repo-1", TaskRetryOpts{})
			return err
		}},
		{name: "RegisterRepo", call: func(c *Client) error {
			_, err := c.RegisterRepo(context.Background(), "/repo")
			return err
		}},
		{name: "RepoRm", call: func(c *Client) error {
			_, err := c.RepoRm(context.Background(), "repo-1")
			return err
		}},
		{name: "WorktreeCreate", call: func(c *Client) error {
			_, err := c.WorktreeCreate(context.Background(), WorktreeCreateOpts{})
			return err
		}},
		{name: "WorktreeRm", call: func(c *Client) error {
			_, err := c.WorktreeRm(context.Background(), "repo-1", "wt-1", false)
			return err
		}},
		{name: "CheckpointApply", call: func(c *Client) error {
			_, err := c.CheckpointApply(context.Background(), "repo-1", "inv-1", 1)
			return err
		}},
		{name: "RecreateHeaded", call: func(c *Client) error {
			_, err := c.RecreateHeaded(context.Background(), "inv-1", "repo-1")
			return err
		}},
		{name: "Land", call: func(c *Client) error {
			_, err := c.Land(context.Background(), LandOpts{RepoID: "repo-1", InvocationID: "inv-1"})
			return err
		}},
		{name: "Discard", call: func(c *Client) error {
			_, err := c.Discard(context.Background(), "repo-1", "inv-1")
			return err
		}},
		{name: "WorktreePRSync", call: func(c *Client) error {
			_, err := c.WorktreePRSync(context.Background(), "wt-1", "repo-1", WorktreePRSyncOpts{})
			return err
		}},
		{name: "WorktreePRMerge", call: func(c *Client) error {
			_, err := c.WorktreePRMerge(context.Background(), "wt-1", "repo-1", WorktreePRMergeOpts{})
			return err
		}},
		{name: "WorktreeRebase", call: func(c *Client) error {
			_, err := c.WorktreeRebase(context.Background(), "wt-1", "repo-1")
			return err
		}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var mutations atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					_ = json.NewEncoder(w).Encode(daemon.HealthResponse{OK: true, APIVersion: daemon.APIVersion + 1})
					return
				}
				mutations.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			})

			client := NewClient(startFakeDaemon(t, handler))
			err := tt.call(client)

			require.Error(t, err)
			assert.Equal(t, errors.EDaemonIncompatible, errors.GetCode(err))
			assert.Zero(t, mutations.Load())
		})
	}
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
