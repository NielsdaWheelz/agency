package daemonclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

const (
	defaultDrainPageLimit = 500
	defaultLogDrainLimit  = 65536
)

// DrainWorktrees drains all worktree list pages for the supplied filters.
func (c *Client) DrainWorktrees(ctx context.Context, opts daemon.ListWorktreesParams) ([]daemon.WorktreeDTO, error) {
	pageLimit, err := validateDrainPageLimit(opts.Limit)
	if err != nil {
		return nil, err
	}

	worktrees := make([]daemon.WorktreeDTO, 0, 128)
	cursor := opts.Cursor
	seenCursors := seenDrainCursors(cursor)
	for {
		result, err := c.ListWorktrees(ctx, daemon.ListWorktreesParams{
			RepoID: opts.RepoID,
			State:  opts.State,
			Limit:  pageLimit,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}

		worktrees = append(worktrees, result.Data.Worktrees...)
		if result.Data.NextCursor == "" {
			return worktrees, nil
		}
		if _, seen := seenCursors[result.Data.NextCursor]; seen {
			return nil, errors.New(errors.EInternal, "worktree pagination cursor did not advance")
		}
		seenCursors[result.Data.NextCursor] = struct{}{}
		cursor = result.Data.NextCursor
	}
}

// DrainInvocations drains all invocation list pages for the supplied filters.
func (c *Client) DrainInvocations(ctx context.Context, opts daemon.ListInvocationsParams) ([]daemon.InvocationDTO, error) {
	pageLimit, err := validateDrainPageLimit(opts.Limit)
	if err != nil {
		return nil, err
	}

	invocations := make([]daemon.InvocationDTO, 0, 128)
	cursor := opts.Cursor
	seenCursors := seenDrainCursors(cursor)
	for {
		result, err := c.ListInvocations(ctx, daemon.ListInvocationsParams{
			RepoID:      opts.RepoID,
			WorktreeRef: opts.WorktreeRef,
			State:       opts.State,
			Mode:        opts.Mode,
			Limit:       pageLimit,
			Cursor:      cursor,
		})
		if err != nil {
			return nil, err
		}

		invocations = append(invocations, result.Data.Invocations...)
		if result.Data.NextCursor == "" {
			return invocations, nil
		}
		if _, seen := seenCursors[result.Data.NextCursor]; seen {
			return nil, errors.New(errors.EInternal, "invocation pagination cursor did not advance")
		}
		seenCursors[result.Data.NextCursor] = struct{}{}
		cursor = result.Data.NextCursor
	}
}

// DrainInvocationTimeline drains all timeline pages for one invocation.
func (c *Client) DrainInvocationTimeline(ctx context.Context, invocationRef, repoID string, opts daemon.GetTimelineParams) ([]daemon.TimelineEntryDTO, error) {
	pageLimit, err := validateDrainPageLimit(opts.Limit)
	if err != nil {
		return nil, err
	}
	order := strings.TrimSpace(opts.Order)
	if order == "desc" {
		return nil, errors.NewWithDetails(
			errors.EInvalidArgument,
			"timeline drain does not support descending order",
			map[string]string{"param": "order"},
		)
	}
	if order != "" && order != "asc" {
		return nil, errors.NewWithDetails(
			errors.EInvalidArgument,
			"timeline order must be asc",
			map[string]string{"param": "order"},
		)
	}

	entries := make([]daemon.TimelineEntryDTO, 0, 128)
	cursor := opts.Cursor
	seenCursors := seenDrainCursors(cursor)
	for {
		result, err := c.GetInvocationTimeline(ctx, invocationRef, repoID, daemon.GetTimelineParams{
			Limit:  pageLimit,
			Cursor: cursor,
			Order:  order,
		})
		if err != nil {
			return nil, err
		}

		entries = append(entries, result.Data.Entries...)
		if result.Data.NextCursor == "" {
			return entries, nil
		}
		if _, seen := seenCursors[result.Data.NextCursor]; seen {
			return nil, errors.New(errors.EInternal, "timeline pagination cursor did not advance")
		}
		seenCursors[result.Data.NextCursor] = struct{}{}
		cursor = result.Data.NextCursor
	}
}

// DrainInvocationCheckpoints drains all checkpoint pages for one invocation.
func (c *Client) DrainInvocationCheckpoints(ctx context.Context, invocationRef, repoID string, opts daemon.ListCheckpointsParams) ([]daemon.CheckpointDTO, error) {
	pageLimit, err := validateDrainPageLimit(opts.Limit)
	if err != nil {
		return nil, err
	}

	checkpoints := make([]daemon.CheckpointDTO, 0, 32)
	cursor := opts.Cursor
	seenCursors := seenDrainCursors(cursor)
	for {
		u := fmt.Sprintf("%s/invocations/%s/checkpoints?", daemonBaseURL, url.PathEscape(invocationRef))
		if repoID != "" {
			u += "repo_id=" + url.QueryEscape(repoID) + "&"
		}
		u += fmt.Sprintf("limit=%d&", pageLimit)
		if cursor != "" {
			u += "cursor=" + url.QueryEscape(cursor) + "&"
		}
		u = strings.TrimSuffix(u, "&")

		apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		result, err := decodeResult[daemon.ListCheckpointsData](apiResp)
		if err != nil {
			return nil, err
		}

		checkpoints = append(checkpoints, result.Data.Checkpoints...)
		if result.Data.NextCursor == "" {
			return checkpoints, nil
		}
		if _, seen := seenCursors[result.Data.NextCursor]; seen {
			return nil, errors.New(errors.EInternal, "checkpoint pagination cursor did not advance")
		}
		seenCursors[result.Data.NextCursor] = struct{}{}
		cursor = result.Data.NextCursor
	}
}

// DrainInvocationLogs drains offset-mode log chunks for one invocation into dst
// and returns the next offset to use when following the log.
func (c *Client) DrainInvocationLogs(ctx context.Context, invocationRef, repoID string, opts daemon.GetLogsParams, dst io.Writer) (int64, error) {
	if opts.Offset < 0 {
		return 0, errors.NewWithDetails(
			errors.EInvalidArgument,
			"log offset must be non-negative",
			map[string]string{"param": "offset"},
		)
	}
	limit := opts.Limit
	if limit == 0 {
		limit = defaultLogDrainLimit
	}
	if limit < 0 || limit > daemon.MaxLogChunk {
		return 0, errors.NewWithDetails(
			errors.EInvalidArgument,
			"log drain limit must be between 1 and daemon.MaxLogChunk",
			map[string]string{"param": "limit"},
		)
	}

	offset := opts.Offset
	for {
		u := fmt.Sprintf("%s/invocations/%s/logs?", daemonBaseURL, url.PathEscape(invocationRef))
		if repoID != "" {
			u += "repo_id=" + url.QueryEscape(repoID) + "&"
		}
		if opts.Kind != "" {
			u += "kind=" + url.QueryEscape(opts.Kind) + "&"
		}
		u += fmt.Sprintf("offset=%d&limit=%d", offset, limit)

		apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
		if err != nil {
			return offset, err
		}
		result, err := decodeResult[daemon.InvocationLogsOffsetData](apiResp)
		if err != nil {
			return offset, err
		}

		if result.Data.NextOffset < offset ||
			(result.Data.NextOffset == offset && result.Data.TotalBytes > offset) {
			return offset, errors.New(errors.EInternal, "log pagination offset did not advance")
		}

		dataB64 := strings.TrimSpace(result.Data.DataB64)
		if dataB64 != "" {
			decoded, decErr := base64.StdEncoding.DecodeString(dataB64)
			if decErr != nil {
				return offset, errors.Wrap(errors.EInternal, "failed to decode log data", decErr)
			}
			if _, writeErr := dst.Write(decoded); writeErr != nil {
				return offset, errors.Wrap(errors.EInternal, "failed to write log data", writeErr)
			}
		}

		offset = result.Data.NextOffset
		if offset >= result.Data.TotalBytes {
			return offset, nil
		}
	}
}

func validateDrainPageLimit(limit int) (int, error) {
	if limit == 0 {
		return defaultDrainPageLimit, nil
	}
	if limit < 0 || limit > 500 {
		return 0, errors.NewWithDetails(
			errors.EInvalidArgument,
			"drain page limit must be between 1 and 500",
			map[string]string{"param": "limit"},
		)
	}
	return limit, nil
}

func seenDrainCursors(cursor string) map[string]struct{} {
	seen := map[string]struct{}{}
	if cursor != "" {
		seen[cursor] = struct{}{}
	}
	return seen
}
