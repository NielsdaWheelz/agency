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

// GetRepo gets a single repo by ID.
func (c *Client) GetRepo(ctx context.Context, repoID string) (*daemon.Result[daemon.RepoDTO], error) {
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, daemonBaseURL+"/repos/"+url.PathEscape(repoID), nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.RepoDTO](apiResp)
}

// GetS1ReleaseReadiness queries the daemon for S1 release readiness.
func (c *Client) GetS1ReleaseReadiness(ctx context.Context, repoID string) (*daemon.Result[daemon.S1ReleaseReadinessData], error) {
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, daemonBaseURL+"/spec/v2.1/s1/release/readiness?repo_id="+url.QueryEscape(repoID), nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.S1ReleaseReadinessData](apiResp)
}

// GetS1ClosureReport queries the daemon for the S1 closure report.
func (c *Client) GetS1ClosureReport(ctx context.Context, repoID string) (*daemon.Result[daemon.S1ClosureReportData], error) {
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, daemonBaseURL+"/spec/v2.1/s1/release/closure-report?repo_id="+url.QueryEscape(repoID), nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.S1ClosureReportData](apiResp)
}

// GetS1FreezeReadiness queries the daemon for S1 freeze readiness.
func (c *Client) GetS1FreezeReadiness(ctx context.Context, repoID string) (*daemon.Result[daemon.S1FreezeReadinessData], error) {
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, daemonBaseURL+"/spec/v2.1/s1/release/freeze-readiness?repo_id="+url.QueryEscape(repoID), nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[daemon.S1FreezeReadinessData](apiResp)
}
