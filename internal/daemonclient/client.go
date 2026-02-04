// Package daemonclient provides a client for communicating with the agency daemon.
package daemonclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/google/uuid"
)

// Client communicates with the agency daemon over Unix socket.
type Client struct {
	socketPath string
	httpClient *http.Client
}

// NewClient creates a new daemon client.
func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}

	return &Client{
		socketPath: socketPath,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}
}

// Health checks the daemon health.
func (c *Client) Health(ctx context.Context) (*daemon.HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://daemon/health", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var health daemon.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, err
	}

	return &health, nil
}

// IsRunning checks if the daemon is running and healthy.
func (c *Client) IsRunning(ctx context.Context) bool {
	health, err := c.Health(ctx)
	return err == nil && health.OK
}

// StartHeadless starts a headless invocation (legacy PR-04 endpoint).
func (c *Client) StartHeadless(ctx context.Context, req *daemon.StartHeadlessRequest) (*daemon.StartHeadlessResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("http://daemon/invocations/%s/start_headless", req.InvocationID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.StartHeadlessResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ControlPlaneStartOpts holds options for control plane start.
type ControlPlaneStartOpts struct {
	RepoRoot           string
	WorktreeRef        string
	Runner             string
	Prompt             string
	InvocationName     string
	RunnerArgs         []string
	Env                map[string]string
	NoIncludeUntracked bool // PR-08: exclude untracked files from checkpoints
}

// ControlPlaneStartHeadless starts a headless invocation via the control plane endpoint (PR-05).
// This endpoint handles all creation: invocation ID generation, sandbox creation, and runner start.
func (c *Client) ControlPlaneStartHeadless(ctx context.Context, opts ControlPlaneStartOpts) (*daemon.ControlPlaneStartResponse, error) {
	// Generate client request ID for idempotency
	clientRequestID := uuid.New().String()

	req := daemon.ControlPlaneStartRequest{
		RepoRoot:           opts.RepoRoot,
		WorktreeRef:        opts.WorktreeRef,
		Runner:             opts.Runner,
		Prompt:             opts.Prompt,
		InvocationName:     opts.InvocationName,
		RunnerArgs:         opts.RunnerArgs,
		Env:                opts.Env,
		ClientRequestID:    clientRequestID,
		NoIncludeUntracked: opts.NoIncludeUntracked, // PR-08
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := "http://daemon/invocations/start_headless"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.ControlPlaneStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// CheckAPIVersion checks if the daemon API version is compatible with the client.
// Returns nil if compatible, error otherwise.
func (c *Client) CheckAPIVersion(ctx context.Context) error {
	health, err := c.Health(ctx)
	if err != nil {
		return err
	}

	if health.APIVersion != daemon.APIVersion {
		return errors.NewWithDetails(
			errors.EDaemonIncompatible,
			fmt.Sprintf("daemon API version mismatch: daemon=%d, client=%d", health.APIVersion, daemon.APIVersion),
			map[string]string{
				"daemon_version": fmt.Sprintf("%d", health.APIVersion),
				"client_version": fmt.Sprintf("%d", daemon.APIVersion),
				"hint":           "restart the daemon with 'agency daemon stop && agency daemon start'",
			},
		)
	}

	return nil
}

// Stop sends a graceful stop signal to an invocation.
func (c *Client) Stop(ctx context.Context, repoID, invocationID string) (*daemon.StopResponse, error) {
	url := fmt.Sprintf("http://daemon/invocations/%s/stop?repo_id=%s", invocationID, repoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.StopResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Kill forcefully terminates an invocation.
func (c *Client) Kill(ctx context.Context, repoID, invocationID string) (*daemon.KillResponse, error) {
	url := fmt.Sprintf("http://daemon/invocations/%s/kill?repo_id=%s", invocationID, repoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.KillResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Shutdown requests graceful daemon shutdown.
func (c *Client) Shutdown(ctx context.Context, force bool) (*daemon.ShutdownResponse, error) {
	url := "http://daemon/shutdown"
	if force {
		url += "?force=true"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.ShutdownResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// WaitForReady waits for the daemon to be ready, polling with exponential backoff.
func (c *Client) WaitForReady(ctx context.Context, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	wait := 50 * time.Millisecond

	for time.Now().Before(deadline) {
		if c.IsRunning(ctx) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}

		// Exponential backoff, capped at 500ms
		wait = wait * 2
		if wait > 500*time.Millisecond {
			wait = 500 * time.Millisecond
		}
	}

	return errors.New(errors.EDaemonNotRunning, "daemon did not become ready within timeout")
}

// ReadRawLog reads the raw log file for an invocation.
func (c *Client) ReadRawLog(logPath string) ([]byte, error) {
	return io.ReadAll(mustOpenFile(logPath))
}

func mustOpenFile(path string) io.Reader {
	f, err := http.DefaultClient.Get("file://" + path)
	if err != nil {
		return bytes.NewReader(nil)
	}
	return f.Body
}

// ----- PR-06 Worktree Client Methods -----

// WorktreeCreateOpts holds options for worktree creation via daemon.
type WorktreeCreateOpts struct {
	RepoRoot       string
	Name           string
	ParentBranch   string
	IdempotencyKey string
}

// WorktreeCreate creates an integration worktree via the daemon.
func (c *Client) WorktreeCreate(ctx context.Context, opts WorktreeCreateOpts) (*daemon.WorktreeCreateResponse, error) {
	req := daemon.WorktreeCreateRequest{
		RepoRoot:       opts.RepoRoot,
		Name:           opts.Name,
		ParentBranch:   opts.ParentBranch,
		IdempotencyKey: opts.IdempotencyKey,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://daemon/worktrees/create", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.WorktreeCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// WorktreeRm removes an integration worktree via the daemon.
func (c *Client) WorktreeRm(ctx context.Context, repoID, worktreeRef string, force bool) (*daemon.WorktreeRmResponse, error) {
	req := daemon.WorktreeRmRequest{
		Force: force,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("http://daemon/worktrees/%s/rm?repo_id=%s", worktreeRef, repoID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.WorktreeRmResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ----- PR-08 Checkpoint Client Methods -----

// CheckpointApply applies a checkpoint to an invocation's sandbox.
func (c *Client) CheckpointApply(ctx context.Context, repoID, invocationID string, checkpointID int) (*daemon.CheckpointApplyResponse, error) {
	req := daemon.CheckpointApplyRequest{
		CheckpointID: checkpointID,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("http://daemon/invocations/%s/checkpoints/apply?repo_id=%s", invocationID, repoID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.CheckpointApplyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ----- PR-09 Landing Client Methods -----

// LandOpts holds options for landing via daemon.
type LandOpts struct {
	RepoID       string
	InvocationID string
	Apply        bool
	RequireBase  bool
}

// Land lands sandbox changes to the integration worktree via daemon.
func (c *Client) Land(ctx context.Context, opts LandOpts) (*daemon.LandResponse, error) {
	req := daemon.LandRequest{
		Apply:       opts.Apply,
		RequireBase: opts.RequireBase,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("http://daemon/invocations/%s/land?repo_id=%s", opts.InvocationID, opts.RepoID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.LandResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Discard discards a sandbox without landing via daemon.
func (c *Client) Discard(ctx context.Context, repoID, invocationID string) (*daemon.DiscardResponse, error) {
	req := daemon.DiscardRequest{}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("http://daemon/invocations/%s/discard?repo_id=%s", invocationID, repoID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.DiscardResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
