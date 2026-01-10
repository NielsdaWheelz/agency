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
