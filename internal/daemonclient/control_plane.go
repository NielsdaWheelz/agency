package daemonclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/google/uuid"
)

// ControlPlaneStartHeadless starts a headless invocation via the control plane endpoint.
// This endpoint handles all creation: invocation ID generation, sandbox creation, and runner start.
func (c *Client) ControlPlaneStartHeadless(ctx context.Context, opts ControlPlaneStartOpts) (*daemon.ControlPlaneStartResponse, error) {
	opts.ClientRequestID = uuid.New().String()

	var result daemon.ControlPlaneStartResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, daemonBaseURL+"/invocations/start_headless", opts, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ControlPlaneStartHeaded starts a headed (tmux) invocation via the control plane endpoint.
// This endpoint handles all creation: invocation ID generation, sandbox creation, and tmux session start.
func (c *Client) ControlPlaneStartHeaded(ctx context.Context, opts ControlPlaneStartHeadedOpts) (*daemon.ControlPlaneStartHeadedResponse, error) {
	opts.ClientRequestID = uuid.New().String()

	var result daemon.ControlPlaneStartHeadedResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, daemonBaseURL+"/invocations/start_headed", opts, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// IngestHeadedHook sends a headed runner hook payload to the daemon.
func (c *Client) IngestHeadedHook(ctx context.Context, repoID, invocationID, runner string, payload []byte) (*daemon.Result[daemon.HeadedHookIngestData], error) {
	u := fmt.Sprintf("%s/invocations/%s/headed_hook?repo_id=%s", daemonBaseURL, url.PathEscape(invocationID), url.QueryEscape(repoID))
	if runner != "" {
		u += "&runner=" + url.QueryEscape(runner)
	}
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

	var apiResp daemon.RawAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	return decodeResult[daemon.HeadedHookIngestData](&apiResp)
}

// SubmitFollowUpPrompt submits a follow-up prompt to an existing invocation.
func (c *Client) SubmitFollowUpPrompt(ctx context.Context, invocationRef, repoID string, opts SubmitFollowUpPromptOpts) (*daemon.ControlPlaneFollowUpPromptResponse, error) {
	opts.ClientRequestID = uuid.New().String()

	u := fmt.Sprintf("http://daemon/invocations/%s/chat", url.PathEscape(invocationRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	var result daemon.ControlPlaneFollowUpPromptResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, u, opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Stop sends a graceful stop signal to an invocation.
func (c *Client) Stop(ctx context.Context, repoID, invocationID string) (*daemon.InvocationActionResponse, error) {
	var result daemon.InvocationActionResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, fmt.Sprintf("%s/invocations/%s/stop?repo_id=%s", daemonBaseURL, url.PathEscape(invocationID), url.QueryEscape(repoID)), nil, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Kill forcefully terminates an invocation.
func (c *Client) Kill(ctx context.Context, repoID, invocationID string) (*daemon.InvocationActionResponse, error) {
	var result daemon.InvocationActionResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, fmt.Sprintf("%s/invocations/%s/kill?repo_id=%s", daemonBaseURL, url.PathEscape(invocationID), url.QueryEscape(repoID)), nil, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Shutdown requests graceful daemon shutdown.
func (c *Client) Shutdown(ctx context.Context, force bool) (*daemon.ShutdownResponse, error) {
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
