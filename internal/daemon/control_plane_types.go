package daemon

import "encoding/json"

// ControlPlaneStartRequest is the request body for POST /invocations/start_headless and /invocations/start_headed.
type ControlPlaneStartRequest struct {
	// RepoRoot is the absolute path to the repository root.
	RepoRoot string `json:"repo_root"`

	// WorktreeRef is the integration worktree reference (name, id, or prefix).
	WorktreeRef string `json:"worktree_ref"`

	// Runner is the canonical runner id (claude-code, codex, amp, opencode, cursor, droid).
	Runner string `json:"runner"`

	// Prompt is the full prompt text (max 256KB).
	Prompt string `json:"prompt"`

	// InvocationName is an optional human-readable label.
	InvocationName string `json:"invocation_name,omitempty"`

	// RunnerArgs are optional pass-through args appended after the base command.
	RunnerArgs []string `json:"runner_args,omitempty"`

	// Env are optional environment variable overrides.
	Env map[string]string `json:"env,omitempty"`

	// ExecutionProfile overrides agency.json/defaults profile selection.
	ExecutionProfile string `json:"execution_profile,omitempty"`

	// AgencyConfigPath is an optional exact agency config file to use.
	AgencyConfigPath string `json:"agency_config_path,omitempty"`

	// ClientRequestID is required for idempotency (UUID format).
	ClientRequestID string `json:"client_request_id"`

	// NoIncludeUntracked excludes untracked files from checkpoint snapshots.
	// Default is false (include untracked files).
	NoIncludeUntracked bool `json:"no_include_untracked,omitempty"`
}

func (r *ControlPlaneStartRequest) UnmarshalJSON(data []byte) error {
	type request ControlPlaneStartRequest
	var decoded request
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	env, ok, err := decodeRequestEnv(data, map[string]bool{
		"repo_root":            true,
		"worktree_ref":         true,
		"runner":               true,
		"prompt":               true,
		"invocation_name":      true,
		"runner_args":          true,
		"env":                  true,
		"execution_profile":    true,
		"agency_config_path":   true,
		"client_request_id":    true,
		"no_include_untracked": true,
	})
	if err != nil {
		return err
	}
	*r = ControlPlaneStartRequest(decoded)
	if ok {
		r.Env = env
	}
	return nil
}

// ControlPlaneStartResponse is the response body for POST /invocations/start_headless.
type ControlPlaneStartResponse struct {
	ResponseEnvelope
	InvocationID     string    `json:"invocation_id,omitempty"`
	SandboxPath      string    `json:"sandbox_path,omitempty"`
	RepoID           string    `json:"repo_id,omitempty"`
	RepoName         string    `json:"repo_name,omitempty"`
	WorktreeID       string    `json:"worktree_id,omitempty"`
	WorktreeName     string    `json:"worktree_name,omitempty"`
	ExecutionProfile string    `json:"execution_profile,omitempty"`
	CheckoutRoot     string    `json:"checkout_root,omitempty"`
	CustomEnvKeys    []string  `json:"custom_env_keys,omitempty"`
	PID              int       `json:"pid,omitempty"`
	PGID             int       `json:"pgid,omitempty"`
	DaemonInstanceID string    `json:"daemon_instance_id,omitempty"`
	AlreadyRunning   bool      `json:"already_running,omitempty"`
	LogPaths         *LogPaths `json:"log_paths,omitempty"`
	ClientRequestID  string    `json:"client_request_id,omitempty"`
}

// ControlPlaneFollowUpRequest is the request body for POST /invocations/{ref}/followup.
type ControlPlaneFollowUpRequest struct {
	// Prompt is the follow-up prompt text (max 256KB).
	Prompt string `json:"prompt"`

	// ClientRequestID is required for idempotent retries.
	ClientRequestID string `json:"client_request_id"`
}

// ControlPlaneFollowUpResponse is the response body for POST /invocations/{ref}/followup.
type ControlPlaneFollowUpResponse struct {
	ResponseEnvelope
	InvocationID    string `json:"invocation_id,omitempty"`
	TimelineEntry   string `json:"timeline_entry_id,omitempty"`
	AlreadyApplied  bool   `json:"already_applied,omitempty"`
	DeliveryMode    string `json:"delivery_mode,omitempty"` // "delivered" (stdin), "queued" (resume), "audit_only" (no relay)
	ClientRequestID string `json:"client_request_id,omitempty"`
}

// InvocationActionResponse is the shared response body for POST /invocations/{id}/stop and /kill.
type InvocationActionResponse struct {
	ResponseEnvelope
	InvocationID string `json:"invocation_id,omitempty"`
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

// ControlPlaneStartHeadedResponse is the response body for POST /invocations/start_headed.
type ControlPlaneStartHeadedResponse struct {
	ResponseEnvelope
	InvocationID     string    `json:"invocation_id,omitempty"`
	SandboxPath      string    `json:"sandbox_path,omitempty"`
	RepoID           string    `json:"repo_id,omitempty"`
	RepoName         string    `json:"repo_name,omitempty"`
	WorktreeID       string    `json:"worktree_id,omitempty"`
	WorktreeName     string    `json:"worktree_name,omitempty"`
	ExecutionProfile string    `json:"execution_profile,omitempty"`
	CheckoutRoot     string    `json:"checkout_root,omitempty"`
	CustomEnvKeys    []string  `json:"custom_env_keys,omitempty"`
	TmuxSession      string    `json:"tmux_session,omitempty"`
	DaemonInstanceID string    `json:"daemon_instance_id,omitempty"`
	AlreadyRunning   bool      `json:"already_running,omitempty"`
	LogPaths         *LogPaths `json:"log_paths,omitempty"`
	GitSHA           string    `json:"git_sha,omitempty"`
	ClientRequestID  string    `json:"client_request_id,omitempty"`
}
