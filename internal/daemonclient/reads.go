package daemonclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

// ListWorktreesOpts holds options for listing worktrees.
type ListWorktreesOpts struct {
	RepoID string // optional, filter by canonical repo_id
	State  string // present, archived, all (default: present)
	Limit  int    // default 100, max 500
	Cursor string // opaque pagination cursor
}

// ListWorktrees lists integration worktrees via the daemon.
func (c *Client) ListWorktrees(ctx context.Context, opts ListWorktreesOpts) (*daemon.Result[daemon.ListWorktreesData], error) {
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
	u = u[:len(u)-1]

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.ListWorktreesData](apiResp)
}

// GetWorktree gets a single worktree by reference within an optional canonical repo_id scope.
func (c *Client) GetWorktree(ctx context.Context, ref string, repoID string) (*daemon.Result[daemon.WorktreeDTO], error) {
	u := fmt.Sprintf("%s/worktrees/%s", daemonBaseURL, url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.WorktreeDTO](apiResp)
}

// GetWorktreeMerge gets durable merge state for one worktree.
func (c *Client) GetWorktreeMerge(ctx context.Context, ref string, repoID string) (*daemon.Result[daemon.WorktreeMergeDTO], error) {
	u := fmt.Sprintf("%s/worktrees/%s/pr/merge", daemonBaseURL, url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.WorktreeMergeDTO](apiResp)
}

// ListInvocationsOpts holds options for listing invocations.
type ListInvocationsOpts struct {
	RepoID      string // optional, filter by canonical repo_id
	WorktreeRef string // optional, filter by worktree ref
	State       string // unresolved, finished, all (default: all)
	Mode        string // headed, headless, all (default: all)
	Limit       int    // default 100, max 500
	Cursor      string // opaque pagination cursor
}

// ListInvocations lists invocations via the daemon.
func (c *Client) ListInvocations(ctx context.Context, opts ListInvocationsOpts) (*daemon.Result[daemon.ListInvocationsData], error) {
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
	u = u[:len(u)-1]

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.ListInvocationsData](apiResp)
}

// GetInvocation gets a single invocation by reference within an optional canonical repo_id scope.
func (c *Client) GetInvocation(ctx context.Context, ref string, repoID string) (*daemon.Result[daemon.InvocationDTO], error) {
	u := fmt.Sprintf("%s/invocations/%s", daemonBaseURL, url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.InvocationDTO](apiResp)
}

// GetInvocationSession gets headed tmux session facts for an invocation via the daemon.
func (c *Client) GetInvocationSession(ctx context.Context, ref string, repoID string) (*daemon.Result[daemon.InvocationSessionData], error) {
	u := fmt.Sprintf("%s/invocations/%s/session", daemonBaseURL, url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.InvocationSessionData](apiResp)
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

// GetInvocationDiff gets the diff for an invocation via the daemon.
func (c *Client) GetInvocationDiff(ctx context.Context, ref string, repoID string, opts GetInvocationDiffOpts) (*daemon.Result[daemon.InvocationDiffData], error) {
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
	u = u[:len(u)-1]

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.InvocationDiffData](apiResp)
}

// GetInvocationCheck gets check/readiness data for an invocation via the daemon.
func (c *Client) GetInvocationCheck(ctx context.Context, ref string, repoID string) (*daemon.Result[daemon.InvocationCheckData], error) {
	u := fmt.Sprintf("%s/invocations/%s/check", daemonBaseURL, url.PathEscape(ref))
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.InvocationCheckData](apiResp)
}

// GetInvocationLogsOffsetOpts holds options for offset-based log reads.
type GetInvocationLogsOffsetOpts struct {
	Kind   string // raw, stderr, stream, hooks, terminal (default: raw)
	Offset int64  // byte offset from start of file
	Limit  int    // max bytes returned (default 65536)
}

// GetInvocationLogsOffset gets logs at a byte offset for an invocation via the daemon.
func (c *Client) GetInvocationLogsOffset(ctx context.Context, ref string, repoID string, opts GetInvocationLogsOffsetOpts) (*daemon.Result[daemon.InvocationLogsOffsetData], error) {
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
	return decodeResult[daemon.InvocationLogsOffsetData](apiResp)
}

// GetInvocationTimelineOpts holds options for timeline reads.
type GetInvocationTimelineOpts struct {
	Limit  int    // default 100, max 500
	Cursor string // opaque pagination cursor
	Order  string // "asc" (default) or "desc"
}

// GetInvocationTimeline gets the unified timeline for an invocation via daemon.
func (c *Client) GetInvocationTimeline(ctx context.Context, ref string, repoID string, opts GetInvocationTimelineOpts) (*daemon.Result[daemon.InvocationTimelineData], error) {
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
	u = u[:len(u)-1]

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.InvocationTimelineData](apiResp)
}

// ListCheckpointsOpts holds options for listing checkpoints.
type ListCheckpointsOpts struct {
	Limit  int    // default 100, max 500
	Cursor string // opaque pagination cursor
}

// ListCheckpoints lists checkpoints for an invocation via the daemon.
func (c *Client) ListCheckpoints(ctx context.Context, invocationRef string, repoID string, opts ListCheckpointsOpts) (*daemon.Result[daemon.ListCheckpointsData], error) {
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
	u = u[:len(u)-1]

	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.ListCheckpointsData](apiResp)
}
