// Package store provides persistence for repo_index.json, repo.json, and meta.json files.
// This file implements filesystem-based run discovery for s2 observability commands.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// RepoInfo holds minimal repo identity information for joining runs to repos.
// This is a best-effort join; may be nil if repo.json is missing/corrupt.
type RepoInfo struct {
	RepoKey   string  `json:"repo_key"`
	OriginURL *string `json:"origin_url,omitempty"`
}

// RunRecord represents a discovered run with its parsed metadata.
// This is the primary output of scanning and contains:
// - Identity derived from directory names (canonical)
// - Parsed meta.json (nil if broken)
// - Best-effort repo.json join (nil if missing/corrupt)
type RunRecord struct {
	// RepoID is the repo_id from the directory name (canonical identity).
	RepoID string

	// RunID is the run_id from the directory name (canonical identity).
	RunID string

	// Broken indicates meta.json is unreadable or invalid.
	// When true, Meta is nil but RepoID/RunID are still populated from dir names.
	Broken bool

	// Meta is the parsed meta.json. Nil if Broken==true.
	Meta *RunMeta

	// Repo is the best-effort join to repo.json. Nil if missing/corrupt.
	// Does not affect Broken status.
	Repo *RepoInfo

	// RunDir is the absolute path to the run directory:
	// ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>
	RunDir string
}

// repoJoinCache caches repo.json lookups to avoid repeated reads.
type repoJoinCache struct {
	dataDir string
	cache   map[string]*RepoInfo // keyed by repo_id; nil value means missing/corrupt
	loaded  map[string]bool      // tracks which repo_ids we've attempted to load
}

func newRepoJoinCache(dataDir string) *repoJoinCache {
	return &repoJoinCache{
		dataDir: dataDir,
		cache:   make(map[string]*RepoInfo),
		loaded:  make(map[string]bool),
	}
}

// get returns cached RepoInfo for a repo_id, loading if not cached.
// Returns nil for missing/corrupt repo.json (does not error).
func (c *repoJoinCache) get(repoID string) *RepoInfo {
	if c.loaded[repoID] {
		return c.cache[repoID]
	}

	c.loaded[repoID] = true

	// Try to load repo.json
	repoJSONPath := filepath.Join(c.dataDir, "repos", repoID, "repo.json")
	data, err := os.ReadFile(repoJSONPath)
	if err != nil {
		// Missing or unreadable - return nil without caching error
		return nil
	}

	var rec RepoRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		// Corrupt JSON - return nil
		return nil
	}

	// Schema version check is lenient here - we just extract what we need
	info := &RepoInfo{
		RepoKey: rec.RepoKey,
	}
	if rec.OriginURL != "" {
		info.OriginURL = &rec.OriginURL
	}

	c.cache[repoID] = info
	return info
}

// ScanAllRuns discovers runs across all repos by scanning the filesystem.
// Returns records sorted by RepoID asc, then RunID asc (stable order).
// Missing directories result in empty slice (not error).
// Corrupt meta.json results in a RunRecord with Broken=true.
func ScanAllRuns(dataDir string) ([]RunRecord, error) {
	reposDir := filepath.Join(dataDir, "repos")

	// List repo directories
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	cache := newRepoJoinCache(dataDir)
	var records []RunRecord

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoID := entry.Name()
		repoRecords, err := scanRepoRuns(dataDir, repoID, cache)
		if err != nil {
			// Skip repos with errors (e.g., permission denied)
			continue
		}
		records = append(records, repoRecords...)
	}

	// Sort by RepoID, then RunID for stable output
	sort.Slice(records, func(i, j int) bool {
		if records[i].RepoID != records[j].RepoID {
			return records[i].RepoID < records[j].RepoID
		}
		return records[i].RunID < records[j].RunID
	})

	return records, nil
}

