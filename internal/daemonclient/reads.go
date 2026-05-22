package daemonclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

// ListWorktrees lists integration worktrees via the daemon.
func (c *Client) ListWorktrees(ctx context.Context, opts daemon.ListWorktreesParams) (*daemon.Result[daemon.ListWorktreesData], error) {
	q := url.Values{}
	if opts.RepoID != "" {
		q.Set("repo_id", opts.RepoID)
	}
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, queryURL("/worktrees", q), nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.ListWorktreesData](apiResp)
}

// GetWorktree gets a single worktree by reference within an optional canonical repo_id scope.
func (c *Client) GetWorktree(ctx context.Context, ref string, repoID string) (*daemon.Result[daemon.WorktreeDTO], error) {
	return getResult[daemon.WorktreeDTO](ctx, c, "/worktrees/"+url.PathEscape(ref), repoID)
}

// GetWorktreeMerge gets durable merge state for one worktree.
func (c *Client) GetWorktreeMerge(ctx context.Context, ref string, repoID string) (*daemon.Result[daemon.WorktreeMergeDTO], error) {
	return getResult[daemon.WorktreeMergeDTO](ctx, c, "/worktrees/"+url.PathEscape(ref)+"/pr/merge", repoID)
}

// ListInvocations lists invocations via the daemon.
func (c *Client) ListInvocations(ctx context.Context, opts daemon.ListInvocationsParams) (*daemon.Result[daemon.ListInvocationsData], error) {
	q := url.Values{}
	if opts.RepoID != "" {
		q.Set("repo_id", opts.RepoID)
	}
	if opts.WorktreeRef != "" {
		q.Set("worktree_ref", opts.WorktreeRef)
	}
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if opts.Mode != "" {
		q.Set("mode", opts.Mode)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, queryURL("/invocations", q), nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.ListInvocationsData](apiResp)
}

// GetInvocation gets a single invocation by reference within an optional canonical repo_id scope.
func (c *Client) GetInvocation(ctx context.Context, ref string, repoID string) (*daemon.Result[daemon.InvocationDTO], error) {
	return getResult[daemon.InvocationDTO](ctx, c, "/invocations/"+url.PathEscape(ref), repoID)
}

// GetInvocationSession gets headed tmux session facts for an invocation via the daemon.
func (c *Client) GetInvocationSession(ctx context.Context, ref string, repoID string) (*daemon.Result[daemon.InvocationSessionData], error) {
	return getResult[daemon.InvocationSessionData](ctx, c, "/invocations/"+url.PathEscape(ref)+"/session", repoID)
}

// GetInvocationDiff gets the diff for an invocation via the daemon.
func (c *Client) GetInvocationDiff(ctx context.Context, ref string, repoID string, opts daemon.GetDiffParams) (*daemon.Result[daemon.InvocationDiffData], error) {
	q := url.Values{}
	if repoID != "" {
		q.Set("repo_id", repoID)
	}
	if opts.ExcludePatch {
		q.Set("include_patch", "false")
	}
	if opts.MaxPatchBytes > 0 {
		q.Set("max_patch_bytes", strconv.Itoa(opts.MaxPatchBytes))
	}
	if opts.ExcludeUncommitted {
		q.Set("include_uncommitted", "false")
	}
	if opts.TurnID != "" {
		q.Set("turn", opts.TurnID)
	}
	if opts.TurnStartID != "" {
		q.Set("turn_start", opts.TurnStartID)
	}
	if opts.TurnEndID != "" {
		q.Set("turn_end", opts.TurnEndID)
	}
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, queryURL("/invocations/"+url.PathEscape(ref)+"/diff", q), nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.InvocationDiffData](apiResp)
}

// GetInvocationCheck gets check/readiness data for an invocation via the daemon.
func (c *Client) GetInvocationCheck(ctx context.Context, ref string, repoID string) (*daemon.Result[daemon.InvocationCheckData], error) {
	return getResult[daemon.InvocationCheckData](ctx, c, "/invocations/"+url.PathEscape(ref)+"/check", repoID)
}

// GetInvocationTimeline gets the unified timeline for an invocation via daemon.
func (c *Client) GetInvocationTimeline(ctx context.Context, ref string, repoID string, opts daemon.GetTimelineParams) (*daemon.Result[daemon.InvocationTimelineData], error) {
	q := url.Values{}
	if repoID != "" {
		q.Set("repo_id", repoID)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	if opts.Order != "" {
		q.Set("order", opts.Order)
	}
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, queryURL("/invocations/"+url.PathEscape(ref)+"/timeline", q), nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.InvocationTimelineData](apiResp)
}
