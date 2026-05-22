package daemonclient

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync/atomic"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testAPIResponse struct {
	OK         bool   `json:"ok"`
	APIVersion int    `json:"api_version,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	Data       any    `json:"data,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Message    string `json:"message,omitempty"`
	Hint       string `json:"hint,omitempty"`
	Details    any    `json:"details,omitempty"`
}

type testInvalidQueryArgumentDetails struct {
	Param         string   `json:"param"`
	Value         string   `json:"value"`
	AllowedValues []string `json:"allowed_values"`
}

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

func requireAgencyError(t *testing.T, err error, code errors.Code, msg string) *errors.AgencyError {
	t.Helper()
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, code, ae.Code)
	if msg != "" {
		assert.Equal(t, msg, ae.Msg)
	}
	return ae
}

func requireDaemonReadError(t *testing.T, err error) *DaemonReadError {
	t.Helper()

	var dre *DaemonReadError
	require.True(t, stderrors.As(err, &dre), "error should be a DaemonReadError")
	return dre
}

func TestDaemonClient_ReadAPIErrorPassthrough_PreservesDetails(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := testAPIResponse{
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

	// DaemonReadError must be extractable.
	dre := requireDaemonReadError(t, err)

	// error_code and message preserved exactly
	ae := requireAgencyError(t, err, errors.EWorktreeIDAmbiguous, "worktree ref 'alpha' is ambiguous")

	// Hint remains on the wrapped AgencyError.
	assert.Equal(t, "specify the full worktree ID to disambiguate", ae.Details["hint"])

	// candidates recoverable as machine-readable data (not parsed from message)
	candidates := dre.Candidates()
	require.Len(t, candidates, 3)
	assert.Equal(t, []string{"wt-001", "wt-002", "wt-003"}, candidates)

	// raw details remain available inside daemonclient for structured accessors.
	require.NotEmpty(t, dre.rawDetails)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(dre.rawDetails, &raw))
	assert.Contains(t, raw, "candidates")

	// AgencyError extractable from the canonical read method.
	code := errors.GetCode(err)
	assert.Equal(t, errors.EWorktreeIDAmbiguous, code)
}

