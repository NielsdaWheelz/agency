package daemon

// WorktreeCreateRequest is the request body for POST /worktrees/create.
type WorktreeCreateRequest struct {
	// RepoRoot is the absolute path to the repository root.
	RepoRoot string `json:"repo_root"`

	// Name is the human-readable name (required, validated).
	Name string `json:"name"`

	// BaseBranch is the branch to branch from (required).
	BaseBranch string `json:"base_branch"`

	// IdempotencyKey is an optional UUID for idempotent create (scoped to repo_id).
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// WorktreeCreateResponse is the response body for POST /worktrees/create.
type WorktreeCreateResponse struct {
	responseEnvelope
	WorktreeID       string `json:"worktree_id,omitempty"`
	TreePath         string `json:"tree_path,omitempty"`
	Branch           string `json:"branch,omitempty"`
	RepoID           string `json:"repo_id,omitempty"`
	ExecutionProfile string `json:"execution_profile,omitempty"`
	CheckoutRoot     string `json:"checkout_root,omitempty"`
}

// WorktreeRmRequest is the request body for POST /worktrees/{id}/rm.
type WorktreeRmRequest struct {
	// Force forces removal even if the worktree is dirty or has unresolved invocations.
	Force bool `json:"force,omitempty"`
}

// WorktreeRmResponse is the response body for POST /worktrees/{id}/rm.
type WorktreeRmResponse struct {
	responseEnvelope
}

// WorktreePRSyncRequest is the request body for POST /worktrees/{ref}/pr/sync.
type WorktreePRSyncRequest struct {
	// AllowDirty permits PR sync when integration worktree has uncommitted changes.
	AllowDirty bool `json:"allow_dirty,omitempty"`

	// ForceWithLease uses git push --force-with-lease.
	ForceWithLease bool `json:"force_with_lease,omitempty"`
}

// WorktreePRSyncResponse is the response body for POST /worktrees/{ref}/pr/sync.
type WorktreePRSyncResponse struct {
	responseEnvelope
	RepoID                string `json:"repo_id,omitempty"`
	IntegrationWorktreeID string `json:"integration_worktree_id,omitempty"`
	Branch                string `json:"branch,omitempty"`
	PRNumber              int    `json:"pr_number,omitempty"`
	PRURL                 string `json:"pr_url,omitempty"`
	PRAction              string `json:"pr_action,omitempty"` // created|updated
}

// WorktreePRMergeRequest is the request body for POST /worktrees/{ref}/pr/merge.
type WorktreePRMergeRequest struct {
	// Strategy selects merge strategy: squash|merge|rebase (default: squash).
	Strategy string `json:"strategy,omitempty"`

	// ConfirmationMode is the explicit confirmation contract: yes|typed.
	ConfirmationMode string `json:"confirmation_mode,omitempty"`

	// Confirmed indicates caller has already satisfied the selected confirmation mode.
	Confirmed bool `json:"confirmed,omitempty"`

	// NoDeleteBranch preserves the remote branch after merge.
	NoDeleteBranch bool `json:"no_delete_branch,omitempty"`

	// AgencyConfigPath is an optional exact agency config file to use instead of canonical repo config.
	AgencyConfigPath string `json:"agency_config_path,omitempty"`
}

// WorktreePRMergeResponse is the response body for POST /worktrees/{ref}/pr/merge.
type WorktreePRMergeResponse struct {
	responseEnvelope
	Action                string            `json:"action,omitempty"` // started|attached
	RepoID                string            `json:"repo_id,omitempty"`
	IntegrationWorktreeID string            `json:"integration_worktree_id,omitempty"`
	Merge                 *WorktreeMergeDTO `json:"merge,omitempty"`
}

// WorktreeRebaseResponse is the response body for POST /worktrees/{ref}/rebase.
type WorktreeRebaseResponse struct {
	responseEnvelope
	RepoID                string `json:"repo_id,omitempty"`
	IntegrationWorktreeID string `json:"integration_worktree_id,omitempty"`
	Branch                string `json:"branch,omitempty"`
	BaseBranch            string `json:"base_branch,omitempty"`
}
