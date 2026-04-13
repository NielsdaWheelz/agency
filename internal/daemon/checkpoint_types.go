package daemon

// CheckpointApplyRequest is the request body for POST /invocations/{id}/checkpoints/apply.
type CheckpointApplyRequest struct {
	// CheckpointID is the checkpoint number to restore.
	CheckpointID int `json:"checkpoint_id"`
}

// CheckpointApplyResponse is the response body for POST /invocations/{id}/checkpoints/apply.
type CheckpointApplyResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	CheckpointID   int    `json:"checkpoint_id,omitempty"`
	SnapshotCommit string `json:"snapshot_commit,omitempty"`
	RestoredAt     string `json:"restored_at,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// RestartFromCheckpointRequest is the request body for POST /invocations/{ref}/restart.
type RestartFromCheckpointRequest struct {
	// CheckpointID is the checkpoint number to restore before runner restart.
	CheckpointID int `json:"checkpoint_id"`

	// Env are optional environment variable overrides for the restarted runner.
	Env map[string]string `json:"env,omitempty"`

	// RunnerArgs are optional additional runner arguments for the restarted runner.
	RunnerArgs []string `json:"runner_args,omitempty"`
}

// RestartFromCheckpointResponse is the response body for POST /invocations/{ref}/restart.
type RestartFromCheckpointResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	InvocationID     string    `json:"invocation_id,omitempty"`
	CheckpointID     int       `json:"checkpoint_id,omitempty"`
	SnapshotCommit   string    `json:"snapshot_commit,omitempty"`
	RestoredAt       string    `json:"restored_at,omitempty"`
	PID              int       `json:"pid,omitempty"`
	PGID             int       `json:"pgid,omitempty"`
	DaemonInstanceID string    `json:"daemon_instance_id,omitempty"`
	LogPaths         *LogPaths `json:"log_paths,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}
