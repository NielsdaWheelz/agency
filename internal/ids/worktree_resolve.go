// Package ids provides identifier resolution for agency commands.
// This file implements integration worktree resolution (Slice 8 PR-01).
package ids

import (
	"sort"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

// WorktreeRef represents a reference to a discovered integration worktree.
type WorktreeRef struct {
	// WorktreeID is the worktree_id from the directory name (canonical identity).
	WorktreeID string

	// RepoID is the repo_id.
	RepoID string

	// Name is the worktree name from meta.json. Empty if broken.
	Name string

	// State is the worktree state (present/archived).
	State string

	// Broken indicates meta.json is unreadable or invalid.
	Broken bool
}

// ErrWorktreeNotFound indicates no matching worktree_id (exact or prefix).
type ErrWorktreeNotFound struct {
	Input string
}

func (e *ErrWorktreeNotFound) Error() string {
	return "worktree not found: " + e.Input
}

// ErrWorktreeAmbiguous indicates prefix matched multiple worktree_ids.
type ErrWorktreeAmbiguous struct {
	Input      string
	Candidates []WorktreeRef
}

func (e *ErrWorktreeAmbiguous) Error() string {
	ids := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		ids[i] = c.WorktreeID
	}
	return "ambiguous worktree id " + e.Input + " matches: " + strings.Join(ids, ", ")
}

// ResolveWorktreeRefOpts contains options for worktree resolution.
type ResolveWorktreeRefOpts struct {
	// IncludeArchived allows matching archived worktrees.
	IncludeArchived bool
}

// ResolveWorktreeRef resolves an input identifier to a single worktree reference.
//
// Resolution rules:
//  1. Name match (among non-archived worktrees by default): if exactly one
//     candidate has Name == input, resolve to that.
//  2. Exact ID match: if exactly one candidate has WorktreeID == input, resolve.
//  3. Prefix match: treat input as a prefix of WorktreeID.
//
// By default, archived and broken worktrees are excluded from name/prefix matching.
// Exact ID match always works (escape hatch).
// If IncludeArchived is true, archived worktrees are included in all matching.
func ResolveWorktreeRef(input string, refs []WorktreeRef, opts ResolveWorktreeRefOpts) (WorktreeRef, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return WorktreeRef{}, &ErrWorktreeNotFound{Input: ""}
	}

	// Build filtered list based on options
	isEligible := func(ref WorktreeRef) bool {
		// Broken worktrees are only matched by exact ID
		if ref.Broken {
			return false
		}
		// Archived worktrees are excluded unless IncludeArchived
		if ref.State == "archived" && !opts.IncludeArchived {
			return false
		}
		return true
	}

	// 1. Name match among eligible worktrees
	var nameMatches []WorktreeRef
	for _, ref := range refs {
		if ref.Name == input && isEligible(ref) {
			nameMatches = append(nameMatches, ref)
		}
	}

	if len(nameMatches) == 1 {
		return nameMatches[0], nil
	}
	if len(nameMatches) > 1 {
		sortWorktreeCandidates(nameMatches)
		return WorktreeRef{}, &ErrWorktreeAmbiguous{Input: input, Candidates: nameMatches}
	}

	// 2. Exact ID match (works for all worktrees including broken/archived)
	var exactMatches []WorktreeRef
	for _, ref := range refs {
		if ref.WorktreeID == input {
			exactMatches = append(exactMatches, ref)
		}
	}

	if len(exactMatches) == 1 {
		return exactMatches[0], nil
	}
	if len(exactMatches) > 1 {
		sortWorktreeCandidates(exactMatches)
		return WorktreeRef{}, &ErrWorktreeAmbiguous{Input: input, Candidates: exactMatches}
	}

	// 3. Prefix match among eligible worktrees
	var prefixMatches []WorktreeRef
	for _, ref := range refs {
		if strings.HasPrefix(ref.WorktreeID, input) && isEligible(ref) {
			prefixMatches = append(prefixMatches, ref)
		}
	}

	switch len(prefixMatches) {
	case 0:
		return WorktreeRef{}, &ErrWorktreeNotFound{Input: input}
	case 1:
		return prefixMatches[0], nil
	default:
		sortWorktreeCandidates(prefixMatches)
		return WorktreeRef{}, &ErrWorktreeAmbiguous{Input: input, Candidates: prefixMatches}
	}
}

// sortWorktreeCandidates sorts candidates deterministically by WorktreeID.
func sortWorktreeCandidates(refs []WorktreeRef) {
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].WorktreeID < refs[j].WorktreeID
	})
}

// CheckWorktreeNameUnique verifies a name is not already used by a non-archived worktree.
// Returns nil if unique, or E_NAME_EXISTS error with details.
func CheckWorktreeNameUnique(name string, refs []WorktreeRef) error {
	for _, ref := range refs {
		if ref.Name == name && !ref.Broken && ref.State != "archived" {
			return errors.NewWithDetails(
				errors.ENameExists,
				"name '"+name+"' is already used by an active integration worktree",
				map[string]string{
					"name":        name,
					"worktree_id": ref.WorktreeID,
				},
			)
		}
	}
	return nil
}
