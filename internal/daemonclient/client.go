// Package daemonclient provides a client for communicating with the agency daemon.
package daemonclient

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

const daemonBaseURL = "http://daemon"

// Client communicates with the agency daemon over Unix socket.
type Client struct {
	httpClient *http.Client
}

// DaemonReadError carries the full daemon read API error envelope for consumers
// that need hint and structured details (e.g., ambiguity candidates).
// It wraps an AgencyError so errors.GetCode and errors.AsAgencyError still work.
type DaemonReadError struct {
	AgencyErr  *errors.AgencyError
	Hint       string
	RawDetails json.RawMessage
}

func (e *DaemonReadError) Error() string { return e.AgencyErr.Error() }
func (e *DaemonReadError) Unwrap() error { return e.AgencyErr }

// DaemonActionError carries a daemon mutation/control-plane failure envelope.
// It wraps an AgencyError so common error helpers still work, while preserving
// the raw response for command-specific details such as conflict files.
type DaemonActionError struct {
	AgencyErr   *errors.AgencyError
	RequestID   string
	RawResponse json.RawMessage
}

func (e *DaemonActionError) Error() string { return e.AgencyErr.Error() }
func (e *DaemonActionError) Unwrap() error { return e.AgencyErr }

// DecodeResponse unmarshals the raw daemon error response into v.
func (e *DaemonActionError) DecodeResponse(v any) error {
	if len(e.RawResponse) == 0 {
		return io.EOF
	}
	return json.Unmarshal(e.RawResponse, v)
}

// Candidates extracts candidate strings from RawDetails when the details
// contain a "candidates" array (e.g., daemon AmbiguousDetails).
func (e *DaemonReadError) Candidates() []string {
	if len(e.RawDetails) == 0 {
		return nil
	}
	var ad struct {
		Candidates []string `json:"candidates"`
	}
	if err := json.Unmarshal(e.RawDetails, &ad); err != nil {
		return nil
	}
	return ad.Candidates
}

// AsDaemonReadError extracts a DaemonReadError from an error chain.
func AsDaemonReadError(err error) (*DaemonReadError, bool) {
	var dre *DaemonReadError
	if stderrors.As(err, &dre) {
		return dre, true
	}
	return nil, false
}

// AsDaemonActionError extracts a DaemonActionError from an error chain.
func AsDaemonActionError(err error) (*DaemonActionError, bool) {
	var dae *DaemonActionError
	if stderrors.As(err, &dae) {
		return dae, true
	}
	return nil, false
}

func (c *Client) newJSONRequest(ctx context.Context, method, rawURL string, reqBody any) (*http.Request, error) {
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
	return req, nil
}

func (c *Client) doJSONRequest(ctx context.Context, method, rawURL string, reqBody any, respBody any) error {
	req, err := c.newJSONRequest(ctx, method, rawURL, reqBody)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if respBody == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respBody)
}

func (c *Client) doActionRequest(ctx context.Context, method, rawURL string, reqBody any, respBody any) error {
	req, err := c.newJSONRequest(ctx, method, rawURL, reqBody)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
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
			AgencyErr: &errors.AgencyError{
				Code:    code,
				Msg:     message,
				Details: details,
			},
			RequestID:   envelope.RequestID,
			RawResponse: append(json.RawMessage(nil), body...),
		}
	}
	if respBody == nil {
		return nil
	}
	return json.Unmarshal(body, respBody)
}

func (c *Client) doAPIRequest(ctx context.Context, method, rawURL string, reqBody any) (*daemon.RawAPIResponse, error) {
	req, err := c.newJSONRequest(ctx, method, rawURL, reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(errors.EDaemonConnectionFailed, "failed to connect to daemon", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var apiResp daemon.RawAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	return &apiResp, nil
}

func decodeResult[T any](apiResp *daemon.RawAPIResponse) (*daemon.Result[T], error) {
	if !apiResp.OK {
		details := map[string]string{}
		if apiResp.Hint != "" {
			details["hint"] = apiResp.Hint
		}
		if apiResp.RequestID != "" {
			details["request_id"] = apiResp.RequestID
		}
		return nil, &DaemonReadError{
			AgencyErr: &errors.AgencyError{
				Code:    errors.Code(apiResp.ErrorCode),
				Msg:     apiResp.Message,
				Details: details,
			},
			Hint:       apiResp.Hint,
			RawDetails: append(json.RawMessage(nil), apiResp.Details...),
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

// CheckAPIVersion checks if the daemon API version is compatible with the client.
// Returns nil if compatible, error otherwise.
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
