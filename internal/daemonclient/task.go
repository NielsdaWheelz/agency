package daemonclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/google/uuid"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

// TaskStart creates a task, integration worktree, and primary invocation.
func (c *Client) TaskStart(ctx context.Context, opts daemon.TaskStartRequest) (*daemon.TaskStartResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	if opts.ClientRequestID == "" {
		opts.ClientRequestID = uuid.New().String()
	}
	var result daemon.TaskStartResponse
	if err := c.doActionRequest(ctx, http.MethodPost, daemonBaseURL+"/tasks/start", opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTasks lists tasks for one repo.
func (c *Client) ListTasks(ctx context.Context, repoID string, all bool) (*daemon.Result[daemon.ListTasksData], error) {
	u := daemonBaseURL + "/tasks?repo_id=" + url.QueryEscape(repoID)
	if all {
		u += "&all=true"
	}
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.ListTasksData](apiResp)
}

// GetTask fetches one task by id/prefix/name.
func (c *Client) GetTask(ctx context.Context, taskRef, repoID string) (*daemon.Result[daemon.TaskDTO], error) {
	u := fmt.Sprintf("%s/tasks/%s?repo_id=%s", daemonBaseURL, url.PathEscape(taskRef), url.QueryEscape(repoID))
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.TaskDTO](apiResp)
}

// ArchiveTask archives one task record.
func (c *Client) ArchiveTask(ctx context.Context, taskRef, repoID string) (*daemon.TaskStartResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	var result daemon.TaskStartResponse
	u := fmt.Sprintf("%s/tasks/%s/archive?repo_id=%s", daemonBaseURL, url.PathEscape(taskRef), url.QueryEscape(repoID))
	if err := c.doActionRequest(ctx, http.MethodPost, u, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RetryTask starts a new primary invocation for an existing task worktree.
func (c *Client) RetryTask(ctx context.Context, taskRef, repoID string, opts daemon.TaskRetryRequest) (*daemon.TaskStartResponse, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	if opts.ClientRequestID == "" {
		opts.ClientRequestID = uuid.New().String()
	}
	var result daemon.TaskStartResponse
	u := fmt.Sprintf("%s/tasks/%s/retry?repo_id=%s", daemonBaseURL, url.PathEscape(taskRef), url.QueryEscape(repoID))
	if err := c.doActionRequest(ctx, http.MethodPost, u, opts, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
