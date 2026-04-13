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
	httpClient *http.Client
}

const daemonBaseURL = "http://daemon"

type apiResponseEnvelope struct {
	OK           bool            `json:"ok"`
	APIVersion   int             `json:"api_version"`
	BuildVersion string          `json:"build_version,omitempty"`
	GitSHA       string          `json:"git_sha,omitempty"`
	RequestID    string          `json:"request_id,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	Message      string          `json:"message,omitempty"`
	Hint         string          `json:"hint,omitempty"`
	Details      json.RawMessage `json:"details,omitempty"`
	Data         json.RawMessage `json:"data,omitempty"`
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
func readAPIErrorRich(resp apiResponseEnvelope) *DaemonReadError {
	var rawDetails json.RawMessage
	rawDetails = append(json.RawMessage(nil), resp.Details...)
	return &DaemonReadError{
		AgencyErr: &errors.AgencyError{
			Code: errors.Code(resp.ErrorCode),
			Msg:  resp.Message,
		},
		Hint:       resp.Hint,
		RawDetails: rawDetails,
	}
}

func (c *Client) newJSONRequest(ctx context.Context, method, rawURL string, reqBody any) (*http.Request, error) {
	var bodyReader io.Reader
	if reqBody != nil {
		body, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) doJSONRequest(ctx context.Context, method, rawURL string, reqBody any, respBody any) error {
	req, err := c.newJSONRequest(ctx, method, rawURL, reqBody)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if respBody == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respBody)
}

func (c *Client) doAPIRequest(ctx context.Context, method, rawURL string, reqBody any) (*apiResponseEnvelope, error) {
	req, err := c.newJSONRequest(ctx, method, rawURL, reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp apiResponseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	return &apiResp, nil
}

func decodeAPIResponseData(raw json.RawMessage, target any) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	return json.Unmarshal(raw, target)
}

// NewClient creates a new daemon client.
func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}

	return &Client{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}
}

// Health checks the daemon health.
func (c *Client) Health(ctx context.Context) (*daemon.HealthResponse, error) {
	var health daemon.HealthResponse
	if err := c.doJSONRequest(ctx, http.MethodGet, daemonBaseURL+"/health", nil, &health); err != nil {
		return nil, err
	}

	return &health, nil
}

// IsRunning checks if the daemon is running and healthy.
func (c *Client) IsRunning(ctx context.Context) bool {
	health, err := c.Health(ctx)
	return err == nil && health.OK
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

	var result daemon.ControlPlaneStartResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, daemonBaseURL+"/invocations/start_headless", req, &result); err != nil {
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

	var result daemon.ControlPlaneStartHeadedResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, daemonBaseURL+"/invocations/start_headed", req, &result); err != nil {
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

	u := fmt.Sprintf("http://daemon/invocations/%s/chat", url.PathEscape(invocationRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	var result daemon.ControlPlaneFollowUpPromptResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, u, reqBody, &result); err != nil {
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
func (c *Client) Stop(ctx context.Context, repoID, invocationID string) (*daemon.InvocationActionResponse, error) {
	var result daemon.InvocationActionResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, fmt.Sprintf("%s/invocations/%s/stop?repo_id=%s", daemonBaseURL, invocationID, repoID), nil, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Kill forcefully terminates an invocation.
func (c *Client) Kill(ctx context.Context, repoID, invocationID string) (*daemon.InvocationActionResponse, error) {
	var result daemon.InvocationActionResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, fmt.Sprintf("%s/invocations/%s/kill?repo_id=%s", daemonBaseURL, invocationID, repoID), nil, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Shutdown requests graceful daemon shutdown.
func (c *Client) Shutdown(ctx context.Context, force bool) (*daemon.ShutdownResponse, error) {
	url := daemonBaseURL + "/shutdown"
	if force {
		url += "?force=true"
	}
	var result daemon.ShutdownResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, url, nil, &result); err != nil {
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
	var result daemon.WorktreeCreateResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, daemonBaseURL+"/worktrees/create", req, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// WorktreeRm removes an integration worktree via the daemon.
func (c *Client) WorktreeRm(ctx context.Context, repoID, worktreeRef string, force bool) (*daemon.WorktreeRmResponse, error) {
	req := daemon.WorktreeRmRequest{Force: force}
	var result daemon.WorktreeRmResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, fmt.Sprintf("%s/worktrees/%s/rm?repo_id=%s", daemonBaseURL, url.PathEscape(worktreeRef), url.QueryEscape(repoID)), req, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ----- PR-08 Checkpoint Client Methods -----

// CheckpointApply applies a checkpoint to an invocation's sandbox.
func (c *Client) CheckpointApply(ctx context.Context, repoID, invocationID string, checkpointID int) (*daemon.CheckpointApplyResponse, error) {
	req := daemon.CheckpointApplyRequest{CheckpointID: checkpointID}
	var result daemon.CheckpointApplyResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, fmt.Sprintf("%s/invocations/%s/checkpoints/apply?repo_id=%s", daemonBaseURL, url.PathEscape(invocationID), url.QueryEscape(repoID)), req, &result); err != nil {
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

	u := fmt.Sprintf("%s/invocations/%s/restart", daemonBaseURL, url.PathEscape(invocationRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	var result daemon.RestartFromCheckpointResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, u, req, &result); err != nil {
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
	var result daemon.LandResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, fmt.Sprintf("%s/invocations/%s/land?repo_id=%s", daemonBaseURL, url.PathEscape(opts.InvocationID), url.QueryEscape(opts.RepoID)), req, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Discard discards a sandbox without landing via daemon.
func (c *Client) Discard(ctx context.Context, repoID, invocationID string) (*daemon.DiscardResponse, error) {
	req := daemon.DiscardRequest{}
	var result daemon.DiscardResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, fmt.Sprintf("%s/invocations/%s/discard?repo_id=%s", daemonBaseURL, url.PathEscape(invocationID), url.QueryEscape(repoID)), req, &result); err != nil {
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
	u := daemonBaseURL + "/worktrees?"
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

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var data daemon.ListWorktreesData
	if err := decodeAPIResponseData(apiResp.Data, &data); err != nil {
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

// GetWorktree gets a single worktree by reference via the daemon, preserving
// structured read errors for ambiguity and hint propagation.
func (c *Client) GetWorktree(ctx context.Context, ref string, repoID string) (*GetWorktreeResult, error) {
	u := fmt.Sprintf("%s/worktrees/%s", daemonBaseURL, url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, readAPIErrorRich(*apiResp)
	}

	var worktree daemon.WorktreeDTO
	if err := decodeAPIResponseData(apiResp.Data, &worktree); err != nil {
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
	u := daemonBaseURL + "/invocations?"
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

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var data daemon.ListInvocationsData
	if err := decodeAPIResponseData(apiResp.Data, &data); err != nil {
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

// GetInvocation gets a single invocation by reference via the daemon,
// preserving structured read errors for ambiguity and hint propagation.
func (c *Client) GetInvocation(ctx context.Context, ref string, repoID string) (*GetInvocationResult, error) {
	u := fmt.Sprintf("%s/invocations/%s", daemonBaseURL, url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, readAPIErrorRich(*apiResp)
	}

	var invocation daemon.InvocationDTO
	if err := decodeAPIResponseData(apiResp.Data, &invocation); err != nil {
		return nil, err
	}

	return &GetInvocationResult{
		Invocation: invocation,
		RequestID:  apiResp.RequestID,
	}, nil
}

// GetInvocationDiffOpts holds options for getting invocation diff.
type GetInvocationDiffOpts struct {
	IncludePatch       bool   // default true
	MaxPatchBytes      int    // default 2MB, max 5MB
	IncludeUncommitted bool   // default true
	TurnID             string // optional single turn selector
	TurnStartID        string // optional inclusive turn-range start selector
	TurnEndID          string // optional inclusive turn-range end selector
}

// GetInvocationDiffResult wraps the invocation diff response.
type GetInvocationDiffResult struct {
	Diff      daemon.InvocationDiffData
	RequestID string
}

// GetInvocationDiff gets the diff for an invocation via the daemon.
func (c *Client) GetInvocationDiff(ctx context.Context, ref string, repoID string, opts GetInvocationDiffOpts) (*GetInvocationDiffResult, error) {
	u := fmt.Sprintf("%s/invocations/%s/diff?", daemonBaseURL, url.PathEscape(ref))
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
	if opts.TurnID != "" {
		u += "turn=" + url.QueryEscape(opts.TurnID) + "&"
	}
	if opts.TurnStartID != "" {
		u += "turn_start=" + url.QueryEscape(opts.TurnStartID) + "&"
	}
	if opts.TurnEndID != "" {
		u += "turn_end=" + url.QueryEscape(opts.TurnEndID) + "&"
	}
	u = u[:len(u)-1] // trim trailing & or ?

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var diff daemon.InvocationDiffData
	if err := decodeAPIResponseData(apiResp.Data, &diff); err != nil {
		return nil, err
	}

	return &GetInvocationDiffResult{
		Diff:      diff,
		RequestID: apiResp.RequestID,
	}, nil
}

// GetInvocationReviewResult wraps the invocation review response.
type GetInvocationReviewResult struct {
	Review    daemon.InvocationReviewData
	RequestID string
}

// GetInvocationReview gets review/readiness data for an invocation via the daemon.
func (c *Client) GetInvocationReview(ctx context.Context, ref string, repoID string) (*GetInvocationReviewResult, error) {
	u := fmt.Sprintf("%s/invocations/%s/review", daemonBaseURL, url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var review daemon.InvocationReviewData
	if err := decodeAPIResponseData(apiResp.Data, &review); err != nil {
		return nil, err
	}

	return &GetInvocationReviewResult{
		Review:    review,
		RequestID: apiResp.RequestID,
	}, nil
}

// WorktreePRSyncOpts holds options for worktree-scoped PR sync.
type WorktreePRSyncOpts struct {
	AllowDirty     bool
	ForceWithLease bool
}

// WorktreePRSync performs worktree-scoped branch push + PR create/update via daemon.
func (c *Client) WorktreePRSync(ctx context.Context, worktreeRef, repoID string, opts WorktreePRSyncOpts) (*daemon.WorktreePRSyncResponse, error) {
	reqBody := daemon.WorktreePRSyncRequest{
		AllowDirty:     opts.AllowDirty,
		ForceWithLease: opts.ForceWithLease,
	}
	u := fmt.Sprintf("%s/worktrees/%s/pr/sync", daemonBaseURL, url.PathEscape(worktreeRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	var result daemon.WorktreePRSyncResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, u, reqBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// WorktreePRMergeOpts holds options for worktree-scoped merge.
type WorktreePRMergeOpts struct {
	Strategy         string
	ConfirmationMode string
	Confirmed        bool
	NoDeleteBranch   bool
}

// WorktreePRMerge performs worktree-scoped verify + merge via daemon.
func (c *Client) WorktreePRMerge(ctx context.Context, worktreeRef, repoID string, opts WorktreePRMergeOpts) (*daemon.WorktreePRMergeResponse, error) {
	reqBody := daemon.WorktreePRMergeRequest{
		Strategy:         opts.Strategy,
		ConfirmationMode: opts.ConfirmationMode,
		Confirmed:        opts.Confirmed,
		NoDeleteBranch:   opts.NoDeleteBranch,
	}
	u := fmt.Sprintf("%s/worktrees/%s/merge", daemonBaseURL, url.PathEscape(worktreeRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	var result daemon.WorktreePRMergeResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, u, reqBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// WorktreeUpdate performs worktree-scoped fetch + rebase via daemon.
func (c *Client) WorktreeUpdate(ctx context.Context, worktreeRef, repoID string) (*daemon.WorktreeUpdateResponse, error) {
	reqBody := daemon.WorktreeUpdateRequest{}
	u := fmt.Sprintf("%s/worktrees/%s/update", daemonBaseURL, url.PathEscape(worktreeRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	var result daemon.WorktreeUpdateResponse
	if err := c.doJSONRequest(ctx, http.MethodPost, u, reqBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetInvocationLogsOffsetOpts holds options for offset-based log reads.
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

// GetInvocationLogsOffset gets logs at a byte offset for an invocation via the daemon.
func (c *Client) GetInvocationLogsOffset(ctx context.Context, ref string, repoID string, opts GetInvocationLogsOffsetOpts) (*GetInvocationLogsOffsetResult, error) {
	u := fmt.Sprintf("%s/invocations/%s/logs?", daemonBaseURL, url.PathEscape(ref))
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

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var logs daemon.InvocationLogsOffsetData
	if err := decodeAPIResponseData(apiResp.Data, &logs); err != nil {
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
	Order  string // "asc" (default) or "desc"
}

// GetInvocationTimelineResult wraps the timeline response.
type GetInvocationTimelineResult struct {
	Entries    []daemon.TimelineEntryDTO
	NextCursor string
	RequestID  string
}

// GetInvocationTimeline gets the unified timeline for an invocation via daemon.
func (c *Client) GetInvocationTimeline(ctx context.Context, ref string, repoID string, opts GetInvocationTimelineOpts) (*GetInvocationTimelineResult, error) {
	u := fmt.Sprintf("%s/invocations/%s/timeline?", daemonBaseURL, url.PathEscape(ref))
	if repoID != "" {
		u += "repo_id=" + url.QueryEscape(repoID) + "&"
	}
	if opts.Limit > 0 {
		u += fmt.Sprintf("limit=%d&", opts.Limit)
	}
	if opts.Cursor != "" {
		u += "cursor=" + url.QueryEscape(opts.Cursor) + "&"
	}
	if opts.Order != "" {
		u += "order=" + url.QueryEscape(opts.Order) + "&"
	}
	u = u[:len(u)-1] // trim trailing & or ?

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var data daemon.InvocationTimelineData
	if err := decodeAPIResponseData(apiResp.Data, &data); err != nil {
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
	u := fmt.Sprintf("%s/invocations/%s/checkpoints?", daemonBaseURL, url.PathEscape(invocationRef))
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

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var data daemon.ListCheckpointsData
	if err := decodeAPIResponseData(apiResp.Data, &data); err != nil {
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

	apiResp, err := c.doAPIRequest(ctx, http.MethodPost, daemonBaseURL+"/repos/register", reqBody)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var data daemon.RepoRegisterData
	if err := decodeAPIResponseData(apiResp.Data, &data); err != nil {
		return nil, err
	}

	return &RegisterRepoResult{
		RepoID:                  data.RepoID,
		RepoKey:                 data.RepoKey,
		Paths:                   data.Paths,
		PreferredRoot:           data.PreferredRoot,
		PreferredRootAccessible: data.PreferredRootAccessible,
		LastSeenAt:              data.LastSeenAt,
	}, nil
}

// ListReposResult wraps the list repos response.
type ListReposResult struct {
	Repos     []daemon.RepoDTO
	RequestID string
}

// ListRepos lists all registered repos.
func (c *Client) ListRepos(ctx context.Context) (*ListReposResult, error) {
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, daemonBaseURL+"/repos", nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var data daemon.ListReposData
	if err := decodeAPIResponseData(apiResp.Data, &data); err != nil {
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
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s", daemonBaseURL, url.PathEscape(repoID)), nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var dto daemon.RepoDTO
	if err := decodeAPIResponseData(apiResp.Data, &dto); err != nil {
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
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, daemonBaseURL+"/spec/v2.1/s1/release/readiness?repo_id="+url.QueryEscape(repoID), nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var data daemon.S1ReleaseReadinessData
	if err := decodeAPIResponseData(apiResp.Data, &data); err != nil {
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
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, daemonBaseURL+"/spec/v2.1/s1/release/closure-report?repo_id="+url.QueryEscape(repoID), nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var data daemon.S1ClosureReportData
	if err := decodeAPIResponseData(apiResp.Data, &data); err != nil {
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
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, daemonBaseURL+"/spec/v2.1/s1/release/freeze-readiness?repo_id="+url.QueryEscape(repoID), nil)
	if err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, errors.New(errors.Code(apiResp.ErrorCode), apiResp.Message)
	}

	var data daemon.S1FreezeReadinessData
	if err := decodeAPIResponseData(apiResp.Data, &data); err != nil {
		return nil, err
	}

	return &S1FreezeReadinessResult{
		Data:      data,
		RequestID: apiResp.RequestID,
	}, nil
}
