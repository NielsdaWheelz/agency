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
