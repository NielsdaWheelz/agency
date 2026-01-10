package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// SchemaVersion is the current schema version for all store files.
const SchemaVersion = "1.0"

// RepoIndex represents the repo_index.json file.
// This is the public contract for v1.
type RepoIndex struct {
	SchemaVersion string                    `json:"schema_version"`
	Repos         map[string]RepoIndexEntry `json:"repos"`
}

// RepoIndexEntry represents an entry in the repo index.
type RepoIndexEntry struct {
	RepoID     string   `json:"repo_id"`
	Paths      []string `json:"paths"`
	LastSeenAt string   `json:"last_seen_at"`
}

// LoadRepoIndex reads repo_index.json from the data directory.
// If the file is missing, returns an empty index with schema_version "1.0".
// Returns E_STORE_CORRUPT if the JSON is invalid or schema_version is missing/invalid.
func (s *Store) LoadRepoIndex() (RepoIndex, error) {
	path := s.RepoIndexPath()

	data, err := s.FS.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty index for missing file
			return RepoIndex{
				SchemaVersion: SchemaVersion,
				Repos:         make(map[string]RepoIndexEntry),
			}, nil
		}
		return RepoIndex{}, errors.Wrap(errors.EStoreCorrupt, "failed to read repo_index.json", err)
	}

	var idx RepoIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return RepoIndex{}, errors.Wrap(errors.EStoreCorrupt, "invalid json in repo_index.json", err)
	}

	// Validate schema_version
	if idx.SchemaVersion == "" {
		return RepoIndex{}, errors.New(errors.EStoreCorrupt, "repo_index.json: missing schema_version")
	}
	if idx.SchemaVersion != SchemaVersion {
		return RepoIndex{}, errors.New(errors.EStoreCorrupt, "repo_index.json: unsupported schema_version: "+idx.SchemaVersion)
	}

	// Initialize map if nil (empty repos object in JSON)
	if idx.Repos == nil {
		idx.Repos = make(map[string]RepoIndexEntry)
	}

	return idx, nil
}

// UpsertRepoIndexEntry updates or creates an entry in the repo index.
// - If the entry exists: updates last_seen_at and moves absPath to front of paths
// - If the entry is new: creates it with the given values
// absPath is normalized via filepath.Clean.
// Paths are de-duplicated case-sensitively.
func (s *Store) UpsertRepoIndexEntry(idx RepoIndex, repoKey, repoID, absPath string) RepoIndex {
	now := s.Now().UTC().Format("2006-01-02T15:04:05Z")
	absPath = filepath.Clean(absPath)

	entry, exists := idx.Repos[repoKey]
	if !exists {
		// Create new entry
		idx.Repos[repoKey] = RepoIndexEntry{
			RepoID:     repoID,
			Paths:      []string{absPath},
			LastSeenAt: now,
		}
		return idx
	}

	// Update existing entry
	entry.LastSeenAt = now

	// Update paths: move absPath to front, de-duplicate
	newPaths := []string{absPath}
	for _, p := range entry.Paths {
		if p != absPath {
			newPaths = append(newPaths, p)
		}
	}
	entry.Paths = newPaths

	idx.Repos[repoKey] = entry
	return idx
}

// SaveRepoIndex writes repo_index.json atomically.
// Creates the data directory if it doesn't exist.
func (s *Store) SaveRepoIndex(idx RepoIndex) error {
	// Ensure data directory exists
	if err := s.FS.MkdirAll(s.DataDir, 0755); err != nil {
		return errors.Wrap(errors.EStoreCorrupt, "failed to create data directory", err)
	}

	// Marshal with indentation for human readability
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return errors.Wrap(errors.EStoreCorrupt, "failed to marshal repo_index.json", err)
	}

	// Add trailing newline
	data = append(data, '\n')

	path := s.RepoIndexPath()
	if err := fs.WriteFileAtomic(s.FS, path, data, 0644); err != nil {
		return errors.Wrap(errors.EStoreCorrupt, "failed to write repo_index.json", err)
	}

	return nil
}
