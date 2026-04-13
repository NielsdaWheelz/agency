// Package daemon implements the agency daemon supervisor.
// This file defines types for the read API (PR-12).
package daemon

// ----- Response Envelope -----

// APIResponse is the standard response envelope for all read endpoints.
// All responses include the envelope fields; data is type-specific.
type APIResponse struct {
	OK           bool        `json:"ok"`
	APIVersion   int         `json:"api_version"`
	BuildVersion string      `json:"build_version"`
	GitSHA       string      `json:"git_sha"`
	RequestID    string      `json:"request_id"`
	Data         interface{} `json:"data,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string      `json:"error_code,omitempty"`
	Message   string      `json:"message,omitempty"`
	Hint      string      `json:"hint,omitempty"`
	Details   interface{} `json:"details,omitempty"`
}

// AmbiguousDetails is the details shape for E_AMBIGUOUS errors.
type AmbiguousDetails struct {
	Candidates []string `json:"candidates"`
}

// InvalidQueryArgumentDetails is the structured error details for invalid
// list-filter enum inputs (L2 InvalidQueryArgumentDetails).
type InvalidQueryArgumentDetails struct {
	Param         string   `json:"param"`
	Value         string   `json:"value"`
	AllowedValues []string `json:"allowed_values"`
}

// CursorInvalidDetails is the details shape for E_CURSOR_INVALID errors.
type CursorInvalidDetails struct {
	Reason string `json:"reason"`
}

// ----- WorktreeDTO -----

// WorktreeDTO is the canonical DTO for integration worktree responses.
type WorktreeDTO struct {
	WorktreeID   string `json:"worktree_id"`
	Name         string `json:"name"`
	RepoID       string `json:"repo_id"`
	Branch       string `json:"branch"`
	ParentBranch string `json:"parent_branch"`
	TreePath     string `json:"tree_path"`
	State        string `json:"state"` // "present" or "archived"
	CreatedAt    string `json:"created_at"`
	LastUsedAt   string `json:"last_used_at,omitempty"`
}

// ----- InvocationDTO -----

// InvocationDTO is the canonical DTO for invocation responses.
// Used by both list and show endpoints (no separate summary DTO).
type InvocationDTO struct {
	InvocationID   string `json:"invocation_id"`
	InvocationName string `json:"invocation_name,omitempty"`
	WorktreeID     string `json:"worktree_id"`
	RepoID         string `json:"repo_id"`
	Runner         string `json:"runner"`
	Mode           string `json:"mode"` // "headed" or "headless"

	// Timestamps
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at,omitempty"`
	LastOutputAt string `json:"last_output_at,omitempty"`

	// Lifecycle status
	Status     string `json:"status"`      // starting, running, finished, failed
	ExitReason string `json:"exit_reason"` // exited, killed, stopped, start_failed, unknown
	ExitCode   *int   `json:"exit_code,omitempty"`

	// Semantic status (headless only)
	SemanticStatus string `json:"semantic_status,omitempty"` // working, needs_input, blocked, ready_for_review

	// Landing status
	LandingStatus string `json:"landing_status,omitempty"` // pending, landed, discarded

	// Derived display fields (computed by daemon)
	DisplayStatus  string   `json:"display_status"`  // daemon-derived human-readable status
	AttentionFlags []string `json:"attention_flags"` // daemon-derived flags
	SortKey        int      `json:"sort_key"`        // daemon-derived priority for rendering

	// Shared activity projection (PR-05)
	StatusSummary  string                        `json:"status_summary,omitempty"`
	LatestActivity *InvocationLatestActivity     `json:"latest_activity,omitempty"`
	Navigation     *InvocationActivityNavigation `json:"navigation,omitempty"`

	// Paths
	SandboxPath string `json:"sandbox_path"`
	LogsDir     string `json:"logs_dir,omitempty"`
}

// InvocationLatestActivity summarizes the latest meaningful invocation activity.
// Shared across list/watch/show/review surfaces (PR-05).
type InvocationLatestActivity struct {
	TurnID    string `json:"turn_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Summary   string `json:"summary,omitempty"`

	ToolCallCount int                          `json:"tool_call_count,omitempty"`
	ToolCalls     []InvocationActivityToolCall `json:"tool_calls,omitempty"`

	CheckpointID           int      `json:"checkpoint_id,omitempty"`
	Restorable             bool     `json:"restorable,omitempty"`
	CheckpointDescription  string   `json:"checkpoint_description,omitempty"`
	CheckpointDiffstat     string   `json:"checkpoint_diffstat,omitempty"`
	CheckpointChangedPaths []string `json:"checkpoint_changed_paths,omitempty"`
	CheckpointChangedCount int      `json:"checkpoint_changed_count,omitempty"`
	CheckpointPathsTrimmed bool     `json:"checkpoint_paths_trimmed,omitempty"`
}

// InvocationActivityToolCall summarizes one tool call in latest_activity.
type InvocationActivityToolCall struct {
	Name     string `json:"name,omitempty"`
	Command  string `json:"command,omitempty"`
	HasExit  bool   `json:"has_exit,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
}

