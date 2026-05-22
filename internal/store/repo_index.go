package store

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// SchemaVersion is the current schema version for store files.
const SchemaVersion = "2.0"

// RepoIndex represents the repo_index.json file.
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
// If the file is missing, returns an empty index with the current schema version.
// Returns E_STORE_CORRUPT if the JSON is invalid or schema_version is missing/invalid.
func (s *Store) LoadRepoIndex() (RepoIndex, error) {
	path := s.RepoIndexPath()

	data, err := s.fsys.ReadFile(path)
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
	if err := decodeStrictJSON(data, &idx); err != nil {
		return RepoIndex{}, errors.Wrap(errors.EStoreCorrupt, "invalid json in repo_index.json", err)
	}
	if err := validateRepoIndex(path, idx); err != nil {
		return RepoIndex{}, err
	}

	return idx, nil
}

// UpsertRepoIndexEntry updates or creates an entry in the repo index.
// - If the entry exists: updates last_seen_at and adds absPath (deduped, sorted)
// - If the entry is new: creates it with the given values
// absPath is normalized via filepath.Clean.
// Paths are de-duplicated case-sensitively and kept sorted for stable diffs.
func (s *Store) UpsertRepoIndexEntry(idx RepoIndex, repoKey, repoID, absPath string) RepoIndex {
	now := s.nowTime().UTC().Format("2006-01-02T15:04:05Z")
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

	// Deduplicate and keep sorted for stable diffs.
	pathSet := make(map[string]bool, len(entry.Paths)+1)
	pathSet[absPath] = true
	for _, p := range entry.Paths {
		pathSet[p] = true
	}
	newPaths := slices.Sorted(maps.Keys(pathSet))
	entry.Paths = newPaths

	idx.Repos[repoKey] = entry
	return idx
}

// SaveRepoIndex writes repo_index.json atomically.
// Creates the data directory if it doesn't exist.
func (s *Store) SaveRepoIndex(idx RepoIndex) error {
	// Ensure data directory exists
	if err := s.fsys.MkdirAll(s.DataDir, 0o700); err != nil {
		return errors.Wrap(errors.EStoreCorrupt, "failed to create data directory", err)
	}
	if err := s.fsys.Chmod(s.DataDir, 0o700); err != nil {
		return errors.Wrap(errors.EStoreCorrupt, "failed to enforce data directory permissions", err)
	}

	// Marshal with indentation for human readability
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return errors.Wrap(errors.EStoreCorrupt, "failed to marshal repo_index.json", err)
	}

	// Add trailing newline
	data = append(data, '\n')

	path := s.RepoIndexPath()
	if err := fs.WriteFileAtomic(s.fsys, path, data, 0o600); err != nil {
		return errors.Wrap(errors.EStoreCorrupt, "failed to write repo_index.json", err)
	}

	return nil
}

func validateRepoIndex(path string, idx RepoIndex) error {
	if err := validateSchemaVersion(idx.SchemaVersion, "repo_index.json", path); err != nil {
		return err
	}
	if idx.Repos == nil {
		return errors.New(errors.EStoreCorrupt, "repo_index.json: missing repos")
	}
	for repoKey, entry := range idx.Repos {
		if repoKey == "" || entry.RepoID == "" || entry.LastSeenAt == "" || len(entry.Paths) == 0 {
			return errors.New(errors.EStoreCorrupt, "repo_index.json: repo entry missing required fields")
		}
		if entry.RepoID != filepath.Base(filepath.Clean(filepath.Join("repos", entry.RepoID))) {
			return errors.New(errors.EStoreCorrupt, "repo_index.json: repo entry has invalid repo_id")
		}
		if err := validateCanonicalStoreTimestamp("repo_index.json", "repo_index_path", path, "last_seen_at", entry.LastSeenAt); err != nil {
			return err
		}
		for _, path := range entry.Paths {
			if path == "" || !filepath.IsAbs(path) {
				return errors.New(errors.EStoreCorrupt, "repo_index.json: repo entry paths must be absolute")
			}
		}
	}
	return nil
}
