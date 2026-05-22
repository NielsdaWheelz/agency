// Package store provides persistence for agency data.
// This file implements integration worktree metadata and operations.
package store

import (
	"os"
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// WorktreeState represents the lifecycle state of an integration worktree.
type WorktreeState string

const (
	// WorktreeStatePresent indicates the worktree tree directory exists.
	WorktreeStatePresent WorktreeState = "present"

	// WorktreeStateArchived indicates the worktree was removed but record retained.
	WorktreeStateArchived WorktreeState = "archived"
)

// IntegrationWorktreeMeta represents the metadata for an integration worktree.
// This is persisted to meta.json in the worktree record directory.
type IntegrationWorktreeMeta struct {
	// SchemaVersion is the store schema version string.
	SchemaVersion string `json:"schema_version"`

	// WorktreeID is the unique identifier (format: <yyyymmddhhmmss>-<4hex>).
	WorktreeID string `json:"worktree_id"`

	// Name is the human-readable name (unique among non-archived worktrees).
	Name string `json:"name"`

	// RepoID is the repository identifier (16 hex chars).
	RepoID string `json:"repo_id"`

	// Branch is the git branch name (format: agency/<name>-<shortid>).
	Branch string `json:"branch"`

	// BaseBranch is the branch this worktree was created from.
	BaseBranch string `json:"base_branch"`

	// TreePath is the absolute path to the tree/ directory (the actual worktree).
	TreePath string `json:"tree_path"`

	// CheckoutRoot is the repo-scoped root that contains Agency-managed checkouts.
	CheckoutRoot string `json:"checkout_root"`

	// ExecutionProfile is the profile label selected when this worktree was created.
	ExecutionProfile string `json:"execution_profile"`

	// CreatedAt is the creation timestamp in RFC3339 UTC format.
	CreatedAt string `json:"created_at"`

	// LastUsedAt is the last activity timestamp in RFC3339 UTC format.
	LastUsedAt string `json:"last_used_at,omitempty"`

	// State is the lifecycle state (present or archived).
	State WorktreeState `json:"state"`

	// TaskID is the high-level task that created this worktree, when any.
	TaskID string `json:"task_id,omitempty"`

	// IdempotencyKey is the worktree create idempotency key, when any.
	IdempotencyKey string `json:"idempotency_key,omitempty"`

	// RequestFingerprint is the durable fingerprint for IdempotencyKey.
	RequestFingerprint string `json:"request_fingerprint,omitempty"`
}

// NewIntegrationWorktreeMeta creates a new IntegrationWorktreeMeta with required fields set.
func NewIntegrationWorktreeMeta(worktreeID, name, repoID, branch, baseBranch, treePath, checkoutRoot, executionProfile string, createdAt time.Time) *IntegrationWorktreeMeta {
	return &IntegrationWorktreeMeta{
		SchemaVersion:    SchemaVersion,
		WorktreeID:       worktreeID,
		Name:             name,
		RepoID:           repoID,
		Branch:           branch,
		BaseBranch:       baseBranch,
		TreePath:         treePath,
		CheckoutRoot:     checkoutRoot,
		ExecutionProfile: executionProfile,
		CreatedAt:        createdAt.UTC().Format(time.RFC3339),
		State:            WorktreeStatePresent,
	}
}

// EnsureIntegrationWorktreeDir creates the integration worktree record directory with exclusive semantics.
// Returns the worktree dir path on success.
// Fails with E_WORKTREE_DIR_EXISTS if the directory already exists.
func (s *Store) EnsureIntegrationWorktreeDir(repoID, worktreeID string) (string, error) {
	worktreeDir := s.IntegrationWorktreeDir(repoID, worktreeID)

	// Ensure parent directories exist (integration_worktrees/)
	parentDir := s.integrationWorktreesDir(repoID)
	if err := s.fsys.MkdirAll(parentDir, 0o700); err != nil {
		return "", errors.WrapWithDetails(
			errors.EWorktreeCreateFailed,
			"failed to create integration worktrees directory",
			err,
			map[string]string{"dir": parentDir},
		)
	}

	// Create worktree directory with exclusive semantics using os.Mkdir
	if err := os.Mkdir(worktreeDir, 0o700); err != nil {
		if os.IsExist(err) {
			return "", errors.NewWithDetails(
				errors.EWorktreeDirExists,
				"worktree directory already exists (worktree_id collision or stale state)",
				map[string]string{"worktree_dir": worktreeDir},
			)
		}
		return "", errors.WrapWithDetails(
			errors.EWorktreeCreateFailed,
			"failed to create worktree directory",
			err,
			map[string]string{"worktree_dir": worktreeDir},
		)
	}

	return worktreeDir, nil
}

// WriteIntegrationWorktreeMeta writes the meta.json for an integration worktree atomically.
func (s *Store) WriteIntegrationWorktreeMeta(repoID, worktreeID string, meta *IntegrationWorktreeMeta) error {
	metaPath := s.IntegrationWorktreeMetaPath(repoID, worktreeID)

	if err := fs.WriteJSONAtomic(s.fsys, metaPath, meta, 0o600); err != nil {
		return errors.WrapWithDetails(
			errors.EMetaWriteFailed,
			"failed to write integration worktree meta.json atomically",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	return nil
}

// UpdateIntegrationWorktreeMeta reads, updates, and writes meta.json atomically.
func (s *Store) UpdateIntegrationWorktreeMeta(repoID, worktreeID string, updateFn func(*IntegrationWorktreeMeta)) error {
	metaPath := s.IntegrationWorktreeMetaPath(repoID, worktreeID)

	// Read current meta
	meta, err := s.ReadIntegrationWorktreeMeta(repoID, worktreeID)
	if err != nil {
		return err
	}

	// Apply update
	updateFn(meta)

	// Write back atomically
	if err := fs.WriteJSONAtomic(s.fsys, metaPath, meta, 0o600); err != nil {
		return errors.WrapWithDetails(
			errors.EMetaWriteFailed,
			"failed to write integration worktree meta.json atomically",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	return nil
}

// ReadIntegrationWorktreeMeta reads and parses meta.json for an integration worktree.
// Returns E_WORKTREE_NOT_FOUND if the meta file doesn't exist.
// Returns E_STORE_CORRUPT if the file can't be parsed.
func (s *Store) ReadIntegrationWorktreeMeta(repoID, worktreeID string) (*IntegrationWorktreeMeta, error) {
	metaPath := s.IntegrationWorktreeMetaPath(repoID, worktreeID)

	data, err := s.fsys.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.NewWithDetails(
				errors.EWorktreeNotFound,
				"integration worktree not found (meta.json does not exist)",
				map[string]string{"meta_path": metaPath},
			)
		}
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to read integration worktree meta.json",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}

	var meta IntegrationWorktreeMeta
	if err := decodeStrictJSON(data, &meta); err != nil {
		return nil, errors.WrapWithDetails(
			errors.EStoreCorrupt,
			"failed to parse integration worktree meta.json",
			err,
			map[string]string{"meta_path": metaPath},
		)
	}
	if err := validateIntegrationWorktreeMeta(meta, repoID, worktreeID, metaPath); err != nil {
		return nil, err
	}

	return &meta, nil
}

func validateIntegrationWorktreeMeta(meta IntegrationWorktreeMeta, repoID, worktreeID, metaPath string) error {
	if err := validateSchemaVersion(meta.SchemaVersion, "integration worktree meta.json", metaPath); err != nil {
		return err
	}
	if meta.WorktreeID == "" || meta.Name == "" || meta.RepoID == "" || meta.Branch == "" ||
		meta.BaseBranch == "" || meta.TreePath == "" || meta.CheckoutRoot == "" ||
		meta.ExecutionProfile == "" || meta.CreatedAt == "" || meta.State == "" {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"integration worktree meta.json missing required fields",
			map[string]string{"meta_path": metaPath},
		)
	}
	if meta.WorktreeID != worktreeID || meta.RepoID != repoID {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"integration worktree meta.json id does not match record path",
			map[string]string{
				"meta_path":        metaPath,
				"path_worktree_id": worktreeID,
				"meta_worktree_id": meta.WorktreeID,
				"path_repo_id":     repoID,
				"meta_repo_id":     meta.RepoID,
			},
		)
	}
	if !filepath.IsAbs(meta.TreePath) || !filepath.IsAbs(meta.CheckoutRoot) {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"integration worktree meta.json paths must be absolute",
			map[string]string{"meta_path": metaPath},
		)
	}
	if !validWorktreeState(meta.State) {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			"integration worktree meta.json has unsupported state",
			map[string]string{
				"meta_path": metaPath,
				"state":     string(meta.State),
			},
		)
	}
	for _, timestamp := range []struct {
		field string
		value string
	}{
		{field: "created_at", value: meta.CreatedAt},
		{field: "last_used_at", value: meta.LastUsedAt},
	} {
		if err := validateCanonicalStoreTimestamp("integration worktree meta.json", "meta_path", metaPath, timestamp.field, timestamp.value); err != nil {
			return err
		}
	}
	return nil
}

func validWorktreeState(state WorktreeState) bool {
	switch state {
	case WorktreeStatePresent, WorktreeStateArchived:
		return true
	default:
		return false
	}
}

// RemoveIntegrationWorktreeDir removes the worktree record directory completely.
// This is used for cleanup on failed creation.
func (s *Store) RemoveIntegrationWorktreeDir(repoID, worktreeID string) error {
	worktreeDir := s.IntegrationWorktreeDir(repoID, worktreeID)
	return fs.SafeRemoveAll(worktreeDir, s.DataDir)
}