// InvocationActivityNavigation links activity projections back to history/diff context.
type InvocationActivityNavigation struct {
	LatestTurnID   string `json:"latest_turn_id,omitempty"`
	HistoryCommand string `json:"history_command,omitempty"`
	DiffCommand    string `json:"diff_command,omitempty"`
}

// ----- CheckpointDTO -----

// CheckpointDTO is the canonical DTO for checkpoint responses.
type CheckpointDTO struct {
	ID                int    `json:"id"`
	CreatedAt         string `json:"created_at"`
	Diffstat          string `json:"diffstat"`
	SnapshotCommit    string `json:"snapshot_commit"`
	IncludesUntracked bool   `json:"includes_untracked"`
	Degraded          bool   `json:"degraded"`

	// Semantic trigger metadata (schema 1.1+). Omitted for legacy checkpoints.
	Trigger     string `json:"trigger,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	StreamSeq   uint64 `json:"stream_seq,omitempty"`
	Description string `json:"description,omitempty"`

	// Authoritative changed-path preview for the checkpoint interval.
	ChangedPaths         []string `json:"changed_paths,omitempty"`
	ChangedPathCount     int      `json:"changed_path_count,omitempty"`
	ChangedPathTruncated bool     `json:"changed_path_truncated,omitempty"`
}

// ----- List Response Types -----

// ListWorktreesData is the data payload for GET /worktrees.
type ListWorktreesData struct {
	Worktrees  []WorktreeDTO `json:"worktrees"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// ListInvocationsData is the data payload for GET /invocations.
type ListInvocationsData struct {
	Invocations []InvocationDTO `json:"invocations"`
	NextCursor  string          `json:"next_cursor,omitempty"`
}

// ListCheckpointsData is the data payload for GET /invocations/{id}/checkpoints.
type ListCheckpointsData struct {
	Checkpoints []CheckpointDTO `json:"checkpoints"`
	NextCursor  string          `json:"next_cursor,omitempty"`
}

// ----- Diff Response Types -----

// DiffCommit represents a single commit in the diff response.
type DiffCommit struct {
	SHA     string `json:"sha"`
	Summary string `json:"summary"`
}

// DiffRange represents a range of changes (committed or working tree).
type DiffRange struct {
	From           string       `json:"from,omitempty"`
	To             string       `json:"to,omitempty"`
	Commits        []DiffCommit `json:"commits,omitempty"`
	Diffstat       string       `json:"diffstat"`
	Patch          string       `json:"patch,omitempty"`
	PatchTruncated bool         `json:"patch_truncated"`
	PatchBytes     int          `json:"patch_bytes"`
}

// DiffTurnSelector captures the turn selector used for turn-aware diff context.
type DiffTurnSelector struct {
	Kind        string `json:"kind"` // "single" or "range"
	TurnID      string `json:"turn_id,omitempty"`
	StartTurnID string `json:"start_turn_id,omitempty"`
	EndTurnID   string `json:"end_turn_id,omitempty"`
}

// DiffTurnContext describes deterministic turn-to-diff mapping metadata.
type DiffTurnContext struct {
	Selector          DiffTurnSelector `json:"selector"`
	StartCheckpointID int              `json:"start_checkpoint_id"`
	EndCheckpointID   int              `json:"end_checkpoint_id"`
	FromCommit        string           `json:"from_commit"`
	ToCommit          string           `json:"to_commit"`
}

// InvocationDiffData is the data payload for GET /invocations/{id}/diff.
type InvocationDiffData struct {
	BaseCommit               string           `json:"base_commit"`
	SandboxBranchTip         string           `json:"sandbox_branch_tip"`
	HasCommits               bool             `json:"has_commits"`
	HasUncommitted           bool             `json:"has_uncommitted"`
	CommittedRange           *DiffRange       `json:"committed_range,omitempty"`
	WorkingTree              *DiffRange       `json:"working_tree,omitempty"`
	PatchIncludesUncommitted bool             `json:"patch_includes_uncommitted"`
	TurnContext              *DiffTurnContext `json:"turn_context,omitempty"`
}

// InvocationReviewReason is one deterministic blocking reason in review output.
type InvocationReviewReason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// InvocationReviewNavigation links review findings back to invocation context.
type InvocationReviewNavigation struct {
	InvocationRef  string `json:"invocation_ref"`
	RepoID         string `json:"repo_id"`
	LatestTurnID   string `json:"latest_turn_id,omitempty"`
	HistoryCommand string `json:"history_command"`
	DiffCommand    string `json:"diff_command,omitempty"`
	PRSyncCommand  string `json:"pr_sync_command,omitempty"`
}

// InvocationReviewData is the data payload for GET /invocations/{id}/review.
type InvocationReviewData struct {
	InvocationID      string                     `json:"invocation_id"`
	RepoID            string                     `json:"repo_id"`
	Ready             bool                       `json:"ready"`
	Readiness         string                     `json:"readiness"` // "ready" or "blocked"
	PRSyncEligible    bool                       `json:"pr_sync_eligible"`
	ReportSource      string                     `json:"report_source,omitempty"`
	Status            string                     `json:"status"`
	DisplayStatus     string                     `json:"display_status"`
	SemanticStatus    string                     `json:"semantic_status,omitempty"`
	LandingStatus     string                     `json:"landing_status,omitempty"`
	RunnerStatus      string                     `json:"runner_status,omitempty"`
	RunnerSummary     string                     `json:"runner_summary,omitempty"`
	StatusSummary     string                     `json:"status_summary,omitempty"`
	LatestActivity    *InvocationLatestActivity  `json:"latest_activity,omitempty"`
	RunnerUpdatedAt   string                     `json:"runner_updated_at,omitempty"`
	HowToTest         string                     `json:"how_to_test,omitempty"`
	ReportDiagnostics []ReportDiagnostic         `json:"report_diagnostics,omitempty"`
	BlockingReasons   []InvocationReviewReason   `json:"blocking_reasons"`
	Navigation        InvocationReviewNavigation `json:"navigation"`
}

// InvocationLogsOffsetData is the data payload for GET /invocations/{id}/logs.
type InvocationLogsOffsetData struct {
	Kind       string `json:"kind"` // raw, stderr, stream
	DataB64    string `json:"data_b64"`
	NextOffset int64  `json:"next_offset"`
	TotalBytes int64  `json:"total_bytes"`
}

// ----- Pagination Cursor Types -----

// InvocationCursor is the internal cursor structure for invocation pagination.
type InvocationCursor struct {
	StartedAt    string `json:"started_at"`
	InvocationID string `json:"invocation_id"`
}

// WorktreeCursor is the internal cursor structure for worktree pagination.
type WorktreeCursor struct {
	LastUsedAt string `json:"last_used_at"`
	WorktreeID string `json:"worktree_id"`
}

// CheckpointCursor is the internal cursor structure for checkpoint pagination.
type CheckpointCursor struct {
	ID int `json:"id"`
}

// ----- Display Status Constants -----

// Display status values derived by daemon.
const (
	DisplayStatusFailed         = "failed"
	DisplayStatusNeedsAttention = "needs attention"
	DisplayStatusNeedsInput     = "needs input"
	DisplayStatusBlocked        = "blocked"
	DisplayStatusReadyForReview = "ready for review"
	DisplayStatusWorking        = "working"
	DisplayStatusRunning        = "running"
	DisplayStatusFinished       = "finished"
	DisplayStatusStarting       = "starting"
	DisplayStatusLanded         = "landed"
	DisplayStatusDiscarded      = "discarded"
)

// Attention flag values.
const (
	AttentionFlagNeedsAttention = "needs_attention"
	AttentionFlagStalled        = "stalled"
	AttentionFlagOrphaned       = "orphaned"
	AttentionFlagLandable       = "landable"
)

// Sort key constants (lower = higher priority).
const (
	SortKeyFailed         = 10
	SortKeyNeedsAttention = 20
	SortKeyNeedsInput     = 30
	SortKeyBlocked        = 40
	SortKeyReadyForReview = 50
	SortKeyWorking        = 60
	SortKeyRunning        = 70
	SortKeyFinished       = 80
	SortKeyStarting       = 90
	SortKeyLanded         = 100
	SortKeyDiscarded      = 110
)

// ----- Query Parameters -----

// ListWorktreesParams holds query parameters for GET /worktrees.
type ListWorktreesParams struct {
	RepoID string // optional, filter by repo
	State  string // present, archived, all (default: present)
	Limit  int    // default 100, max 500
	Cursor string // opaque pagination cursor
}

// ListInvocationsParams holds query parameters for GET /invocations.
type ListInvocationsParams struct {
	RepoID      string // optional, filter by repo
	WorktreeID  string // optional, filter by worktree (can be ref)
	WorktreeRef string // optional, filter by worktree ref (name/id/prefix)
	State       string // active, finished, all (default: all)
	Mode        string // headed, headless, all (default: all)
	Limit       int    // default 100, max 500
	Cursor      string // opaque pagination cursor
}

// GetLogsParams holds query parameters for GET /invocations/{id}/logs.
type GetLogsParams struct {
	Kind   string // raw, stderr, stream (default: raw)
	Offset int64  // byte offset from start of file (>= 0)
	Limit  int    // max bytes returned; clamped to [1, MaxLogChunk]
}

// MaxLogChunk is the maximum bytes per offset-mode log read (1 MB).
const MaxLogChunk = 1_048_576

// ----- Timeline Response Types (S3 PR-01) -----

// TimelineEntryDTO is a normalized, typed entry in the invocation timeline.
type TimelineEntryDTO struct {
	EntryID   string                 `json:"entry_id"`
	Kind      string                 `json:"kind"`
	Source    string                 `json:"source"`
	Timestamp string                 `json:"timestamp"`
	Seq       uint64                 `json:"seq,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// InvocationTimelineData is the data payload for GET /invocations/{id}/timeline.
type InvocationTimelineData struct {
	Entries    []TimelineEntryDTO `json:"entries"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// TimelineCursor is the internal cursor structure for timeline keyset pagination.
type TimelineCursor struct {
	Timestamp  string `json:"timestamp"`
	SourceRank int    `json:"source_rank"`
	Seq        uint64 `json:"seq"`
	EntryID    string `json:"entry_id"`
}

// GetTimelineParams holds query parameters for GET /invocations/{id}/timeline.
type GetTimelineParams struct {
	Limit  int    // default 100, max 500
	Cursor string // opaque pagination cursor
	Order  string // "asc" (default) or "desc"
}

// ----- S1 Release Gate DTOs (PR-05) -----

// S1ReleaseReadinessData is the data payload for GET /spec/v2.1/s1/release/readiness.
type S1ReleaseReadinessData struct {
	Slice      string            `json:"slice"`
	SliceReady bool              `json:"slice_ready"`
	GateA      *S1GateStatusData `json:"gate_a"`
	GateB      *S1GateStatusData `json:"gate_b"`
}

// S1GateStatusData is the gate-level status within S1 release data.
type S1GateStatusData struct {
	GateID        string   `json:"gate_id"`
	Status        string   `json:"status"`
	TotalItems    int      `json:"total_items"`
	ClosedItems   int      `json:"closed_items"`
	BlockingItems []string `json:"blocking_items"`
}

// S1ClosureReportData is the data payload for GET /spec/v2.1/s1/release/closure-report.
type S1ClosureReportData struct {
	Slice string             `json:"slice"`
	GateA *S1GateClosureData `json:"gate_a"`
	GateB *S1GateClosureData `json:"gate_b"`
}

// S1GateClosureData is the gate-level closure snapshot within S1 closure report.
type S1GateClosureData struct {
	GateID         string                 `json:"gate_id"`
	Status         string                 `json:"status"`
	TotalItems     int                    `json:"total_items"`
	ClosedItems    int                    `json:"closed_items"`
	BlockingItems  []string               `json:"blocking_items"`
	ClosedEvidence []S1ClosedItemEvidence `json:"closed_evidence"`
}

// S1ClosedItemEvidence is the evidence payload for a single closed gate item.
type S1ClosedItemEvidence struct {
	IssuePath       string               `json:"issue_path"`
	ImplementedRefs []string             `json:"implemented_refs"`
	TargetedTests   []S1TestEvidenceData `json:"targeted_tests"`
	SuiteTests      []S1TestEvidenceData `json:"suite_tests"`
}

// S1TestEvidenceData is a single test evidence entry in daemon response.
type S1TestEvidenceData struct {
	IssuePath   string `json:"issue_path"`
	Command     string `json:"command"`
	Scope       string `json:"scope"`
	Result      string `json:"result"`
	ArtifactRef string `json:"artifact_ref"`
	RecordedAt  string `json:"recorded_at"`
}

// S1FreezeReadinessData is the data payload for GET /spec/v2.1/s1/release/freeze-readiness.
type S1FreezeReadinessData struct {
	FreezeReady     bool   `json:"freeze_ready"`
	UnresolvedCount int    `json:"unresolved_count"`
	SpecPath        string `json:"spec_path"`
	FirstQuestion   string `json:"first_question,omitempty"`
}

// GetDiffParams holds query parameters for GET /invocations/{id}/diff.
type GetDiffParams struct {
	IncludePatch       bool   // default true
	MaxPatchBytes      int    // default 2097152 (2MB), max 5242880 (5MB)
	IncludeUncommitted bool   // default true
	TurnID             string // optional timeline entry id (turn selector)
	TurnStartID        string // optional inclusive range start timeline entry id
	TurnEndID          string // optional inclusive range end timeline entry id
}
