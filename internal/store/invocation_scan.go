// Package store provides persistence for agency data.
// This file implements filesystem-based invocation discovery.
package store

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
)

// InvocationRecord represents a discovered invocation with its parsed metadata.
type InvocationRecord struct {
	// InvocationID is the invocation_id from the directory name (canonical identity).
	InvocationID string

	// RepoID is the repo_id (from context, not directory).
	RepoID string

	// IntegrationWorktreeID is the target integration worktree from meta.json.
	// Empty if Broken==true.
	IntegrationWorktreeID string

	// InvocationName is the optional invocation name from meta.json.
	// Empty if Broken==true or if not set.
	InvocationName string

	// Broken indicates meta.json is unreadable or invalid.
	// When true, Meta is nil but InvocationID is still populated from dir name.
	Broken bool

	// Meta is the parsed meta.json. Nil if Broken==true.
	Meta *InvocationMeta

	// InvocationDir is the absolute path to the invocation record directory:
	// ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>
	InvocationDir string

	// SandboxExists indicates whether the sandbox tree directory exists.
	// Useful for detecting orphaned invocations or landed/discarded state.
	SandboxExists bool
}

// ScanInvocationsForRepo discovers invocations for a single repo_id.
// Returns records sorted by started_at descending (newest first), then invocation_id.
// Missing directories result in empty slice (not error).
// Corrupt meta.json results in an InvocationRecord with Broken=true.
func (s *Store) ScanInvocationsForRepo(repoID string) ([]InvocationRecord, error) {
	invocationsDir := s.InvocationsDir(repoID)

	entries, err := os.ReadDir(invocationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var records []InvocationRecord

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		invocationID := entry.Name()
		invocationDir := filepath.Join(invocationsDir, invocationID)
		metaPath := filepath.Join(invocationDir, "meta.json")

		record := InvocationRecord{
			InvocationID:  invocationID,
			RepoID:        repoID,
			InvocationDir: invocationDir,
		}

		// Try to read and parse meta.json
		data, err := os.ReadFile(metaPath)
		if err != nil {
			// Missing or unreadable - mark as broken
			record.Broken = true
			records = append(records, record)
			continue
		}

		var meta InvocationMeta
		if err := decodeStrictJSON(data, &meta); err != nil {
			// Invalid JSON - mark as broken
			record.Broken = true
			records = append(records, record)
			continue
		}

		fields, err := strictJSONObjectFields(data)
		if err != nil {
			record.Broken = true
			records = append(records, record)
			continue
		}
		if err := validateInvocationMeta(meta, invocationID, metaPath, fields); err != nil {
			record.Broken = true
			records = append(records, record)
			continue
		}

		record.Meta = &meta
		record.IntegrationWorktreeID = meta.IntegrationWorktreeID
		record.InvocationName = meta.InvocationName
		if _, err := os.Stat(meta.SandboxPath); err == nil {
			record.SandboxExists = true
		}
		records = append(records, record)
	}

	// Sort by started_at descending (newest first), then invocation_id
	slices.SortFunc(records, func(a, b InvocationRecord) int {
		// Broken records sort last
		if a.Broken != b.Broken {
			if a.Broken {
				return 1
			}
			return -1
		}
		if a.Broken && b.Broken {
			return cmp.Compare(b.InvocationID, a.InvocationID)
		}

		// Sort by started_at descending (newest first)
		if a.Meta.StartedAt != b.Meta.StartedAt {
			return cmp.Compare(b.Meta.StartedAt, a.Meta.StartedAt)
		}

		// Tie-breaker: invocation_id descending
		return cmp.Compare(b.InvocationID, a.InvocationID)
	})

	return records, nil
}
