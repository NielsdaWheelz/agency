// Package daemonclient provides a client for communicating with the agency daemon.
package daemonclient

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

// DaemonReadError carries the full daemon read API error envelope for consumers
// that need hint and structured details (e.g., ambiguity candidates).
// It wraps an AgencyError so errors.GetCode and errors.AsAgencyError still work.
type DaemonReadError struct {
	AgencyErr  *errors.AgencyError
	Hint       string
	RawDetails json.RawMessage
}

func (e *DaemonReadError) Error() string { return e.AgencyErr.Error() }
func (e *DaemonReadError) Unwrap() error { return e.AgencyErr }

// Candidates extracts candidate strings from RawDetails when the details
// contain a "candidates" array (e.g., daemon AmbiguousDetails).
func (e *DaemonReadError) Candidates() []string {
	if len(e.RawDetails) == 0 {
		return nil
	}
	var ad struct {
		Candidates []string `json:"candidates"`
	}
	if err := json.Unmarshal(e.RawDetails, &ad); err != nil {
		return nil
	}
	return ad.Candidates
}

// AsDaemonReadError extracts a DaemonReadError from an error chain.
func AsDaemonReadError(err error) (*DaemonReadError, bool) {
	var dre *DaemonReadError
	if stderrors.As(err, &dre) {
		return dre, true
	}
	return nil, false
}

