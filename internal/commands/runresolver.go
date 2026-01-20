// Package commands implements agency CLI commands.
package commands

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// RunResolutionContext holds the context for run resolution.
type RunResolutionContext struct {
	// DataDir is the agency data directory.
	DataDir string

	// CWDRepoID is the repo_id derived from CWD (empty if not inside a repo).
	CWDRepoID string

	// CWDRepoRoot is the repo root path derived from CWD (empty if not inside a repo).
	CWDRepoRoot string

	// ExplicitRepoID is the repo_id from --repo flag (empty if not provided).
	ExplicitRepoID string

	// ExplicitRepoRoot is the repo root from --repo flag (empty if not provided).
	ExplicitRepoRoot string
}

// ResolvedRun contains the resolved run information.
type ResolvedRun struct {
	// RunRef is the resolved run reference.
	ids.RunRef

	// Record is the matching run record.
	Record *store.RunRecord

	// RepoID is the repo_id containing the run.
	RepoID string

	// RepoRoot is the best-effort repo root path for the run.
	// May be derived from CWD, explicit --repo, or repo_index.
	RepoRoot string
}

// Regular expressions for run_id format.
// Exact run_id: 14 digits (yyyymmddhhmmss) + hyphen + 4 hex chars (e.g., "20260109013207-a3f2")
// Prefix patterns (must support partial prefixes like "20260115" or "20260115-a"):
// - Just digits: could be a timestamp prefix
// - Digits + hyphen + optional hex: partial run_id
var (
	exactRunIDRegex  = regexp.MustCompile(`^\d{8,14}-[a-f0-9]{4}$`)
	prefixRunIDRegex = regexp.MustCompile(`^\d{8,14}(-[a-f0-9]{0,4})?$`)
)

// isRunIDFormat checks if input looks like a run_id (exact or prefix).
// This includes partial prefixes like "20260115" (just timestamp digits).
func isRunIDFormat(input string) bool {
	return exactRunIDRegex.MatchString(input) || prefixRunIDRegex.MatchString(input)
}

// ResolveRunContext builds a RunResolutionContext from the current environment.
// If repoPath is provided (from --repo flag), it normalizes and validates it.
// If CWD is inside a repo, it captures that for name resolution preference.
func ResolveRunContext(ctx context.Context, cr agencyexec.CommandRunner, cwd string, repoPath string) (*RunResolutionContext, error) {
	// Get home directory for path resolution
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}

	// Resolve data directory
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	dataDir := dirs.DataDir

	rctx := &RunResolutionContext{
		DataDir: dataDir,
	}

	// Handle explicit --repo flag
	if repoPath != "" {
		repoRoot, repoID, err := normalizeRepoPath(ctx, cr, repoPath)
		if err != nil {
			return nil, err
		}
		rctx.ExplicitRepoRoot = repoRoot
		rctx.ExplicitRepoID = repoID
	}

	// Try to get CWD repo context (best-effort, errors are not fatal)
	if repoRoot, err := git.GetRepoRoot(ctx, cr, cwd); err == nil {
		originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
		repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)
		rctx.CWDRepoRoot = repoRoot.Path
		rctx.CWDRepoID = repoIdentity.RepoID
	}

	return rctx, nil
}

// normalizeRepoPath normalizes and validates a --repo path.
// Returns the repo root and repo_id, or an error if invalid.
func normalizeRepoPath(ctx context.Context, cr agencyexec.CommandRunner, repoPath string) (string, string, error) {
	// Check if path exists
	info, err := os.Stat(repoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", errors.NewWithDetails(
				errors.EInvalidRepoPath,
				fmt.Sprintf("--repo path does not exist: %s", repoPath),
				map[string]string{"path": repoPath},
			)
		}
		return "", "", errors.Wrap(errors.EInvalidRepoPath, "failed to stat --repo path", err)
	}

	// If it's a file, use the directory
	if !info.IsDir() {
		repoPath = strings.TrimSuffix(repoPath, "/"+info.Name())
	}

	// Get repo root using git rev-parse
	repoRoot, err := git.GetRepoRoot(ctx, cr, repoPath)
	if err != nil {
		return "", "", errors.NewWithDetails(
			errors.EInvalidRepoPath,
			fmt.Sprintf("--repo path is not inside a git repository: %s", repoPath),
			map[string]string{"path": repoPath},
		)
	}

	// Derive repo identity
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	return repoRoot.Path, repoIdentity.RepoID, nil
}

