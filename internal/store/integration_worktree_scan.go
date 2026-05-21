// Package store provides persistence for agency data.
// This file implements filesystem-based integration worktree discovery.
package store

import (
	"os"
	"path/filepath"
	"sort"
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
		if err := decodeStrictJSON(data, &meta); err != nil {
			// Invalid JSON - mark as broken
			record.Broken = true
			records = append(records, record)
			continue
		}

		if err := validateIntegrationWorktreeMeta(meta, repoID, worktreeID, metaPath); err != nil {
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

		// Sort by created_at ascending
		if records[i].Meta.CreatedAt != records[j].Meta.CreatedAt {
			return records[i].Meta.CreatedAt < records[j].Meta.CreatedAt
		}

		// Tie-breaker: worktree_id ascending
		return records[i].WorktreeID < records[j].WorktreeID
	})

	return records, nil
}
