package daemonclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

// WorktreeCreate creates an integration worktree via the daemon.
func (c *Client) WorktreeCreate(ctx context.Context, opts daemon.WorktreeCreateRequest) (*daemon.WorktreeCreateResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	var result daemon.WorktreeCreateResponse
	if err := c.doActionRequest(ctx, http.MethodPost, daemonBaseURL+"/worktrees/create", opts, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// WorktreeRm removes an integration worktree via the daemon.
func (c *Client) WorktreeRm(ctx context.Context, repoID, worktreeRef string, req daemon.WorktreeRmRequest) (*daemon.WorktreeRmResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	var result daemon.WorktreeRmResponse
	if err := c.doActionRequest(ctx, http.MethodPost, fmt.Sprintf("%s/worktrees/%s/rm?repo_id=%s", daemonBaseURL, url.PathEscape(worktreeRef), url.QueryEscape(repoID)), req, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// CheckpointApply applies a checkpoint to an invocation's sandbox.
func (c *Client) CheckpointApply(ctx context.Context, repoID, invocationID string, checkpointID int) (*daemon.CheckpointApplyResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	req := daemon.CheckpointApplyRequest{CheckpointID: checkpointID}
	var result daemon.CheckpointApplyResponse
	if err := c.doActionRequest(ctx, http.MethodPost, fmt.Sprintf("%s/invocations/%s/checkpoints/apply?repo_id=%s", daemonBaseURL, url.PathEscape(invocationID), url.QueryEscape(repoID)), req, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// RecreateHeaded starts a new tmux session for an existing headed invocation.
func (c *Client) RecreateHeaded(ctx context.Context, invocationRef, repoID string) (*daemon.ControlPlaneStartHeadedResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/invocations/%s/recreate", daemonBaseURL, url.PathEscape(invocationRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	var result daemon.ControlPlaneStartHeadedResponse
	if err := c.doActionRequest(ctx, http.MethodPost, u, nil, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Land lands sandbox changes to the integration worktree via daemon.
func (c *Client) Land(ctx context.Context, invocationRef, repoID string, req daemon.LandRequest) (*daemon.LandResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/invocations/%s/land", daemonBaseURL, url.PathEscape(invocationRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	var result daemon.LandResponse
	if err := c.doActionRequest(ctx, http.MethodPost, u, req, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Discard discards a sandbox without landing via daemon.
func (c *Client) Discard(ctx context.Context, repoID, invocationID string) (*daemon.DiscardResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	var result daemon.DiscardResponse
	if err := c.doActionRequest(ctx, http.MethodPost, fmt.Sprintf("%s/invocations/%s/discard?repo_id=%s", daemonBaseURL, url.PathEscape(invocationID), url.QueryEscape(repoID)), nil, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// WorktreePRSync performs worktree-scoped branch push + PR create/update via daemon.
func (c *Client) WorktreePRSync(ctx context.Context, worktreeRef, repoID string, opts daemon.WorktreePRSyncRequest) (*daemon.WorktreePRSyncResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/worktrees/%s/pr/sync", daemonBaseURL, url.PathEscape(worktreeRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	var result daemon.WorktreePRSyncResponse
	if err := c.doActionRequest(ctx, http.MethodPost, u, opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// WorktreePRMerge performs worktree-scoped verify + merge via daemon.
func (c *Client) WorktreePRMerge(ctx context.Context, worktreeRef, repoID string, opts daemon.WorktreePRMergeRequest) (*daemon.WorktreePRMergeResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/worktrees/%s/pr/merge", daemonBaseURL, url.PathEscape(worktreeRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	var result daemon.WorktreePRMergeResponse
	if err := c.doActionRequest(ctx, http.MethodPost, u, opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// WorktreeRebase performs worktree-scoped fetch + rebase via daemon.
func (c *Client) WorktreeRebase(ctx context.Context, worktreeRef, repoID string) (*daemon.WorktreeRebaseResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/worktrees/%s/rebase", daemonBaseURL, url.PathEscape(worktreeRef))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	var result daemon.WorktreeRebaseResponse
	if err := c.doActionRequest(ctx, http.MethodPost, u, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
