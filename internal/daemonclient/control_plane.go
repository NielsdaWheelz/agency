package daemonclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/google/uuid"
)

// ControlPlaneStartHeadless starts a headless invocation via the control plane endpoint.
// This endpoint handles all creation: invocation ID generation, sandbox creation, and runner start.
func (c *Client) ControlPlaneStartHeadless(ctx context.Context, opts daemon.ControlPlaneStartRequest) (*daemon.ControlPlaneStartResponse, error) {
	if opts.ClientRequestID == "" {
		opts.ClientRequestID = uuid.New().String()
	}
	return postAction[daemon.ControlPlaneStartResponse](ctx, c, "/invocations/start_headless", "", opts)
}

// ControlPlaneStartHeaded starts a headed (tmux) invocation via the control plane endpoint.
// This endpoint handles all creation: invocation ID generation, sandbox creation, and tmux session start.
func (c *Client) ControlPlaneStartHeaded(ctx context.Context, opts daemon.ControlPlaneStartRequest) (*daemon.ControlPlaneStartHeadedResponse, error) {
	if opts.ClientRequestID == "" {
		opts.ClientRequestID = uuid.New().String()
	}
	return postAction[daemon.ControlPlaneStartHeadedResponse](ctx, c, "/invocations/start_headed", "", opts)
}

// IngestHeadedHook sends a headed runner hook payload to the daemon.
func (c *Client) IngestHeadedHook(ctx context.Context, repoID, invocationID, runner string, payload []byte) (*daemon.Result[daemon.HeadedHookIngestData], error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	q := url.Values{"repo_id": []string{repoID}}
	if runner != "" {
		q.Set("runner", runner)
	}
	u := queryURL("/invocations/"+url.PathEscape(invocationID)+"/headed_hook", q)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp rawAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	return decodeResult[daemon.HeadedHookIngestData](&apiResp)
}

// SubmitFollowUp submits a follow-up prompt to an existing invocation.
func (c *Client) SubmitFollowUp(ctx context.Context, invocationRef, repoID string, opts daemon.ControlPlaneFollowUpRequest) (*daemon.ControlPlaneFollowUpResponse, error) {
	if opts.ClientRequestID == "" {
		opts.ClientRequestID = uuid.New().String()
	}
	return postAction[daemon.ControlPlaneFollowUpResponse](ctx, c, "/invocations/"+url.PathEscape(invocationRef)+"/followup", repoID, opts)
}

// Stop sends a graceful stop signal to an invocation.
func (c *Client) Stop(ctx context.Context, repoID, invocationID string) (*daemon.InvocationActionResponse, error) {
	return postAction[daemon.InvocationActionResponse](ctx, c, "/invocations/"+url.PathEscape(invocationID)+"/stop", repoID, nil)
}

// Kill forcefully terminates an invocation.
func (c *Client) Kill(ctx context.Context, repoID, invocationID string) (*daemon.InvocationActionResponse, error) {
	return postAction[daemon.InvocationActionResponse](ctx, c, "/invocations/"+url.PathEscape(invocationID)+"/kill", repoID, nil)
}

// Shutdown requests graceful daemon shutdown.
func (c *Client) Shutdown(ctx context.Context, force bool) (*daemon.ShutdownResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	u := daemonBaseURL + "/shutdown"
	if force {
		u += "?force=true"
	}
	var result daemon.ShutdownResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, u, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
