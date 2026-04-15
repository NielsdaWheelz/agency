package daemon

// WorktreeCreateRequest is the request body for POST /worktrees/create.
type WorktreeCreateRequest struct {
	// RepoRoot is the absolute path to the repository root.
	RepoRoot string `json:"repo_root"`

	// Name is the human-readable name (required, validated).
	Name string `json:"name"`

	// ParentBranch is the branch to branch from (optional, defaults to current branch).
	ParentBranch string `json:"parent_branch,omitempty"`

	// IdempotencyKey is an optional UUID for idempotent create (scoped to repo_id).
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// WorktreeCreateResponse is the response body for POST /worktrees/create.
type WorktreeCreateResponse struct {
	OK           bool   `json:"ok"`
	WorktreeID   string `json:"worktree_id,omitempty"`
	TreePath     string `json:"tree_path,omitempty"`
	Branch       string `json:"branch,omitempty"`
	RepoID       string `json:"repo_id,omitempty"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// WorktreeRmRequest is the request body for POST /worktrees/{id}/rm.
type WorktreeRmRequest struct {
	// Force forces removal even if the worktree is dirty or has active invocations.
	Force bool `json:"force,omitempty"`
}

// WorktreeRmResponse is the response body for POST /worktrees/{id}/rm.
type WorktreeRmResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// WorktreePRSyncRequest is the request body for POST /worktrees/{ref}/pr/sync.
type WorktreePRSyncRequest struct {
	// AllowDirty permits PR sync when integration worktree has uncommitted changes.
	AllowDirty bool `json:"allow_dirty,omitempty"`

	// ForceWithLease uses git push --force-with-lease.
	ForceWithLease bool `json:"force_with_lease,omitempty"`
}

// ReportDiagnostic is an explicit machine-readable report contract diagnostic.
type ReportDiagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
}

// WorktreePRSyncResponse is the response body for POST /worktrees/{ref}/pr/sync.
type WorktreePRSyncResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	RepoID                string             `json:"repo_id,omitempty"`
	IntegrationWorktreeID string             `json:"integration_worktree_id,omitempty"`
	Branch                string             `json:"branch,omitempty"`
	PRNumber              int                `json:"pr_number,omitempty"`
	PRURL                 string             `json:"pr_url,omitempty"`
	PRAction              string             `json:"pr_action,omitempty"` // created|updated
	ReportSource          string             `json:"report_source,omitempty"`
	ReportDiagnostics     []ReportDiagnostic `json:"report_diagnostics,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
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
}

// WorktreePRMergeResponse is the response body for POST /worktrees/{ref}/pr/merge.
type WorktreePRMergeResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	RepoID                string             `json:"repo_id,omitempty"`
	IntegrationWorktreeID string             `json:"integration_worktree_id,omitempty"`
	Branch                string             `json:"branch,omitempty"`
	PRNumber              int                `json:"pr_number,omitempty"`
	PRURL                 string             `json:"pr_url,omitempty"`
	Strategy              string             `json:"strategy,omitempty"`
	DeleteBranch          bool               `json:"delete_branch,omitempty"`
	MergeLogPath          string             `json:"merge_log_path,omitempty"`
	VerifyLogPath         string             `json:"verify_log_path,omitempty"`
	ArchiveLogPath        string             `json:"archive_log_path,omitempty"`
	ReportSource          string             `json:"report_source,omitempty"`
	ReportDiagnostics     []ReportDiagnostic `json:"report_diagnostics,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// WorktreeUpdateResponse is the response body for POST /worktrees/{ref}/update.
type WorktreeUpdateResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	RepoID                string `json:"repo_id,omitempty"`
	IntegrationWorktreeID string `json:"integration_worktree_id,omitempty"`
	Branch                string `json:"branch,omitempty"`
	ParentBranch          string `json:"parent_branch,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}
