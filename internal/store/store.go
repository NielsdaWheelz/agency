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
