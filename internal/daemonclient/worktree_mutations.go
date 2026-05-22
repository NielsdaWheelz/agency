package daemonclient

import (
	"context"
	"net/url"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

// WorktreeCreate creates an integration worktree via the daemon.
func (c *Client) WorktreeCreate(ctx context.Context, opts daemon.WorktreeCreateRequest) (*daemon.WorktreeCreateResponse, error) {
	return postAction[daemon.WorktreeCreateResponse](ctx, c, "/worktrees/create", "", opts)
}

// WorktreeRm removes an integration worktree via the daemon.
func (c *Client) WorktreeRm(ctx context.Context, repoID, worktreeRef string, req daemon.WorktreeRmRequest) (*daemon.WorktreeRmResponse, error) {
	return postAction[daemon.WorktreeRmResponse](ctx, c, "/worktrees/"+url.PathEscape(worktreeRef)+"/rm", repoID, req)
}

// CheckpointApply applies a checkpoint to an invocation's sandbox.
func (c *Client) CheckpointApply(ctx context.Context, repoID, invocationID string, checkpointID int) (*daemon.CheckpointApplyResponse, error) {
	req := daemon.CheckpointApplyRequest{CheckpointID: checkpointID}
	return postAction[daemon.CheckpointApplyResponse](ctx, c, "/invocations/"+url.PathEscape(invocationID)+"/checkpoints/apply", repoID, req)
}

// RecreateHeaded starts a new tmux session for an existing headed invocation.
func (c *Client) RecreateHeaded(ctx context.Context, invocationRef, repoID string) (*daemon.ControlPlaneStartHeadedResponse, error) {
	return postAction[daemon.ControlPlaneStartHeadedResponse](ctx, c, "/invocations/"+url.PathEscape(invocationRef)+"/recreate", repoID, nil)
}

// Land lands sandbox changes to the integration worktree via daemon.
func (c *Client) Land(ctx context.Context, invocationRef, repoID string, req daemon.LandRequest) (*daemon.LandResponse, error) {
	return postAction[daemon.LandResponse](ctx, c, "/invocations/"+url.PathEscape(invocationRef)+"/land", repoID, req)
}

// Discard discards a sandbox without landing via daemon.
func (c *Client) Discard(ctx context.Context, repoID, invocationID string) (*daemon.DiscardResponse, error) {
	return postAction[daemon.DiscardResponse](ctx, c, "/invocations/"+url.PathEscape(invocationID)+"/discard", repoID, nil)
}

// WorktreePRSync performs worktree-scoped branch push + PR create/update via daemon.
func (c *Client) WorktreePRSync(ctx context.Context, worktreeRef, repoID string, opts daemon.WorktreePRSyncRequest) (*daemon.WorktreePRSyncResponse, error) {
	return postAction[daemon.WorktreePRSyncResponse](ctx, c, "/worktrees/"+url.PathEscape(worktreeRef)+"/pr/sync", repoID, opts)
}

// WorktreePRMerge performs worktree-scoped verify + merge via daemon.
func (c *Client) WorktreePRMerge(ctx context.Context, worktreeRef, repoID string, opts daemon.WorktreePRMergeRequest) (*daemon.WorktreePRMergeResponse, error) {
	return postAction[daemon.WorktreePRMergeResponse](ctx, c, "/worktrees/"+url.PathEscape(worktreeRef)+"/pr/merge", repoID, opts)
}

// WorktreeRebase performs worktree-scoped fetch + rebase via daemon.
func (c *Client) WorktreeRebase(ctx context.Context, worktreeRef, repoID string) (*daemon.WorktreeRebaseResponse, error) {
	return postAction[daemon.WorktreeRebaseResponse](ctx, c, "/worktrees/"+url.PathEscape(worktreeRef)+"/rebase", repoID, nil)
}