// ResolveRun resolves a run reference using the resolution algorithm from the spec.
//
// Resolution rules (in priority order):
//  1. Exact run_id match - resolve globally (--repo ignored)
//  2. Unique run_id prefix - resolve globally (--repo ignored)
//  3. Explicit repo scope (--repo) - resolve name within that repo only
//  4. CWD repo scope - resolve name within CWD repo first
//  5. Global name resolution - resolve name across all repos
//
// Archived runs are excluded from name matching but can be resolved by run_id.
func ResolveRun(rctx *RunResolutionContext, input string) (*ResolvedRun, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New(errors.EUsage, "run reference is required")
	}

	// Scan all runs for resolution
	allRecords, err := store.ScanAllRuns(rctx.DataDir)
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to scan runs", err)
	}

	// If input looks like a run_id (exact or prefix), try global ID resolution first
	// This bypasses name resolution and ignores --repo
	if isRunIDFormat(input) {
		if result, err := resolveByRunID(rctx, input, allRecords); err == nil {
			return result, nil
		} else if !stderrors.Is(err, errNotFoundByID) {
			// Return actual errors (ambiguous, etc.) but not "not found"
			// because we'll fall through to name resolution
			return nil, err
		}
		// Not found by ID - fall through to name resolution
	}

	// Name-based resolution
	return resolveByName(rctx, input, allRecords)
}

// errNotFoundByID is a sentinel error for "not found by ID" (internal use).
var errNotFoundByID = stderrors.New("not found by run_id")

// resolveByRunID resolves input as a run_id (exact or prefix) globally.
func resolveByRunID(rctx *RunResolutionContext, input string, records []store.RunRecord) (*ResolvedRun, error) {
	// Build refs for ID resolution (include all runs, even archived)
	refs := make([]ids.RunRef, len(records))
	for i, rec := range records {
		refs[i] = ids.RunRef{
			RepoID: rec.RepoID,
			RunID:  rec.RunID,
			Name:   rec.Name,
			Broken: rec.Broken,
		}
	}

	// Use existing ID resolution (exact then prefix)
	resolved, err := ids.ResolveRunRef(input, refs)
	if err != nil {
		var notFound *ids.ErrNotFound
		if stderrors.As(err, &notFound) {
			return nil, errNotFoundByID
		}
		// Convert ambiguous error
		var ambiguous *ids.ErrAmbiguous
		if stderrors.As(err, &ambiguous) {
			return nil, formatAmbiguousError(input, ambiguous.Candidates, "run_id")
		}
		return nil, errors.Wrap(errors.EInternal, "failed to resolve run", err)
	}

	// Find the matching record
	for i := range records {
		if records[i].RunID == resolved.RunID && records[i].RepoID == resolved.RepoID {
			return buildResolvedRun(rctx, resolved, &records[i])
		}
	}

	return nil, errNotFoundByID
}