// readAPIErrorRich creates a DaemonReadError from a failed APIResponse,
// preserving error_code, message, hint, and raw structured details.
func readAPIErrorRich(resp daemon.APIResponse) *DaemonReadError {
	var rawDetails json.RawMessage
	if resp.Details != nil {
		rawDetails, _ = json.Marshal(resp.Details)
	}
	return &DaemonReadError{
		AgencyErr: &errors.AgencyError{
			Code: errors.Code(resp.ErrorCode),
			Msg:  resp.Message,
		},
		Hint:       resp.Hint,
		RawDetails: rawDetails,
	}
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

// ControlPlaneStartOpts holds options for control plane start (headless).
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

// ControlPlaneStartHeadedOpts holds options for control plane headed start (PR-10).
type ControlPlaneStartHeadedOpts struct {
	RepoRoot           string
	WorktreeRef        string
	Runner             string
	InvocationName     string
	RunnerArgs         []string
	Env                map[string]string
	NoIncludeUntracked bool
}

// SubmitFollowUpPromptOpts holds options for invocation-scoped follow-up prompts (S3 PR-02).
type SubmitFollowUpPromptOpts struct {
	Prompt string
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

// ControlPlaneStartHeaded starts a headed (tmux) invocation via the control plane endpoint (PR-10).
// This endpoint handles all creation: invocation ID generation, sandbox creation, and tmux session start.
func (c *Client) ControlPlaneStartHeaded(ctx context.Context, opts ControlPlaneStartHeadedOpts) (*daemon.ControlPlaneStartHeadedResponse, error) {
	// Generate client request ID for idempotency
	clientRequestID := uuid.New().String()

	req := daemon.ControlPlaneStartHeadedRequest{
		RepoRoot:           opts.RepoRoot,
		WorktreeRef:        opts.WorktreeRef,
		Runner:             opts.Runner,
		InvocationName:     opts.InvocationName,
		RunnerArgs:         opts.RunnerArgs,
		Env:                opts.Env,
		ClientRequestID:    clientRequestID,
		NoIncludeUntracked: opts.NoIncludeUntracked,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := "http://daemon/invocations/start_headed"
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

	var result daemon.ControlPlaneStartHeadedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SubmitFollowUpPrompt submits a follow-up prompt to an existing invocation (S3 PR-02).
func (c *Client) SubmitFollowUpPrompt(ctx context.Context, invocationRef, repoID string, opts SubmitFollowUpPromptOpts) (*daemon.ControlPlaneFollowUpPromptResponse, error) {
	clientRequestID := uuid.New().String()
	reqBody := daemon.ControlPlaneFollowUpPromptRequest{
		Prompt:          opts.Prompt,
		ClientRequestID: clientRequestID,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	u := fmt.Sprintf("http://daemon/invocations/%s/chat", url.PathEscape(invocationRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.ControlPlaneFollowUpPromptResponse
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

// RestartFromCheckpointOpts holds options for canonical restart-from-checkpoint flow (S3 PR-03).
type RestartFromCheckpointOpts struct {
	CheckpointID int
	Env          map[string]string
	RunnerArgs   []string
}

// RestartFromCheckpoint applies checkpoint and restarts the same invocation in one flow.
func (c *Client) RestartFromCheckpoint(ctx context.Context, invocationRef, repoID string, opts RestartFromCheckpointOpts) (*daemon.RestartFromCheckpointResponse, error) {
	req := daemon.RestartFromCheckpointRequest{
		CheckpointID: opts.CheckpointID,
		Env:          opts.Env,
		RunnerArgs:   opts.RunnerArgs,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	u := fmt.Sprintf("http://daemon/invocations/%s/restart", url.PathEscape(invocationRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.RestartFromCheckpointResponse
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

// ----- PR-12 Read API Client Methods -----

// ListWorktreesOpts holds options for listing worktrees.
type ListWorktreesOpts struct {
	RepoID string // optional, filter by repo
	State  string // present, archived, all (default: present)
	Limit  int    // default 100, max 500
	Cursor string // opaque pagination cursor
}

// ListWorktreesResult wraps the list worktrees response.
type ListWorktreesResult struct {
	Worktrees  []daemon.WorktreeDTO
	NextCursor string
	RequestID  string
}

// ListWorktrees lists integration worktrees via the daemon.
func (c *Client) ListWorktrees(ctx context.Context, opts ListWorktreesOpts) (*ListWorktreesResult, error) {
	u := "http://daemon/worktrees?"
	if opts.RepoID != "" {
		u += "repo_id=" + url.QueryEscape(opts.RepoID) + "&"
	}
	if opts.State != "" {
		u += "state=" + url.QueryEscape(opts.State) + "&"
	}
	if opts.Limit > 0 {
		u += fmt.Sprintf("limit=%d&", opts.Limit)
	}
	if opts.Cursor != "" {
		u += "cursor=" + url.QueryEscape(opts.Cursor) + "&"
	}
	u = u[:len(u)-1] // trim trailing & or ?

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	// Decode data field
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var data daemon.ListWorktreesData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, err
	}

	return &ListWorktreesResult{
		Worktrees:  data.Worktrees,
		NextCursor: data.NextCursor,
		RequestID:  apiResp.RequestID,
	}, nil
}

// GetWorktreeResult wraps the get worktree response.
type GetWorktreeResult struct {
	Worktree  daemon.WorktreeDTO
	RequestID string
}

// GetWorktree gets a single worktree by reference via the daemon.
func (c *Client) GetWorktree(ctx context.Context, ref string, repoID string) (*GetWorktreeResult, error) {
	u := fmt.Sprintf("http://daemon/worktrees/%s", url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	// Decode data field
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var worktree daemon.WorktreeDTO
	if err := json.Unmarshal(dataBytes, &worktree); err != nil {
		return nil, err
	}

	return &GetWorktreeResult{
		Worktree:  worktree,
		RequestID: apiResp.RequestID,
	}, nil
}

// GetWorktreeRich gets a worktree by reference via daemon, preserving full error details
// (hint, structured details) for navigation kernel consumers.
// Existing GetWorktree callers are unaffected — use this when you need candidate data.
func (c *Client) GetWorktreeRich(ctx context.Context, ref string, repoID string) (*GetWorktreeResult, error) {
	u := fmt.Sprintf("http://daemon/worktrees/%s", url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, readAPIErrorRich(apiResp)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var worktree daemon.WorktreeDTO
	if err := json.Unmarshal(dataBytes, &worktree); err != nil {
		return nil, err
	}

	return &GetWorktreeResult{
		Worktree:  worktree,
		RequestID: apiResp.RequestID,
	}, nil
}

// ListInvocationsOpts holds options for listing invocations.
type ListInvocationsOpts struct {
	RepoID      string // optional, filter by repo
	WorktreeRef string // optional, filter by worktree ref
	State       string // active, finished, all (default: all)
	Mode        string // headed, headless, all (default: all)
	Limit       int    // default 100, max 500
	Cursor      string // opaque pagination cursor
}

// ListInvocationsResult wraps the list invocations response.
type ListInvocationsResult struct {
	Invocations []daemon.InvocationDTO
	NextCursor  string
	RequestID   string
}

// ListInvocations lists invocations via the daemon.
func (c *Client) ListInvocations(ctx context.Context, opts ListInvocationsOpts) (*ListInvocationsResult, error) {
	u := "http://daemon/invocations?"
	if opts.RepoID != "" {
		u += "repo_id=" + url.QueryEscape(opts.RepoID) + "&"
	}
	if opts.WorktreeRef != "" {
		u += "worktree_ref=" + url.QueryEscape(opts.WorktreeRef) + "&"
	}
	if opts.State != "" {
		u += "state=" + url.QueryEscape(opts.State) + "&"
	}
	if opts.Mode != "" {
		u += "mode=" + url.QueryEscape(opts.Mode) + "&"
	}
	if opts.Limit > 0 {
		u += fmt.Sprintf("limit=%d&", opts.Limit)
	}
	if opts.Cursor != "" {
		u += "cursor=" + url.QueryEscape(opts.Cursor) + "&"
	}
	u = u[:len(u)-1] // trim trailing & or ?

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	// Decode data field
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var data daemon.ListInvocationsData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, err
	}

	return &ListInvocationsResult{
		Invocations: data.Invocations,
		NextCursor:  data.NextCursor,
		RequestID:   apiResp.RequestID,
	}, nil
}

// GetInvocationResult wraps the get invocation response.
type GetInvocationResult struct {
	Invocation daemon.InvocationDTO
	RequestID  string
}

// GetInvocation gets a single invocation by reference via the daemon.
func (c *Client) GetInvocation(ctx context.Context, ref string, repoID string) (*GetInvocationResult, error) {
	u := fmt.Sprintf("http://daemon/invocations/%s", url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	// Decode data field
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var invocation daemon.InvocationDTO
	if err := json.Unmarshal(dataBytes, &invocation); err != nil {
		return nil, err
	}

	return &GetInvocationResult{
		Invocation: invocation,
		RequestID:  apiResp.RequestID,
	}, nil
}

// GetInvocationRich gets an invocation by reference via daemon, preserving full error details
// (hint, structured details) for navigation kernel consumers.
// Existing GetInvocation callers are unaffected — use this when you need candidate data.
func (c *Client) GetInvocationRich(ctx context.Context, ref string, repoID string) (*GetInvocationResult, error) {
	u := fmt.Sprintf("http://daemon/invocations/%s", url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, readAPIErrorRich(apiResp)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var invocation daemon.InvocationDTO
	if err := json.Unmarshal(dataBytes, &invocation); err != nil {
		return nil, err
	}

	return &GetInvocationResult{
		Invocation: invocation,
		RequestID:  apiResp.RequestID,
	}, nil
}

// GetInvocationDiffOpts holds options for getting invocation diff.
type GetInvocationDiffOpts struct {
	IncludePatch       bool // default true
	MaxPatchBytes      int  // default 2MB, max 5MB
	IncludeUncommitted bool // default true
}

// GetInvocationDiffResult wraps the invocation diff response.
type GetInvocationDiffResult struct {
	Diff      daemon.InvocationDiffData
	RequestID string
}

// GetInvocationDiff gets the diff for an invocation via the daemon.
func (c *Client) GetInvocationDiff(ctx context.Context, ref string, repoID string, opts GetInvocationDiffOpts) (*GetInvocationDiffResult, error) {
	u := fmt.Sprintf("http://daemon/invocations/%s/diff?", url.PathEscape(ref))
	if repoID != "" {
		u += "repo_id=" + url.QueryEscape(repoID) + "&"
	}
	if !opts.IncludePatch {
		u += "include_patch=false&"
	}
	if opts.MaxPatchBytes > 0 {
		u += fmt.Sprintf("max_patch_bytes=%d&", opts.MaxPatchBytes)
	}
	if !opts.IncludeUncommitted {
		u += "include_uncommitted=false&"
	}
	u = u[:len(u)-1] // trim trailing & or ?

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	// Decode data field
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var diff daemon.InvocationDiffData
	if err := json.Unmarshal(dataBytes, &diff); err != nil {
		return nil, err
	}

	return &GetInvocationDiffResult{
		Diff:      diff,
		RequestID: apiResp.RequestID,
	}, nil
}

// GetInvocationLogsOpts holds options for getting invocation logs.
type GetInvocationLogsOpts struct {
	Kind      string // raw, stderr, stream (default: raw)
	TailBytes int    // default 64KB, max 1MB
}

// GetInvocationLogsResult wraps the invocation logs response.
type GetInvocationLogsResult struct {
	Logs      daemon.InvocationLogsData
	RequestID string
}

// GetInvocationLogs gets logs for an invocation via the daemon.
func (c *Client) GetInvocationLogs(ctx context.Context, ref string, repoID string, opts GetInvocationLogsOpts) (*GetInvocationLogsResult, error) {
	u := fmt.Sprintf("http://daemon/invocations/%s/logs?", url.PathEscape(ref))
	if repoID != "" {
		u += "repo_id=" + url.QueryEscape(repoID) + "&"
	}
	if opts.Kind != "" {
		u += "kind=" + url.QueryEscape(opts.Kind) + "&"
	}
	if opts.TailBytes > 0 {
		u += fmt.Sprintf("tail_bytes=%d&", opts.TailBytes)
	}
	u = u[:len(u)-1] // trim trailing & or ?

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	// Decode data field
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var logs daemon.InvocationLogsData
	if err := json.Unmarshal(dataBytes, &logs); err != nil {
		return nil, err
	}

	return &GetInvocationLogsResult{
		Logs:      logs,
		RequestID: apiResp.RequestID,
	}, nil
}

// GetInvocationLogsOffsetOpts holds options for offset-based log reads (PR-B).
type GetInvocationLogsOffsetOpts struct {
	Kind   string // raw, stderr, stream (default: raw)
	Offset int64  // byte offset from start of file
	Limit  int    // max bytes returned (default 65536)
}

// GetInvocationLogsOffsetResult wraps the offset-mode logs response.
type GetInvocationLogsOffsetResult struct {
	Logs      daemon.InvocationLogsOffsetData
	RequestID string
}

// GetInvocationLogsOffset gets logs at a byte offset for an invocation via the daemon (PR-B).
func (c *Client) GetInvocationLogsOffset(ctx context.Context, ref string, repoID string, opts GetInvocationLogsOffsetOpts) (*GetInvocationLogsOffsetResult, error) {
	u := fmt.Sprintf("http://daemon/invocations/%s/logs?", url.PathEscape(ref))
	if repoID != "" {
		u += "repo_id=" + url.QueryEscape(repoID) + "&"
	}
	if opts.Kind != "" {
		u += "kind=" + url.QueryEscape(opts.Kind) + "&"
	}
	u += fmt.Sprintf("offset=%d&", opts.Offset)
	limit := opts.Limit
	if limit <= 0 {
		limit = 65536
	}
	u += fmt.Sprintf("limit=%d", limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	// Decode data field
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var logs daemon.InvocationLogsOffsetData
	if err := json.Unmarshal(dataBytes, &logs); err != nil {
		return nil, err
	}

	return &GetInvocationLogsOffsetResult{
		Logs:      logs,
		RequestID: apiResp.RequestID,
	}, nil
}

// GetInvocationTimelineOpts holds options for timeline reads.
type GetInvocationTimelineOpts struct {
	Limit  int    // default 100, max 500
	Cursor string // opaque pagination cursor
}

// GetInvocationTimelineResult wraps the timeline response.
type GetInvocationTimelineResult struct {
	Entries    []daemon.TimelineEntryDTO
	NextCursor string
	RequestID  string
}

// GetInvocationTimeline gets the unified timeline for an invocation via daemon.
func (c *Client) GetInvocationTimeline(ctx context.Context, ref string, repoID string, opts GetInvocationTimelineOpts) (*GetInvocationTimelineResult, error) {
	u := fmt.Sprintf("http://daemon/invocations/%s/timeline?", url.PathEscape(ref))
	if repoID != "" {
		u += "repo_id=" + url.QueryEscape(repoID) + "&"
	}
	if opts.Limit > 0 {
		u += fmt.Sprintf("limit=%d&", opts.Limit)
	}
	if opts.Cursor != "" {
		u += "cursor=" + url.QueryEscape(opts.Cursor) + "&"
	}
	u = u[:len(u)-1] // trim trailing & or ?

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var data daemon.InvocationTimelineData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, err
	}

	return &GetInvocationTimelineResult{
		Entries:    data.Entries,
		NextCursor: data.NextCursor,
		RequestID:  apiResp.RequestID,
	}, nil
}

// ListCheckpointsOpts holds options for listing checkpoints.
type ListCheckpointsOpts struct {
	Limit  int    // default 100, max 500
	Cursor string // opaque pagination cursor
}

// ListCheckpointsResult wraps the list checkpoints response.
type ListCheckpointsResult struct {
	Checkpoints []daemon.CheckpointDTO
	NextCursor  string
	RequestID   string
}

// ListCheckpoints lists checkpoints for an invocation via the daemon.
func (c *Client) ListCheckpoints(ctx context.Context, invocationRef string, repoID string, opts ListCheckpointsOpts) (*ListCheckpointsResult, error) {
	u := fmt.Sprintf("http://daemon/invocations/%s/checkpoints?", url.PathEscape(invocationRef))
	if repoID != "" {
		u += "repo_id=" + url.QueryEscape(repoID) + "&"
	}
	if opts.Limit > 0 {
		u += fmt.Sprintf("limit=%d&", opts.Limit)
	}
	if opts.Cursor != "" {
		u += "cursor=" + url.QueryEscape(opts.Cursor) + "&"
	}
	u = u[:len(u)-1] // trim trailing & or ?

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	// Decode data field
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var data daemon.ListCheckpointsData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, err
	}

	return &ListCheckpointsResult{
		Checkpoints: data.Checkpoints,
		NextCursor:  data.NextCursor,
		RequestID:   apiResp.RequestID,
	}, nil
}

// ----- PR-A: Repo Registry Client Methods -----

// RegisterRepoResult wraps the register repo response.
type RegisterRepoResult struct {
	RepoID                  string
	RepoKey                 string
	Paths                   []string
	PreferredRoot           string
	PreferredRootAccessible bool
	LastSeenAt              string
}

// RegisterRepo registers a repo root with the daemon and returns the resolved repo_id.
// This is the canonical way to get repo_id — CLI should not compute it locally.
func (c *Client) RegisterRepo(ctx context.Context, repoRoot string) (*RegisterRepoResult, error) {
	reqBody := daemon.RepoRegisterRequest{
		RepoRoot: repoRoot,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://daemon/repos/register", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result daemon.RepoRegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.OK {
		return nil, errors.New(errors.Code(result.ErrorCode), result.Message)
	}

	if result.Data == nil {
		return nil, errors.New(errors.EInternal, "daemon returned success but no data")
	}

	return &RegisterRepoResult{
		RepoID:                  result.Data.RepoID,
		RepoKey:                 result.Data.RepoKey,
		Paths:                   result.Data.Paths,
		PreferredRoot:           result.Data.PreferredRoot,
		PreferredRootAccessible: result.Data.PreferredRootAccessible,
		LastSeenAt:              result.Data.LastSeenAt,
	}, nil
}

// ListReposResult wraps the list repos response.
type ListReposResult struct {
	Repos     []daemon.RepoDTO
	RequestID string
}

// ListRepos lists all registered repos.
func (c *Client) ListRepos(ctx context.Context) (*ListReposResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://daemon/repos", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var data daemon.ListReposData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, err
	}

	return &ListReposResult{
		Repos:     data.Repos,
		RequestID: apiResp.RequestID,
	}, nil
}

// GetRepoResult wraps the get repo response.
type GetRepoResult struct {
	Repo      daemon.RepoDTO
	RequestID string
}

// GetRepo gets a single repo by ID.
func (c *Client) GetRepo(ctx context.Context, repoID string) (*GetRepoResult, error) {
	u := fmt.Sprintf("http://daemon/repos/%s", url.PathEscape(repoID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var dto daemon.RepoDTO
	if err := json.Unmarshal(dataBytes, &dto); err != nil {
		return nil, err
	}

	return &GetRepoResult{
		Repo:      dto,
		RequestID: apiResp.RequestID,
	}, nil
}

// ----- PR-05 S1 Release Gate Client Methods -----

// S1ReleaseReadinessResult wraps the S1 release readiness response.
type S1ReleaseReadinessResult struct {
	Data      daemon.S1ReleaseReadinessData
	RequestID string
}

// GetS1ReleaseReadiness queries the daemon for S1 release readiness.
func (c *Client) GetS1ReleaseReadiness(ctx context.Context, repoID string) (*S1ReleaseReadinessResult, error) {
	u := "http://daemon/spec/v2.1/s1/release/readiness?repo_id=" + url.QueryEscape(repoID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var data daemon.S1ReleaseReadinessData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, err
	}

	return &S1ReleaseReadinessResult{
		Data:      data,
		RequestID: apiResp.RequestID,
	}, nil
}

// S1ClosureReportResult wraps the S1 closure report response.
type S1ClosureReportResult struct {
	Data      daemon.S1ClosureReportData
	RequestID string
}

// GetS1ClosureReport queries the daemon for the S1 closure report.
func (c *Client) GetS1ClosureReport(ctx context.Context, repoID string) (*S1ClosureReportResult, error) {
	u := "http://daemon/spec/v2.1/s1/release/closure-report?repo_id=" + url.QueryEscape(repoID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var data daemon.S1ClosureReportData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, err
	}

	return &S1ClosureReportResult{
		Data:      data,
		RequestID: apiResp.RequestID,
	}, nil
}

// S1FreezeReadinessResult wraps the S1 freeze readiness response.
type S1FreezeReadinessResult struct {
	Data      daemon.S1FreezeReadinessData
	RequestID string
}

// GetS1FreezeReadiness queries the daemon for S1 freeze readiness.
func (c *Client) GetS1FreezeReadiness(ctx context.Context, repoID string) (*S1FreezeReadinessResult, error) {
	u := "http://daemon/spec/v2.1/s1/release/freeze-readiness?repo_id=" + url.QueryEscape(repoID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, err
	}
	var data daemon.S1FreezeReadinessData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, err
	}

	return &S1FreezeReadinessResult{
		Data:      data,
		RequestID: apiResp.RequestID,
	}, nil
}
