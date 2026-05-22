package daemon

// CheckpointApplyRequest is the request body for POST /invocations/{id}/checkpoints/apply.
type CheckpointApplyRequest struct {
	// CheckpointID is the checkpoint number to restore.
	CheckpointID int `json:"checkpoint_id"`
}

// CheckpointApplyResponse is the response body for POST /invocations/{id}/checkpoints/apply.
type CheckpointApplyResponse struct {
	ResponseEnvelope
	CheckpointID   int    `json:"checkpoint_id,omitempty"`
	SnapshotCommit string `json:"snapshot_commit,omitempty"`
	RestoredAt     string `json:"restored_at,omitempty"`
}