// resolveByName resolves input as a run name using the scoping rules.
func resolveByName(rctx *RunResolutionContext, input string, allRecords []store.RunRecord) (*ResolvedRun, error) {
	// Build active (non-archived) records for name matching
	activeRecords := filterActiveRecords(allRecords)

	// Build refs for name matching
	activeRefs := make([]ids.RunRef, len(activeRecords))
	for i, rec := range activeRecords {
		activeRefs[i] = ids.RunRef{
			RepoID: rec.RepoID,
			RunID:  rec.RunID,
			Name:   rec.Name,
			Broken: rec.Broken,
		}
	}

	// Step 3: If --repo provided, scope to that repo only
	if rctx.ExplicitRepoID != "" {
		repoRecords := filterByRepoID(activeRecords, rctx.ExplicitRepoID)
		if len(repoRecords) == 0 {
			return nil, errors.New(errors.ERunNotFound, fmt.Sprintf("run not found: %s", input))
		}

		repoRefs := make([]ids.RunRef, len(repoRecords))
		for i, rec := range repoRecords {
			repoRefs[i] = ids.RunRef{
				RepoID: rec.RepoID,
				RunID:  rec.RunID,
				Name:   rec.Name,
				Broken: rec.Broken,
			}
		}

		resolved, err := resolveNameInRefs(input, repoRefs)
		if err != nil {
			return nil, err
		}

		for i := range repoRecords {
			if repoRecords[i].RunID == resolved.RunID && repoRecords[i].RepoID == resolved.RepoID {
				return buildResolvedRun(rctx, resolved, &repoRecords[i])
			}
		}
		return nil, errors.New(errors.ERunNotFound, fmt.Sprintf("run not found: %s", input))
	}

	// Step 4: If CWD is inside a repo, try that repo first
	if rctx.CWDRepoID != "" {
		cwdRecords := filterByRepoID(activeRecords, rctx.CWDRepoID)
		if len(cwdRecords) > 0 {
			cwdRefs := make([]ids.RunRef, len(cwdRecords))
			for i, rec := range cwdRecords {
				cwdRefs[i] = ids.RunRef{
					RepoID: rec.RepoID,
					RunID:  rec.RunID,
					Name:   rec.Name,
					Broken: rec.Broken,
				}
			}

			// Try to find exact name match in CWD repo
			var nameMatches []ids.RunRef
			for _, ref := range cwdRefs {
				if ref.Name == input {
					nameMatches = append(nameMatches, ref)
				}
			}

			if len(nameMatches) == 1 {
				for i := range cwdRecords {
					if cwdRecords[i].RunID == nameMatches[0].RunID && cwdRecords[i].RepoID == nameMatches[0].RepoID {
						return buildResolvedRun(rctx, nameMatches[0], &cwdRecords[i])
					}
				}
			}
			// If no match or ambiguous within CWD repo, fall through to global
		}
	}

	// Step 5: Global name resolution across all repos
	var globalNameMatches []ids.RunRef
	for _, ref := range activeRefs {
		if ref.Name == input {
			globalNameMatches = append(globalNameMatches, ref)
		}
	}

	if len(globalNameMatches) == 0 {
		return nil, errors.New(errors.ERunNotFound, fmt.Sprintf("run not found: %s", input))
	}

	if len(globalNameMatches) == 1 {
		for i := range activeRecords {
			if activeRecords[i].RunID == globalNameMatches[0].RunID && activeRecords[i].RepoID == globalNameMatches[0].RepoID {
				return buildResolvedRun(rctx, globalNameMatches[0], &activeRecords[i])
			}
		}
	}

	// Multiple matches - ambiguous
	return nil, formatAmbiguousError(input, globalNameMatches, "name")
}

// resolveNameInRefs resolves input as a name within a set of refs.
func resolveNameInRefs(input string, refs []ids.RunRef) (ids.RunRef, error) {
	var nameMatches []ids.RunRef
	for _, ref := range refs {
		if ref.Name == input {
			nameMatches = append(nameMatches, ref)
		}
	}

	switch len(nameMatches) {
	case 0:
		return ids.RunRef{}, errors.New(errors.ERunNotFound, fmt.Sprintf("run not found: %s", input))
	case 1:
		return nameMatches[0], nil
	default:
		return ids.RunRef{}, formatAmbiguousError(input, nameMatches, "name")
	}
}

// filterActiveRecords returns non-archived records.
func filterActiveRecords(records []store.RunRecord) []store.RunRecord {
	var active []store.RunRecord
	for _, rec := range records {
		if rec.Broken {
			continue
		}
		if rec.Meta != nil && rec.Meta.Archive != nil && rec.Meta.Archive.ArchivedAt != "" {
			continue
		}
		active = append(active, rec)
	}
	return active
}

// filterByRepoID returns records matching the given repo_id.
func filterByRepoID(records []store.RunRecord, repoID string) []store.RunRecord {
	var filtered []store.RunRecord
	for _, rec := range records {
		if rec.RepoID == repoID {
			filtered = append(filtered, rec)
		}
	}
	return filtered
}

