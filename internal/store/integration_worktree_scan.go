// Package store provides persistence for agency data.
// This file implements filesystem-based integration worktree discovery (Slice 8 PR-01).
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// IntegrationWorktreeRecord represents a discovered integration worktree with its parsed metadata.
type IntegrationWorktreeRecord struct {
	// WorktreeID is the worktree_id from the directory name (canonical identity).
	WorktreeID string

	// RepoID is the repo_id (from context, not directory).
	RepoID string

	// Name is the worktree name from meta.json. Empty if Broken==true.
	Name string

	// Broken indicates meta.json is unreadable or invalid.
	// When true, Meta is nil but WorktreeID is still populated from dir name.
	Broken bool

	// Meta is the parsed meta.json. Nil if Broken==true.
	Meta *IntegrationWorktreeMeta

	// WorktreeDir is the absolute path to the worktree record directory:
	// ${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>
	WorktreeDir string
}

// ScanIntegrationWorktreesForRepo discovers integration worktrees for a single repo_id.
// Returns records sorted by created_at ascending, then worktree_id.
// Missing directories result in empty slice (not error).
// Corrupt meta.json results in an IntegrationWorktreeRecord with Broken=true.
func ScanIntegrationWorktreesForRepo(dataDir, repoID string) ([]IntegrationWorktreeRecord, error) {
	worktreesDir := filepath.Join(dataDir, "repos", repoID, "integration_worktrees")

	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var records []IntegrationWorktreeRecord

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		worktreeID := entry.Name()
		worktreeDir := filepath.Join(worktreesDir, worktreeID)
		metaPath := filepath.Join(worktreeDir, "meta.json")

		record := IntegrationWorktreeRecord{
			WorktreeID:  worktreeID,
			RepoID:      repoID,
			WorktreeDir: worktreeDir,
		}

		// Try to read and parse meta.json
		data, err := os.ReadFile(metaPath)
		if err != nil {
			// Missing or unreadable - mark as broken
			record.Broken = true
			records = append(records, record)
			continue
		}

		var meta IntegrationWorktreeMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			// Invalid JSON - mark as broken
			record.Broken = true
			records = append(records, record)
			continue
		}

		// Validate minimal required fields for non-broken status
		if meta.SchemaVersion == "" || meta.CreatedAt == "" {
			record.Broken = true
			records = append(records, record)
			continue
		}

		record.Meta = &meta
		record.Name = meta.Name
		records = append(records, record)
	}

	// Sort by created_at ascending, then worktree_id
	sort.Slice(records, func(i, j int) bool {
		// Broken records sort last
		if records[i].Broken != records[j].Broken {
			return !records[i].Broken // non-broken first
		}
		if records[i].Broken && records[j].Broken {
			return records[i].WorktreeID < records[j].WorktreeID
		}

		// Parse created_at timestamps
		ti, erri := time.Parse(time.RFC3339, records[i].Meta.CreatedAt)
		tj, errj := time.Parse(time.RFC3339, records[j].Meta.CreatedAt)

		// If either timestamp is invalid, fall back to worktree_id
		if erri != nil || errj != nil {
			return records[i].WorktreeID < records[j].WorktreeID
		}

		// Sort by created_at ascending
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}

		// Tie-breaker: worktree_id ascending
		return records[i].WorktreeID < records[j].WorktreeID
	})

	return records, nil
}

// ScanAllIntegrationWorktrees discovers integration worktrees across all repos.
// Returns records sorted by RepoID asc, then created_at asc, then WorktreeID asc.
func ScanAllIntegrationWorktrees(dataDir string) ([]IntegrationWorktreeRecord, error) {
	reposDir := filepath.Join(dataDir, "repos")

	entries, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var records []IntegrationWorktreeRecord

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoID := entry.Name()
		repoRecords, err := ScanIntegrationWorktreesForRepo(dataDir, repoID)
		if err != nil {
			// Skip repos with errors (e.g., permission denied)
			continue
		}
		records = append(records, repoRecords...)
	}

	// Sort by RepoID, then created_at, then WorktreeID
	sort.Slice(records, func(i, j int) bool {
		if records[i].RepoID != records[j].RepoID {
			return records[i].RepoID < records[j].RepoID
		}
		// Broken records sort last within repo
		if records[i].Broken != records[j].Broken {
			return !records[i].Broken
		}
		if records[i].Broken && records[j].Broken {
			return records[i].WorktreeID < records[j].WorktreeID
		}

		ti, erri := time.Parse(time.RFC3339, records[i].Meta.CreatedAt)
		tj, errj := time.Parse(time.RFC3339, records[j].Meta.CreatedAt)

		if erri != nil || errj != nil {
			return records[i].WorktreeID < records[j].WorktreeID
		}

		if !ti.Equal(tj) {
			return ti.Before(tj)
		}

		return records[i].WorktreeID < records[j].WorktreeID
	})

	return records, nil
}
