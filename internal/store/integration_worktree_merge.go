package store

import (
	"os"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// WorktreeMergeStatus represents the lifecycle status of a worktree PR merge.
type WorktreeMergeStatus string

const (
	// WorktreeMergeStatusRunning indicates the merge attempt is still active.
	WorktreeMergeStatusRunning WorktreeMergeStatus = "running"

	// WorktreeMergeStatusSucceeded indicates the merge attempt completed successfully.
	WorktreeMergeStatusSucceeded WorktreeMergeStatus = "succeeded"

	// WorktreeMergeStatusFailed indicates the merge attempt terminated unsuccessfully.
	WorktreeMergeStatusFailed WorktreeMergeStatus = "failed"
)

// WorktreeMergeStage represents the current linear step of a worktree PR merge.
type WorktreeMergeStage string

const (
	WorktreeMergeStagePreflight WorktreeMergeStage = "preflight"
	WorktreeMergeStageVerify    WorktreeMergeStage = "verify"
	WorktreeMergeStageMerge     WorktreeMergeStage = "merge"
	WorktreeMergeStageArchive   WorktreeMergeStage = "archive"
	WorktreeMergeStageCompleted WorktreeMergeStage = "completed"
)

// IntegrationWorktreeMergeMeta is the durable merge lifecycle record for one worktree.
type IntegrationWorktreeMergeMeta struct {
	SchemaVersion string `json:"schema_version"`

	RepoID     string `json:"repo_id"`
	WorktreeID string `json:"worktree_id"`
	AttemptID  string `json:"attempt_id"`
	RequestID  string `json:"request_id,omitempty"`

	Status WorktreeMergeStatus `json:"status"`
	Stage  WorktreeMergeStage  `json:"stage"`

	Strategy         string `json:"strategy"`
	DeleteBranch     bool   `json:"delete_branch"`
	AgencyConfigPath string `json:"agency_config_path,omitempty"`

	Branch   string `json:"branch,omitempty"`
	PRNumber int    `json:"pr_number,omitempty"`
	PRURL    string `json:"pr_url,omitempty"`

	MergeLogPath   string `json:"merge_log_path,omitempty"`
	VerifyLogPath  string `json:"verify_log_path,omitempty"`
	ArchiveLogPath string `json:"archive_log_path,omitempty"`

	StartedAt  string `json:"started_at"`
	UpdatedAt  string `json:"updated_at"`
	FinishedAt string `json:"finished_at,omitempty"`

	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	Hint         string `json:"hint,omitempty"`
}

// NewIntegrationWorktreeMergeMeta creates a new worktree merge record in running/preflight state.
func NewIntegrationWorktreeMergeMeta(
	repoID, worktreeID, attemptID, requestID, strategy string,
	deleteBranch bool,
	agencyConfigPath string,
	now time.Time,
) *IntegrationWorktreeMergeMeta {
	ts := now.UTC().Format(time.RFC3339)
	return &IntegrationWorktreeMergeMeta{
		SchemaVersion:    SchemaVersion,
		RepoID:           repoID,
		WorktreeID:       worktreeID,
		AttemptID:        attemptID,
		RequestID:        requestID,
		Status:           WorktreeMergeStatusRunning,
		Stage:            WorktreeMergeStagePreflight,
		Strategy:         strategy,
		DeleteBranch:     deleteBranch,
		AgencyConfigPath: agencyConfigPath,
		StartedAt:        ts,
		UpdatedAt:        ts,
	}
}

// WriteIntegrationWorktreeMerge writes merge.json atomically.
func (s *Store) WriteIntegrationWorktreeMerge(repoID, worktreeID string, meta *IntegrationWorktreeMergeMeta) error {
	mergePath := s.IntegrationWorktreeMergePath(repoID, worktreeID)
	if err := fs.WriteJSONAtomic(s.fsys, mergePath, meta, 0o600); err != nil {
		return errors.WrapWithDetails(
			errors.EMetaWriteFailed,
			"failed to write integration worktree merge.json atomically",
			err,
			map[string]string{"merge_path": mergePath},
		)
	}
	return nil
}

// ReadIntegrationWorktreeMerge reads and parses merge.json for an integration worktree.
// Returns (nil, nil) when merge.json does not exist.
func (s *Store) ReadIntegrationWorktreeMerge(repoID, worktreeID string) (*IntegrationWorktreeMergeMeta, error) {
	mergePath := s.IntegrationWorktreeMergePath(repoID, worktreeID)
	data, err := s.fsys.ReadFile(mergePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to read integration worktree merge.json",
			err,
			map[string]string{"merge_path": mergePath},
		)
	}

	var meta IntegrationWorktreeMergeMeta
	if err := decodeStrictJSON(data, &meta); err != nil {
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to parse integration worktree merge.json",
			err,
			map[string]string{"merge_path": mergePath},
		)
	}
	fields, err := strictJSONObjectFields(data)
	if err != nil {
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to parse integration worktree merge.json",
			err,
			map[string]string{"merge_path": mergePath},
		)
	}
	for _, field := range []string{
		"schema_version",
		"repo_id",
		"worktree_id",
		"attempt_id",
		"status",
		"stage",
		"strategy",
		"delete_branch",
		"started_at",
		"updated_at",
	} {
		if _, ok := fields[field]; !ok {
			return nil, errors.NewWithDetails(
				errors.EStoreCorrupt,
				"integration worktree merge.json missing required field "+field,
				map[string]string{"merge_path": mergePath},
			)
		}
	}
	if err := validateSchemaVersion(meta.SchemaVersion, "integration worktree merge.json", mergePath); err != nil {
		return nil, err
	}
	if meta.WorktreeID == "" || meta.RepoID == "" || meta.AttemptID == "" || meta.Strategy == "" ||
		meta.StartedAt == "" || meta.UpdatedAt == "" {
		return nil, errors.NewWithDetails(
			errors.EStoreCorrupt,
			"integration worktree merge.json is missing required fields",
			map[string]string{"merge_path": mergePath},
		)
	}
	if meta.WorktreeID != worktreeID || meta.RepoID != repoID {
		return nil, errors.NewWithDetails(
			errors.EStoreCorrupt,
			"integration worktree merge.json id does not match record path",
			map[string]string{
				"merge_path":       mergePath,
				"path_worktree_id": worktreeID,
				"meta_worktree_id": meta.WorktreeID,
				"path_repo_id":     repoID,
				"meta_repo_id":     meta.RepoID,
			},
		)
	}

	switch meta.Status {
	case WorktreeMergeStatusRunning, WorktreeMergeStatusSucceeded, WorktreeMergeStatusFailed:
	default:
		return nil, errors.NewWithDetails(
			errors.EStoreCorrupt,
			"integration worktree merge.json has unsupported status",
			map[string]string{
				"merge_path": mergePath,
				"status":     string(meta.Status),
			},
		)
	}

	switch meta.Stage {
	case WorktreeMergeStagePreflight, WorktreeMergeStageVerify, WorktreeMergeStageMerge, WorktreeMergeStageArchive, WorktreeMergeStageCompleted:
	default:
		return nil, errors.NewWithDetails(
			errors.EStoreCorrupt,
			"integration worktree merge.json has unsupported stage",
			map[string]string{
				"merge_path": mergePath,
				"stage":      string(meta.Stage),
			},
		)
	}
	for _, timestamp := range []struct {
		field string
		value string
	}{
		{field: "started_at", value: meta.StartedAt},
		{field: "updated_at", value: meta.UpdatedAt},
		{field: "finished_at", value: meta.FinishedAt},
	} {
		if err := validateCanonicalStoreTimestamp("integration worktree merge.json", "merge_path", mergePath, timestamp.field, timestamp.value); err != nil {
			return nil, err
		}
	}

	return &meta, nil
}

// UpdateIntegrationWorktreeMerge reads, updates, and writes merge.json atomically.
func (s *Store) UpdateIntegrationWorktreeMerge(repoID, worktreeID string, updateFn func(*IntegrationWorktreeMergeMeta)) error {
	meta, err := s.ReadIntegrationWorktreeMerge(repoID, worktreeID)
	if err != nil {
		return err
	}
	if meta == nil {
		return errors.NewWithDetails(
			errors.EWorktreeNotFound,
			"integration worktree merge state not found",
			map[string]string{"merge_path": s.IntegrationWorktreeMergePath(repoID, worktreeID)},
		)
	}
	updateFn(meta)
	return s.WriteIntegrationWorktreeMerge(repoID, worktreeID, meta)
}
