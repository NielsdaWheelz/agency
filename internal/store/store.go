// Package store provides persistence for repo_index.json and repo.json files.
// Files are written atomically via temp file + rename.
package store

import (
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/fs"
)

// Store handles persistence of repo index and repo records.
type Store struct {
	FS      fs.FS            // filesystem interface for stubbing
	DataDir string           // resolved AGENCY_DATA_DIR
	Now     func() time.Time // injectable clock for deterministic tests
}

// NewStore creates a new Store with the given dependencies.
func NewStore(filesystem fs.FS, dataDir string, now func() time.Time) *Store {
	return &Store{
		FS:      filesystem,
		DataDir: dataDir,
		Now:     now,
	}
}

// RepoIndexPath returns the path to repo_index.json.
func (s *Store) RepoIndexPath() string {
	return filepath.Join(s.DataDir, "repo_index.json")
}

// RepoDir returns the directory for a repo's data.
func (s *Store) RepoDir(repoID string) string {
	return filepath.Join(s.DataDir, "repos", repoID)
}

// RepoRecordPath returns the path to a repo's repo.json.
func (s *Store) RepoRecordPath(repoID string) string {
	return filepath.Join(s.RepoDir(repoID), "repo.json")
}

// RunsDir returns the runs directory for a repo.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/
func (s *Store) RunsDir(repoID string) string {
	return filepath.Join(s.RepoDir(repoID), "runs")
}

// RunDir returns the directory for a specific run.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/
func (s *Store) RunDir(repoID, runID string) string {
	return filepath.Join(s.RunsDir(repoID), runID)
}

// RunMetaPath returns the path to a run's meta.json.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json
func (s *Store) RunMetaPath(repoID, runID string) string {
	return filepath.Join(s.RunDir(repoID, runID), "meta.json")
}

// RunLogsDir returns the logs directory for a run.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/
func (s *Store) RunLogsDir(repoID, runID string) string {
	return filepath.Join(s.RunDir(repoID, runID), "logs")
}

// VerifyRecordPath returns the path to a run's verify_record.json.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json
func (s *Store) VerifyRecordPath(repoID, runID string) string {
	return filepath.Join(s.RunDir(repoID, runID), "verify_record.json")
}

// EventsPath returns the path to a run's events.jsonl.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl
func (s *Store) EventsPath(repoID, runID string) string {
	return filepath.Join(s.RunDir(repoID, runID), "events.jsonl")
}

// ----- V2 Integration Worktree paths (Slice 8) -----

// IntegrationWorktreesDir returns the integration worktrees directory for a repo.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/
func (s *Store) IntegrationWorktreesDir(repoID string) string {
	return filepath.Join(s.RepoDir(repoID), "integration_worktrees")
}

// IntegrationWorktreeDir returns the directory for a specific integration worktree.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>/
func (s *Store) IntegrationWorktreeDir(repoID, worktreeID string) string {
	return filepath.Join(s.IntegrationWorktreesDir(repoID), worktreeID)
}

// IntegrationWorktreeMetaPath returns the path to an integration worktree's meta.json.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>/meta.json
func (s *Store) IntegrationWorktreeMetaPath(repoID, worktreeID string) string {
	return filepath.Join(s.IntegrationWorktreeDir(repoID, worktreeID), "meta.json")
}

// IntegrationWorktreeTreePath returns the path to an integration worktree's tree directory.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>/tree/
func (s *Store) IntegrationWorktreeTreePath(repoID, worktreeID string) string {
	return filepath.Join(s.IntegrationWorktreeDir(repoID, worktreeID), "tree")
}
