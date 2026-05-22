package daemonclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/uuid"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

// TaskStart creates a task, integration worktree, and primary invocation.
func (c *Client) TaskStart(ctx context.Context, opts daemon.TaskStartRequest) (*daemon.TaskStartResponse, error) {
	if opts.ClientRequestID == "" {
		opts.ClientRequestID = uuid.New().String()
	}
	return postAction[daemon.TaskStartResponse](ctx, c, "/tasks/start", "", opts)
}

// ListTasks lists tasks for one repo.
func (c *Client) ListTasks(ctx context.Context, repoID string, all bool) (*daemon.Result[daemon.ListTasksData], error) {
	q := url.Values{"repo_id": []string{repoID}}
	if all {
		q.Set("all", strconv.FormatBool(true))
	}
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, queryURL("/tasks", q), nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.ListTasksData](apiResp)
}

// GetTask fetches one task by id/prefix/name.
func (c *Client) GetTask(ctx context.Context, taskRef, repoID string) (*daemon.Result[daemon.TaskDTO], error) {
	return getResult[daemon.TaskDTO](ctx, c, "/tasks/"+url.PathEscape(taskRef), repoID)
}

// ArchiveTask archives one task record.
func (c *Client) ArchiveTask(ctx context.Context, taskRef, repoID string) (*daemon.TaskStartResponse, error) {
	return postAction[daemon.TaskStartResponse](ctx, c, "/tasks/"+url.PathEscape(taskRef)+"/archive", repoID, nil)
}

// RetryTask starts a new primary invocation for an existing task worktree.
func (c *Client) RetryTask(ctx context.Context, taskRef, repoID string, opts daemon.TaskRetryRequest) (*daemon.TaskStartResponse, error) {
	if opts.ClientRequestID == "" {
		opts.ClientRequestID = uuid.New().String()
	}
	return postAction[daemon.TaskStartResponse](ctx, c, "/tasks/"+url.PathEscape(taskRef)+"/retry", repoID, opts)
}
