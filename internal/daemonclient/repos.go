package daemonclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

// RegisterRepo registers a repo root with the daemon and returns the resolved repo_id.
// This is the canonical way to get repo_id — CLI should not compute it locally.
func (c *Client) RegisterRepo(ctx context.Context, repoRoot string) (*daemon.Result[daemon.RepoRegisterData], error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	reqBody := daemon.RepoRegisterRequest{
		RepoRoot: repoRoot,
	}

	apiResp, err := c.doAPIRequest(ctx, http.MethodPost, daemonBaseURL+"/repos/register", reqBody)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.RepoRegisterData](apiResp)
}

// ListRepos lists all registered repos.
func (c *Client) ListRepos(ctx context.Context) (*daemon.Result[daemon.ListReposData], error) {
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, daemonBaseURL+"/repos", nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.ListReposData](apiResp)
}

// GetRepo gets a single repo by repo ref.
func (c *Client) GetRepo(ctx context.Context, repoRef string) (*daemon.Result[daemon.RepoDTO], error) {
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, daemonBaseURL+"/repos/"+url.PathEscape(repoRef), nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.RepoDTO](apiResp)
}

// RepoRm removes a registered repository via the daemon.
func (c *Client) RepoRm(ctx context.Context, repoRef string) (*daemon.Result[daemon.RepoRmData], error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	apiResp, err := c.doAPIRequest(ctx, http.MethodPost, daemonBaseURL+"/repos/rm", daemon.RepoRmRequest{RepoRef: repoRef})
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.RepoRmData](apiResp)
}
