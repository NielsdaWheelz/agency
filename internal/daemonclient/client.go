// Package daemonclient provides a client for communicating with the agency daemon.
package daemonclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

const daemonBaseURL = "http://daemon"

// Client communicates with the agency daemon over Unix socket.
type Client struct {
	httpClient *http.Client
}

type rawAPIResponse struct {
	OK           bool            `json:"ok"`
	APIVersion   int             `json:"api_version"`
	BuildVersion string          `json:"build_version,omitempty"`
	GitSHA       string          `json:"git_sha,omitempty"`
	RequestID    string          `json:"request_id,omitempty"`
	Data         json.RawMessage `json:"data,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	Message      string          `json:"message,omitempty"`
	Hint         string          `json:"hint,omitempty"`
	Details      json.RawMessage `json:"details,omitempty"`
}

// DaemonReadError carries the full daemon read API error envelope for consumers
// that need structured details (e.g., ambiguity candidates).
// It wraps an AgencyError so errors.GetCode and errors.AsAgencyError still work.
type DaemonReadError struct {
	agencyErr  *errors.AgencyError
	rawDetails json.RawMessage
}

func (e *DaemonReadError) Error() string { return e.agencyErr.Error() }
func (e *DaemonReadError) Unwrap() error { return e.agencyErr }

// DaemonActionError carries a daemon mutation/control-plane failure envelope.
// It wraps an AgencyError so common error helpers still work, while preserving
// the raw response for command-specific details such as conflict files.
type DaemonActionError struct {
	agencyErr   *errors.AgencyError
	rawResponse json.RawMessage
}

func (e *DaemonActionError) Error() string { return e.agencyErr.Error() }
func (e *DaemonActionError) Unwrap() error { return e.agencyErr }

// DecodeResponse unmarshals the raw daemon error response into v.
func (e *DaemonActionError) DecodeResponse(v any) error {
	if len(e.rawResponse) == 0 {
		return io.EOF
	}
	return json.Unmarshal(e.rawResponse, v)
}

// Candidates extracts candidate strings from the raw details when they
// contain a "candidates" array (e.g., daemon AmbiguousDetails).
func (e *DaemonReadError) Candidates() []string {
	if len(e.rawDetails) == 0 {
		return nil
	}
	var ad daemon.AmbiguousDetails
	if err := json.Unmarshal(e.rawDetails, &ad); err != nil {
		return nil
	}
	return ad.Candidates
}

func (c *Client) doHTTPRequest(ctx context.Context, method, rawURL string, reqBody any) (*http.Response, error) {
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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	return resp, nil
}

func (c *Client) doJSONRequest(ctx context.Context, method, rawURL string, reqBody any, respBody any) error {
	resp, err := c.doHTTPRequest(ctx, method, rawURL, reqBody)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if respBody == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respBody)
}

func (c *Client) doActionRequest(ctx context.Context, method, rawURL string, reqBody any, respBody any) error {
	resp, err := c.doHTTPRequest(ctx, method, rawURL, reqBody)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var envelope struct {
		OK        bool   `json:"ok"`
		ErrorCode string `json:"error_code,omitempty"`
		Message   string `json:"message,omitempty"`
		Hint      string `json:"hint,omitempty"`
		RequestID string `json:"request_id,omitempty"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	if !envelope.OK {
		code := errors.Code(envelope.ErrorCode)
		if code == "" {
			code = errors.EInternal
		}
		message := envelope.Message
		if message == "" {
			message = "daemon request failed"
		}
		details := map[string]string{}
		if envelope.Hint != "" {
			details["hint"] = envelope.Hint
		}
		if envelope.RequestID != "" {
			details["request_id"] = envelope.RequestID
		}
		return &DaemonActionError{
			agencyErr: &errors.AgencyError{
				Code:    code,
				Msg:     message,
				Details: details,
			},
			rawResponse: append(json.RawMessage(nil), body...),
		}
	}
	if respBody == nil {
		return nil
	}
	return json.Unmarshal(body, respBody)
}

func (c *Client) doAPIRequest(ctx context.Context, method, rawURL string, reqBody any) (*rawAPIResponse, error) {
	resp, err := c.doHTTPRequest(ctx, method, rawURL, reqBody)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp rawAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	return &apiResp, nil
}

// postAction is the canonical POST-mutation helper used across this package:
// checks API version, builds the URL with an optional ?repo_id= query, posts
// body, decodes into T. repoID is "" for endpoints that don't take it.
func postAction[T any](ctx context.Context, c *Client, urlPath, repoID string, body any) (*T, error) {
	if err := c.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	u := daemonBaseURL + urlPath
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	var result T
	if err := c.doActionRequest(ctx, http.MethodPost, u, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// getResult is the canonical GET-and-decode helper for read endpoints whose
// only query parameter is an optional repo_id. Endpoints taking more
// parameters build url.Values directly and call doAPIRequest themselves.
func getResult[T any](ctx context.Context, c *Client, urlPath, repoID string) (*daemon.Result[T], error) {
	u := daemonBaseURL + urlPath
	if repoID != "" {
		u += "?repo_id=" + url.QueryEscape(repoID)
	}
	apiResp, err := c.doAPIRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return decodeResult[T](apiResp)
}

// queryURL builds urlPath?<encoded query>. If q has no entries the "?" is omitted.
func queryURL(urlPath string, q url.Values) string {
	if encoded := q.Encode(); encoded != "" {
		return daemonBaseURL + urlPath + "?" + encoded
	}
	return daemonBaseURL + urlPath
}

func decodeResult[T any](apiResp *rawAPIResponse) (*daemon.Result[T], error) {
	if !apiResp.OK {
		details := map[string]string{}
		if apiResp.Hint != "" {
			details["hint"] = apiResp.Hint
		}
		if apiResp.RequestID != "" {
			details["request_id"] = apiResp.RequestID
		}
		return nil, &DaemonReadError{
			agencyErr: &errors.AgencyError{
				Code:    errors.Code(apiResp.ErrorCode),
				Msg:     apiResp.Message,
				Details: details,
			},
			rawDetails: append(json.RawMessage(nil), apiResp.Details...),
		}
	}

	var data T
	if len(apiResp.Data) != 0 && !bytes.Equal(apiResp.Data, []byte("null")) {
		if err := json.Unmarshal(apiResp.Data, &data); err != nil {
			return nil, err
		}
	}

	return &daemon.Result[T]{
		Data:      data,
		RequestID: apiResp.RequestID,
	}, nil
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

// CheckAPIVersion checks if the daemon API version matches the client.
// Returns nil if versions match, error otherwise.
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

		wait = wait * 2
		if wait > 500*time.Millisecond {
			wait = 500 * time.Millisecond
		}
	}

	return errors.New(errors.EDaemonNotRunning, "daemon did not become ready within timeout")
}
