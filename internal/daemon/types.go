// Package daemon implements the agency daemon supervisor for headless invocations.
package daemon

// APIVersion is the current API version. Incremented on breaking changes.
const APIVersion = 1

// StartHeadlessRequest is the request body for POST /invocations/{id}/start_headless.
type StartHeadlessRequest struct {
	// RepoID is the repo identifier.
	RepoID string `json:"repo_id"`

	// InvocationID is the invocation identifier (created by CLI).
	InvocationID string `json:"invocation_id"`

	// Runner is the runner type (claude, codex).
	Runner string `json:"runner"`

	// SandboxPath is the absolute path to the sandbox tree (runner CWD).
	SandboxPath string `json:"sandbox_path"`

	// Prompt is the full prompt text.
	Prompt string `json:"prompt"`

	// RunnerArgs are optional pass-through args appended after the base command.
	RunnerArgs []string `json:"runner_args,omitempty"`

	// Env are optional environment variable overrides.
	Env map[string]string `json:"env,omitempty"`
}

// StartHeadlessResponse is the response body for POST /invocations/{id}/start_headless.
type StartHeadlessResponse struct {
	OK               bool      `json:"ok"`
	PID              int       `json:"pid,omitempty"`
	PGID             int       `json:"pgid,omitempty"`
	DaemonInstanceID string    `json:"daemon_instance_id,omitempty"`
	AlreadyRunning   bool      `json:"already_running,omitempty"`
	Orphaned         bool      `json:"orphaned,omitempty"`
	LogPaths         *LogPaths `json:"log_paths,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// LogPaths contains paths to log files.
type LogPaths struct {
	Raw    string `json:"raw"`
	Stderr string `json:"stderr"`
}

// StopResponse is the response body for POST /invocations/{id}/stop.
type StopResponse struct {
	OK        bool   `json:"ok"`
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// KillResponse is the response body for POST /invocations/{id}/kill.
type KillResponse struct {
	OK        bool   `json:"ok"`
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// ShutdownResponse is the response body for POST /shutdown.
type ShutdownResponse struct {
	OK        bool   `json:"ok"`
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
	// RunningInvocations lists invocation IDs that are still running (when E_DAEMON_BUSY).
	RunningInvocations []string `json:"running_invocations,omitempty"`
}

// HealthResponse is the response body for GET /health.
type HealthResponse struct {
	OK               bool   `json:"ok"`
	APIVersion       int    `json:"api_version"`
	BuildVersion     string `json:"build_version"`
	GitSHA           string `json:"git_sha"`
	PID              int    `json:"pid"`
	DaemonInstanceID string `json:"daemon_instance_id"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
}

// ErrorResponse is a generic error response.
type ErrorResponse struct {
	OK        bool   `json:"ok"`
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
}

// SupervisedProcess holds runtime state for a supervised headless process.
type SupervisedProcess struct {
	InvocationID string
	RepoID       string
	PID          int
	PGID         int
	RawLogFile   string
	StderrFile   string

	// lastOutputAt is updated in-memory on every chunk; persisted with throttling.
	lastOutputAt int64

	// done channel is closed when the process exits.
	done chan struct{}
}