// ScanRunsForRepo discovers runs for a single repo_id.
// Returns records sorted by RunID asc (stable order).
// Missing directories result in empty slice (not error).
// Corrupt meta.json results in a RunRecord with Broken=true.
func ScanRunsForRepo(dataDir, repoID string) ([]RunRecord, error) {
	cache := newRepoJoinCache(dataDir)
	records, err := scanRepoRuns(dataDir, repoID, cache)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	// Sort by RunID for stable output
	sort.Slice(records, func(i, j int) bool {
		return records[i].RunID < records[j].RunID
	})

	return records, nil
}

// scanRepoRuns scans runs for a single repo, using the provided cache.
func scanRepoRuns(dataDir, repoID string, cache *repoJoinCache) ([]RunRecord, error) {
	runsDir := filepath.Join(dataDir, "repos", repoID, "runs")

	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var records []RunRecord
	repoInfo := cache.get(repoID)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		runID := entry.Name()
		runDir := filepath.Join(runsDir, runID)
		metaPath := filepath.Join(runDir, "meta.json")

		record := RunRecord{
			RepoID: repoID,
			RunID:  runID,
			RunDir: runDir,
			Repo:   repoInfo,
		}

		// Try to read and parse meta.json
		data, err := os.ReadFile(metaPath)
		if err != nil {
			// Missing or unreadable - mark as broken
			record.Broken = true
			records = append(records, record)
			continue
		}

		var meta RunMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			// Invalid JSON - mark as broken
			record.Broken = true
			records = append(records, record)
			continue
		}

		// Validate minimal required fields for non-broken status
		// SchemaVersion must be present and CreatedAt must be non-empty
		if meta.SchemaVersion == "" || meta.CreatedAt == "" {
			record.Broken = true
			records = append(records, record)
			continue
		}

		record.Meta = &meta
		records = append(records, record)
	}

	return records, nil
}

// LoadRepoIndexForScan loads repo_index.json for display-only purposes.
// Different from Store.LoadRepoIndex:
//   - Returns (*RepoIndex, nil) if file is missing (not empty index)
//   - Returns (nil, error) if JSON is invalid
//   - Accepts both { "schema_version": "1.0", "repos": {...} } format
//     and legacy { "entries": {...} } format for compatibility
func LoadRepoIndexForScan(dataDir string) (*RepoIndex, error) {
	path := filepath.Join(dataDir, "repo_index.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	// Try standard format first
	var idx RepoIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}

	// If standard format worked but repos is nil, try legacy format
	if idx.Repos == nil {
		// Try to parse as legacy format with "entries" key
		var legacy struct {
			Entries map[string]RepoIndexEntry `json:"entries"`
		}
		if json.Unmarshal(data, &legacy) == nil && legacy.Entries != nil {
			idx.Repos = legacy.Entries
		}
	}

	// Initialize empty map if still nil
	if idx.Repos == nil {
		idx.Repos = make(map[string]RepoIndexEntry)
	}

	return &idx, nil
}

// PickRepoRoot selects a repo root path for DISPLAY ONLY.
// This is used for printing paths and default scope selection, not for
// run discovery or run_id resolution.
//
// Preference order (per s2_spec):
// 1. cwdRepoRoot if provided and exists on disk (as directory)
// 2. First entry in Paths that exists on disk (most recent first)
// 3. nil
//
// Parameters:
// - repoKey: the repo_key to look up in the index
// - cwdRepoRoot: optional current working directory repo root (for preference)
// - idx: the loaded repo index (may be nil)
func PickRepoRoot(repoKey string, cwdRepoRoot *string, idx *RepoIndex) *string {
	// Preference 1: cwdRepoRoot if provided and exists
	if cwdRepoRoot != nil && *cwdRepoRoot != "" {
		if dirExists(*cwdRepoRoot) {
			return cwdRepoRoot
		}
	}

	// If no index or no entry for this repo_key, return nil
	if idx == nil {
		return nil
	}
	entry, ok := idx.Repos[repoKey]
	if !ok {
		return nil
	}

	// Preference 2/3: First existing path from Paths (most recent first)
	for _, p := range entry.Paths {
		if dirExists(p) {
			result := p
			return &result
		}
	}

	return nil
}

// dirExists checks if a path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
