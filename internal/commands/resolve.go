// Package commands implements agency CLI commands.
package commands

import (
	stderrors "errors"
	"fmt"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// resolveRunByNameOrID resolves a run identifier (name or run_id) to a specific run.
// It supports:
//   - Exact name match among active (non-archived) runs
//   - Exact run_id match
//   - Unique run_id prefix match
//
// Parameters:
//   - input: the user-provided identifier (name or run_id)
//   - records: all discovered runs from store.ScanAllRuns or store.ScanRepoRuns
//
// Returns the resolved RunRef, the matching RunRecord, and any error.
func resolveRunByNameOrID(input string, records []store.RunRecord) (ids.RunRef, *store.RunRecord, error) {
	// Build refs with Name populated
	refs := make([]ids.RunRef, len(records))
	for i, rec := range records {
		refs[i] = ids.RunRef{
			RepoID: rec.RepoID,
			RunID:  rec.RunID,
			Name:   rec.Name,
			Broken: rec.Broken,
		}
	}

	// isActive predicate: a run is active if it's not archived
	// A run is archived if:
	// - meta.Archive.ArchivedAt is set, OR
	// - the record is broken (can't determine archive state, treat as inactive for name matching)
	isActive := func(ref ids.RunRef) bool {
		if ref.Broken {
			return false
		}
		// Find the record to check archive state
		for i := range records {
			if records[i].RunID == ref.RunID && records[i].RepoID == ref.RepoID {
				if records[i].Meta != nil && records[i].Meta.Archive != nil && records[i].Meta.Archive.ArchivedAt != "" {
					return false
				}
				return true
			}
		}
		return false
	}

	// Resolve using name-aware resolution
	resolved, err := ids.ResolveRunRefWithName(input, refs, isActive)
	if err != nil {
		return ids.RunRef{}, nil, err
	}

	// Find the matching record
	for i := range records {
		if records[i].RunID == resolved.RunID && records[i].RepoID == resolved.RepoID {
			return resolved, &records[i], nil
		}
	}

	// Should not happen if resolver worked correctly
	return ids.RunRef{}, nil, stderrors.New("resolved run not found in records")
}

// handleResolveErr converts ids resolution errors to agency errors.
// Returns the appropriate agency error for the given resolution error.
func handleResolveErr(err error, input string) error {
	var notFound *ids.ErrNotFound
	if stderrors.As(err, &notFound) {
		return errors.New(errors.ERunNotFound, fmt.Sprintf("run not found: %s", input))
	}

	var ambiguous *ids.ErrAmbiguous
	if stderrors.As(err, &ambiguous) {
		candidates := make([]string, len(ambiguous.Candidates))
		for i, c := range ambiguous.Candidates {
			// Include name if available for better UX
			if c.Name != "" {
				candidates[i] = fmt.Sprintf("%s (%s)", c.RunID, c.Name)
			} else {
				candidates[i] = c.RunID
			}
		}
		return errors.NewWithDetails(
			errors.ERunIDAmbiguous,
			fmt.Sprintf("ambiguous identifier %q matches multiple runs: %s", input, strings.Join(candidates, ", ")),
			map[string]string{"input": input},
		)
	}

	return errors.Wrap(errors.EInternal, "failed to resolve run", err)
}

// resolveRunInRepo resolves a run identifier within a specific repo.
// This is used by commands that require being inside a repo (attach, resume, stop, kill, clean).
//
// Parameters:
//   - input: the user-provided identifier (name or run_id)
//   - repoID: the repo to search within
//   - dataDir: the agency data directory
//
// Returns the resolved RunRef, the matching RunRecord, and any error.
func resolveRunInRepo(input, repoID, dataDir string) (ids.RunRef, *store.RunRecord, error) {
	// Scan runs in this repo only
	records, err := store.ScanRunsForRepo(dataDir, repoID)
	if err != nil {
		return ids.RunRef{}, nil, errors.Wrap(errors.EInternal, "failed to scan repo runs", err)
	}

	resolved, record, err := resolveRunByNameOrID(input, records)
	if err != nil {
		return ids.RunRef{}, nil, handleResolveErr(err, input)
	}

	return resolved, record, nil
}

// resolveRunGlobal resolves a run identifier across all repos.
// This is used by commands that don't require being inside a repo (show, push, open, verify).
//
// Parameters:
//   - input: the user-provided identifier (name or run_id)
//   - dataDir: the agency data directory
//
// Returns the resolved RunRef, the matching RunRecord, and any error.
func resolveRunGlobal(input, dataDir string) (ids.RunRef, *store.RunRecord, error) {
	// Scan all runs
	records, err := store.ScanAllRuns(dataDir)
	if err != nil {
		return ids.RunRef{}, nil, errors.Wrap(errors.EInternal, "failed to scan runs", err)
	}

	resolved, record, err := resolveRunByNameOrID(input, records)
	if err != nil {
		return ids.RunRef{}, nil, handleResolveErr(err, input)
	}

	return resolved, record, nil
}