// buildResolvedRun builds a ResolvedRun from the resolved ref and record.
func buildResolvedRun(rctx *RunResolutionContext, ref ids.RunRef, rec *store.RunRecord) (*ResolvedRun, error) {
	result := &ResolvedRun{
		RunRef: ref,
		Record: rec,
		RepoID: rec.RepoID,
	}

	// Determine best repo root path
	// Priority: explicit --repo > CWD repo (if matches) > worktree meta > repo_index recovery
	if rctx.ExplicitRepoID == rec.RepoID && rctx.ExplicitRepoRoot != "" {
		result.RepoRoot = rctx.ExplicitRepoRoot
	} else if rctx.CWDRepoID == rec.RepoID && rctx.CWDRepoRoot != "" {
		result.RepoRoot = rctx.CWDRepoRoot
	} else if rec.Meta != nil && rec.Meta.WorktreePath != "" {
		// Try to derive repo root from worktree path
		// For now, just use the worktree path (commands will use their own discovery)
		result.RepoRoot = ""
	}

	return result, nil
}

// formatAmbiguousError formats an ambiguous resolution error with disambiguation hints.
func formatAmbiguousError(input string, candidates []ids.RunRef, matchType string) error {
	var details []string
	for _, c := range candidates {
		if c.Name != "" {
			details = append(details, fmt.Sprintf("  %s (%s) in repo %s", c.RunID, c.Name, c.RepoID))
		} else {
			details = append(details, fmt.Sprintf("  %s in repo %s", c.RunID, c.RepoID))
		}
	}

	msg := fmt.Sprintf("ambiguous %s %q matches multiple runs:\n%s\nhint: use full run_id or --repo to disambiguate",
		matchType, input, strings.Join(details, "\n"))

	if matchType == "name" {
		return errors.NewWithDetails(
			errors.ERunRefAmbiguous,
			msg,
			map[string]string{"input": input},
		)
	}
	return errors.NewWithDetails(
		errors.ERunIDAmbiguous,
		msg,
		map[string]string{"input": input},
	)
}

// resolveRunGlobal is a simplified global resolution function for commands that
// don't need full scoping (CWD or --repo) and just resolve by run_id or name globally.
// Returns the resolved RunRef, matching RunRecord, and error.
//
// Used by: show, path, open, push, resolve commands
func resolveRunGlobal(input string, dataDir string) (ids.RunRef, *store.RunRecord, error) {
	// Build a minimal resolution context (no CWD or explicit repo scoping)
	rctx := &RunResolutionContext{
		DataDir: dataDir,
	}

	resolved, err := ResolveRun(rctx, input)
	if err != nil {
		return ids.RunRef{}, nil, err
	}

	return resolved.RunRef, resolved.Record, nil
}

// resolveRunInRepo resolves a run within a specific repo by repo_id.
// Used for commands with --repo flag that need scoped resolution.
func resolveRunInRepo(input, repoID, dataDir string) (ids.RunRef, *store.RunRecord, error) {
	// Build resolution context with explicit repo scope
	rctx := &RunResolutionContext{
		DataDir:        dataDir,
		ExplicitRepoID: repoID,
	}

	resolved, err := ResolveRun(rctx, input)
	if err != nil {
		return ids.RunRef{}, nil, err
	}

	return resolved.RunRef, resolved.Record, nil
}

