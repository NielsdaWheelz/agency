package daemon

import (
	"encoding/json"

	"github.com/NielsdaWheelz/agency/internal/store"
)

// TaskStartRequest is the request body for POST /tasks/start.
type TaskStartRequest struct {
	RepoRoot string `json:"repo_root"`

	// Name is the task and integration worktree name.
	Name string `json:"name"`

	BaseBranch string `json:"base_branch"`

	// Mode is headless or headed. Empty defaults to headless.
	Mode string `json:"mode,omitempty"`

	Runner string `json:"runner"`
	Prompt string `json:"prompt,omitempty"`

	// InvocationName is an optional label for the primary invocation.
	InvocationName string `json:"invocation_name,omitempty"`

	RunnerArgs []string          `json:"runner_args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`

	ExecutionProfile string `json:"execution_profile,omitempty"`
	AgencyConfigPath string `json:"agency_config_path,omitempty"`
	CheckoutRoot     string `json:"-"`

	ClientRequestID string `json:"client_request_id"`

	NoIncludeUntracked bool `json:"no_include_untracked,omitempty"`
}

func (r *TaskStartRequest) UnmarshalJSON(data []byte) error {
	type request TaskStartRequest
	var decoded request
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	env, ok, err := decodeRequestEnv(data, map[string]bool{
		"repo_root":            true,
		"name":                 true,
		"base_branch":          true,
		"mode":                 true,
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
	*r = TaskStartRequest(decoded)
	if ok {
		r.Env = env
	}
	return nil
}

// TaskRetryRequest is the request body for POST /tasks/{ref}/retry.
type TaskRetryRequest struct {
	Mode string `json:"mode,omitempty"`

	Runner string `json:"runner,omitempty"`
	Prompt string `json:"prompt,omitempty"`

	InvocationName string            `json:"invocation_name,omitempty"`
	RunnerArgs     []string          `json:"runner_args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`

	ExecutionProfile string `json:"execution_profile,omitempty"`
	AgencyConfigPath string `json:"agency_config_path,omitempty"`
	CheckoutRoot     string `json:"-"`

	ClientRequestID string `json:"client_request_id"`

	NoIncludeUntracked bool `json:"no_include_untracked,omitempty"`
}

func (r *TaskRetryRequest) UnmarshalJSON(data []byte) error {
	type request TaskRetryRequest
	var decoded request
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	env, ok, err := decodeRequestEnv(data, map[string]bool{
		"mode":                 true,
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
	*r = TaskRetryRequest(decoded)
	if ok {
		r.Env = env
	}
	return nil
}

// TaskStartResponse is the response body for POST /tasks/start.
type TaskStartResponse struct {
	OK           bool   `json:"ok"`
	RequestID    string `json:"request_id,omitempty"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`

	ClientRequestID string `json:"client_request_id,omitempty"`
	Duplicate       bool   `json:"duplicate,omitempty"`

	TaskID   string          `json:"task_id,omitempty"`
	TaskName string          `json:"task_name,omitempty"`
	State    store.TaskState `json:"state,omitempty"`

	RepoID   string `json:"repo_id,omitempty"`
	RepoName string `json:"repo_name,omitempty"`

	WorktreeID   string `json:"worktree_id,omitempty"`
	WorktreeName string `json:"worktree_name,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Branch       string `json:"branch,omitempty"`

	ExecutionProfile string   `json:"execution_profile,omitempty"`
	CheckoutRoot     string   `json:"checkout_root,omitempty"`
	CustomEnvKeys    []string `json:"custom_env_keys,omitempty"`

	InvocationID     string           `json:"invocation_id,omitempty"`
	SandboxPath      string           `json:"sandbox_path,omitempty"`
	Mode             store.RunnerMode `json:"mode,omitempty"`
	Runner           string           `json:"runner,omitempty"`
	PID              int              `json:"pid,omitempty"`
	PGID             int              `json:"pgid,omitempty"`
	TmuxSession      string           `json:"tmux_session,omitempty"`
	DaemonInstanceID string           `json:"daemon_instance_id,omitempty"`
	LogPaths         *LogPaths        `json:"log_paths,omitempty"`

	Partial     bool   `json:"partial,omitempty"`
	FailedPhase string `json:"failed_phase,omitempty"`

	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// TaskDTO is the read DTO for a task.
type TaskDTO struct {
	TaskID   string          `json:"task_id"`
	Name     string          `json:"name"`
	State    store.TaskState `json:"state"`
	RepoID   string          `json:"repo_id"`
	RepoName string          `json:"repo_name,omitempty"`

	BaseBranch       string `json:"base_branch,omitempty"`
	CheckoutRoot     string `json:"checkout_root,omitempty"`
	ExecutionProfile string `json:"execution_profile,omitempty"`

	WorktreeID   string `json:"worktree_id,omitempty"`
	WorktreeName string `json:"worktree_name,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Branch       string `json:"branch,omitempty"`

	PrimaryInvocationID string           `json:"primary_invocation_id,omitempty"`
	Mode                store.RunnerMode `json:"mode,omitempty"`
	Runner              string           `json:"runner,omitempty"`

	ClientRequestID string `json:"client_request_id,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	FailedPhase     string `json:"failed_phase,omitempty"`
	ErrorCode       string `json:"error_code,omitempty"`
	Error           string `json:"error,omitempty"`
}

// ListTasksData is the data payload for GET /tasks.
type ListTasksData struct {
	Tasks []TaskDTO `json:"tasks"`
}