func TestDaemonClient_ReadAPIErrorPassthrough_Invocation(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := testAPIResponse{
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

	dre := requireDaemonReadError(t, err)
	ae := requireAgencyError(t, err, errors.EInvocationIDAmbiguous, "")
	assert.Equal(t, "use the full invocation ID", ae.Details["hint"])

	candidates := dre.Candidates()
	assert.Equal(t, []string{"inv-a", "inv-b"}, candidates)
}

func TestDaemonClient_GetWorktree_ReturnsDaemonReadError(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := testAPIResponse{
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

	_ = requireDaemonReadError(t, err)
	ae := requireAgencyError(t, err, errors.EWorktreeNotFound, "")
	assert.Equal(t, "canonical read method should preserve this hint", ae.Details["hint"])
}

func TestDaemonClient_ReadAPIErrorPassthrough_NoDetails(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := testAPIResponse{
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

	dre := requireDaemonReadError(t, err)
	ae := requireAgencyError(t, err, errors.EWorktreeNotFound, "")
	assert.Empty(t, ae.Details["hint"])
	assert.Nil(t, dre.Candidates())
}

func TestDaemonClient_ReadMethodsPreserveRichErrors(t *testing.T) {
	t.Parallel()

	expectedDetails := testInvalidQueryArgumentDetails{
		Param:         "state",
		Value:         "bogus",
		AllowedValues: []string{"present", "archived", "all"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := testAPIResponse{
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
				_, err := client.ListWorktrees(context.Background(), daemon.ListWorktreesParams{State: "bogus"})
				return err
			},
		},
		{
			name: "ListInvocations",
			call: func() error {
				_, err := client.ListInvocations(context.Background(), daemon.ListInvocationsParams{State: "bogus"})
				return err
			},
		},
		{
			name: "GetInvocationDiff",
			call: func() error {
				_, err := client.GetInvocationDiff(context.Background(), "inv-1", "repo-1", daemon.GetDiffParams{})
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
			name: "GetInvocationTimeline",
			call: func() error {
				_, err := client.GetInvocationTimeline(context.Background(), "inv-1", "repo-1", daemon.GetTimelineParams{})
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

			dre := requireDaemonReadError(t, err)
			ae := requireAgencyError(t, err, errors.EInvalidArgument, "invalid argument")
			assert.Equal(t, "preserve the structured read error", ae.Details["hint"])

			var details testInvalidQueryArgumentDetails
			require.NoError(t, json.Unmarshal(dre.rawDetails, &details))
			assert.Equal(t, expectedDetails, details)
		})
	}
}

func TestDaemonClient_GetInvocationDiffZeroValueUsesDaemonDefaults(t *testing.T) {
	t.Parallel()

	queries := make(chan url.Values, 2)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query()
		resp := testAPIResponse{
			OK:   true,
			Data: daemon.InvocationDiffData{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	client := NewClient(startFakeDaemon(t, handler))
	_, err := client.GetInvocationDiff(context.Background(), "inv-1", "repo-1", daemon.GetDiffParams{})
	require.NoError(t, err)
	defaultsQuery := <-queries
	assert.Equal(t, "repo-1", defaultsQuery.Get("repo_id"))
	assert.Empty(t, defaultsQuery.Get("include_patch"))
	assert.Empty(t, defaultsQuery.Get("include_uncommitted"))
	assert.Empty(t, defaultsQuery.Get("max_patch_bytes"))

	_, err = client.GetInvocationDiff(context.Background(), "inv-1", "repo-1", daemon.GetDiffParams{
		ExcludePatch:       true,
		ExcludeUncommitted: true,
		MaxPatchBytes:      1000,
	})
	require.NoError(t, err)
	overrideQuery := <-queries
	assert.Equal(t, "repo-1", overrideQuery.Get("repo_id"))
	assert.Equal(t, "false", overrideQuery.Get("include_patch"))
	assert.Equal(t, "false", overrideQuery.Get("include_uncommitted"))
	assert.Equal(t, "1000", overrideQuery.Get("max_patch_bytes"))
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
			_ = json.NewEncoder(w).Encode(daemon.ControlPlaneStartResponse{ResponseEnvelope: daemon.ResponseEnvelope{OK: true, APIVersion: daemon.APIVersion}, ClientRequestID: seen[r.URL.Path]})
		case "/invocations/start_headed":
			_ = json.NewEncoder(w).Encode(daemon.ControlPlaneStartHeadedResponse{ResponseEnvelope: daemon.ResponseEnvelope{OK: true, APIVersion: daemon.APIVersion}, ClientRequestID: seen[r.URL.Path]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	client := NewClient(startFakeDaemon(t, handler))
	_, err := client.ControlPlaneStartHeadless(context.Background(), daemon.ControlPlaneStartRequest{ClientRequestID: "headless-req"})
	require.NoError(t, err)
	_, err = client.ControlPlaneStartHeaded(context.Background(), daemon.ControlPlaneStartRequest{ClientRequestID: "headed-req"})
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
		_ = json.NewEncoder(w).Encode(daemon.ControlPlaneFollowUpResponse{ResponseEnvelope: daemon.ResponseEnvelope{OK: true, APIVersion: daemon.APIVersion}, ClientRequestID: seen})
	})

	client := NewClient(startFakeDaemon(t, handler))
	_, err := client.SubmitFollowUp(context.Background(), "inv-1", "repo-1", daemon.ControlPlaneFollowUpRequest{ClientRequestID: "followup-req"})
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
			_, err := c.ControlPlaneStartHeadless(context.Background(), daemon.ControlPlaneStartRequest{})
			return err
		}},
		{name: "ControlPlaneStartHeaded", call: func(c *Client) error {
			_, err := c.ControlPlaneStartHeaded(context.Background(), daemon.ControlPlaneStartRequest{})
			return err
		}},
		{name: "IngestHeadedHook", call: func(c *Client) error {
			_, err := c.IngestHeadedHook(context.Background(), "repo-1", "inv-1", "codex", []byte(`{}`))
			return err
		}},
		{name: "SubmitFollowUp", call: func(c *Client) error {
			_, err := c.SubmitFollowUp(context.Background(), "inv-1", "repo-1", daemon.ControlPlaneFollowUpRequest{})
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
			_, err := c.TaskStart(context.Background(), daemon.TaskStartRequest{})
			return err
		}},
		{name: "ArchiveTask", call: func(c *Client) error {
			_, err := c.ArchiveTask(context.Background(), "task-1", "repo-1")
			return err
		}},
		{name: "RetryTask", call: func(c *Client) error {
			_, err := c.RetryTask(context.Background(), "task-1", "repo-1", daemon.TaskRetryRequest{})
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
			_, err := c.WorktreeCreate(context.Background(), daemon.WorktreeCreateRequest{})
			return err
		}},
		{name: "WorktreeRm", call: func(c *Client) error {
			_, err := c.WorktreeRm(context.Background(), "repo-1", "wt-1", daemon.WorktreeRmRequest{})
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
			_, err := c.Land(context.Background(), "inv-1", "repo-1", daemon.LandRequest{})
			return err
		}},
		{name: "Discard", call: func(c *Client) error {
			_, err := c.Discard(context.Background(), "repo-1", "inv-1")
			return err
		}},
		{name: "WorktreePRSync", call: func(c *Client) error {
			_, err := c.WorktreePRSync(context.Background(), "wt-1", "repo-1", daemon.WorktreePRSyncRequest{})
			return err
		}},
		{name: "WorktreePRMerge", call: func(c *Client) error {
			_, err := c.WorktreePRMerge(context.Background(), "wt-1", "repo-1", daemon.WorktreePRMergeRequest{})
			return err
		}},
		{name: "WorktreeRebase", call: func(c *Client) error {
			_, err := c.WorktreeRebase(context.Background(), "wt-1", "repo-1")
			return err
		}},
	}

	for _, tt := range tests {
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

func TestDaemonClient_GetInvocationTimeline_OrderControlsReturnedEntries(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entryID := "oldest"
		if r.URL.Query().Get("order") == "desc" {
			entryID = "newest"
		}
		resp := testAPIResponse{
			OK:   true,
			Data: daemon.InvocationTimelineData{Entries: []daemon.TimelineEntryDTO{{EntryID: entryID}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	socketPath := startFakeDaemon(t, handler)
	client := NewClient(socketPath)

	desc, err := client.GetInvocationTimeline(context.Background(), "inv-1", "repo-1", daemon.GetTimelineParams{
		Limit: 1,
		Order: "desc",
	})
	require.NoError(t, err)
	require.Len(t, desc.Data.Entries, 1)
	assert.Equal(t, "newest", desc.Data.Entries[0].EntryID)

	defaultOrder, err := client.GetInvocationTimeline(context.Background(), "inv-1", "repo-1", daemon.GetTimelineParams{
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, defaultOrder.Data.Entries, 1)
	assert.Equal(t, "oldest", defaultOrder.Data.Entries[0].EntryID)
}