// resolveRunByNameOrID resolves a run from a pre-loaded list of records.
// This is a simpler helper for cases where records are already loaded.
// Returns raw ids errors (*ids.ErrNotFound, *ids.ErrAmbiguous) for caller to handle.
//
// Per spec: archived runs are excluded from name matching but can still be resolved by run_id.
func resolveRunByNameOrID(input string, records []store.RunRecord) (ids.RunRef, *store.RunRecord, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return ids.RunRef{}, nil, &ids.ErrNotFound{Input: input}
	}

	// Build refs from records
	refs := make([]ids.RunRef, len(records))
	for i, rec := range records {
		refs[i] = ids.RunRef{
			RepoID: rec.RepoID,
			RunID:  rec.RunID,
			Name:   rec.Name,
			Broken: rec.Broken,
		}
	}

	// Try ID resolution first (includes all runs, even archived)
	if isRunIDFormat(input) {
		resolved, err := ids.ResolveRunRef(input, refs)
		if err == nil {
			for i := range records {
				if records[i].RunID == resolved.RunID && records[i].RepoID == resolved.RepoID {
					return resolved, &records[i], nil
				}
			}
		}
		// If not found by ID format but it looks like an ID, return not found
		var notFound *ids.ErrNotFound
		if stderrors.As(err, &notFound) {
			// Fall through to name resolution
		} else if err != nil {
			return ids.RunRef{}, nil, err
		}
	}

	// Try name resolution - exclude archived runs
	var nameMatches []ids.RunRef
	for i, ref := range refs {
		if ref.Name == input {
			// Check if run is archived
			if isRecordArchived(&records[i]) {
				continue
			}
			nameMatches = append(nameMatches, ref)
		}
	}

	switch len(nameMatches) {
	case 0:
		return ids.RunRef{}, nil, &ids.ErrNotFound{Input: input}
	case 1:
		for i := range records {
			if records[i].RunID == nameMatches[0].RunID && records[i].RepoID == nameMatches[0].RepoID {
				return nameMatches[0], &records[i], nil
			}
		}
		return ids.RunRef{}, nil, &ids.ErrNotFound{Input: input}
	default:
		return ids.RunRef{}, nil, &ids.ErrAmbiguous{Input: input, Candidates: nameMatches}
	}
}

// isRecordArchived checks if a run record is archived.
func isRecordArchived(rec *store.RunRecord) bool {
	if rec.Meta != nil && rec.Meta.Archive != nil && rec.Meta.Archive.ArchivedAt != "" {
		return true
	}
	return false
}

// handleResolveErr converts resolution errors to user-facing AgencyErrors.
func handleResolveErr(err error, input string) error {
	if err == nil {
		return nil
	}

	var notFound *ids.ErrNotFound
	if stderrors.As(err, &notFound) {
		return errors.New(errors.ERunNotFound, fmt.Sprintf("run not found: %s", input))
	}

	var ambiguous *ids.ErrAmbiguous
	if stderrors.As(err, &ambiguous) {
		return formatAmbiguousError(input, ambiguous.Candidates, "run_id")
	}

	// Pass through other errors
	return err
}

// ResolveRepoContext resolves a repo context for commands that need a repo.
// If repoPath is provided, uses that. Otherwise uses CWD.
// Returns the repo root path and repo_id.
func ResolveRepoContext(ctx context.Context, cr agencyexec.CommandRunner, cwd string, repoPath string) (string, string, error) {
	// Get home directory for path resolution
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}

	targetPath := cwd
	if repoPath != "" {
		// Validate and normalize the --repo path
		info, err := os.Stat(repoPath)
		if err != nil {
			if os.IsNotExist(err) {
				return "", "", errors.NewWithDetails(
					errors.EInvalidRepoPath,
					fmt.Sprintf("--repo path does not exist: %s", repoPath),
					map[string]string{"path": repoPath},
				)
			}
			return "", "", errors.Wrap(errors.EInvalidRepoPath, "failed to stat --repo path", err)
		}
		if !info.IsDir() {
			return "", "", errors.NewWithDetails(
				errors.EInvalidRepoPath,
				fmt.Sprintf("--repo path is not a directory: %s", repoPath),
				map[string]string{"path": repoPath},
			)
		}
		targetPath = repoPath
	}

	// Get repo root
	repoRoot, err := git.GetRepoRoot(ctx, cr, targetPath)
	if err != nil {
		if repoPath != "" {
			return "", "", errors.NewWithDetails(
				errors.EInvalidRepoPath,
				fmt.Sprintf("--repo path is not inside a git repository: %s", repoPath),
				map[string]string{"path": repoPath},
			)
		}
		return "", "", err
	}

	// Derive repo identity
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve data directory for future use
	_ = paths.ResolveDirs(osEnv{}, homeDir)

	return repoRoot.Path, repoIdentity.RepoID, nil
}
