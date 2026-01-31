// Package ids provides identifier resolution for agency commands.
// This file implements invocation resolution (Slice 8 PR-02).
package ids

import (
	"sort"
	"strings"
)

// InvocationRef represents a reference to a discovered invocation.
type InvocationRef struct {
	// InvocationID is the invocation_id from the directory name (canonical identity).
	InvocationID string

	// RepoID is the repo_id.
	RepoID string

	// IntegrationWorktreeID is the target integration worktree.
	IntegrationWorktreeID string

	// InvocationName is the optional human-readable name. Empty if broken or not set.
	// NOTE: Names are NOT used for resolution - only for display.
	InvocationName string

	// Status is the invocation status (starting, running, finished, failed).
	Status string

	// LandingStatus is the landing status (pending, landed, discarded).
	LandingStatus string

	// SandboxExists indicates whether the sandbox tree still exists.
	SandboxExists bool

	// Broken indicates meta.json is unreadable or invalid.
	Broken bool
}

// ErrInvocationNotFound indicates no matching invocation_id (exact or prefix).
type ErrInvocationNotFound struct {
	Input string
}

func (e *ErrInvocationNotFound) Error() string {
	return "invocation not found: " + e.Input
}

// ErrInvocationAmbiguous indicates prefix matched multiple invocation_ids.
type ErrInvocationAmbiguous struct {
	Input      string
	Candidates []InvocationRef
}

func (e *ErrInvocationAmbiguous) Error() string {
	ids := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		ids[i] = c.InvocationID
	}
	return "ambiguous invocation id " + e.Input + " matches: " + strings.Join(ids, ", ")
}

// ResolveInvocationRefOpts contains options for invocation resolution.
type ResolveInvocationRefOpts struct {
	// IncludeFinished allows matching finished (landed/discarded) invocations.
	// By default, only non-terminal invocations are matched.
	IncludeFinished bool

	// WorktreeFilter limits resolution to a specific worktree.
	// Empty string means no filter.
	WorktreeFilter string
}

// ResolveInvocationRef resolves an input identifier to a single invocation reference.
//
// Resolution rules:
//  1. Exact ID match: if exactly one candidate has InvocationID == input, resolve.
//  2. Prefix match: treat input as a prefix of InvocationID.
//
// NOTE: Unlike worktrees, invocation names are NOT used for resolution.
// Names are display-only and may be duplicated.
//
// By default, landed/discarded invocations are excluded from matching.
// Exact ID match always works (escape hatch).
// If IncludeFinished is true, all invocations are included in matching.
func ResolveInvocationRef(input string, refs []InvocationRef, opts ResolveInvocationRefOpts) (InvocationRef, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return InvocationRef{}, &ErrInvocationNotFound{Input: ""}
	}

	// Build filtered list based on options
	isEligible := func(ref InvocationRef) bool {
		// Broken invocations are only matched by exact ID
		if ref.Broken {
			return false
		}
		// Worktree filter
		if opts.WorktreeFilter != "" && ref.IntegrationWorktreeID != opts.WorktreeFilter {
			return false
		}
		// Landed/discarded invocations are excluded unless IncludeFinished
		if !opts.IncludeFinished {
			if ref.LandingStatus == "landed" || ref.LandingStatus == "discarded" {
				return false
			}
		}
		return true
	}

	// 1. Exact ID match (works for all invocations including broken)
	var exactMatches []InvocationRef
	for _, ref := range refs {
		if ref.InvocationID == input {
			// Apply worktree filter even for exact match
			if opts.WorktreeFilter != "" && ref.IntegrationWorktreeID != opts.WorktreeFilter {
				continue
			}
			exactMatches = append(exactMatches, ref)
		}
	}

	if len(exactMatches) == 1 {
		return exactMatches[0], nil
	}
	if len(exactMatches) > 1 {
		sortInvocationCandidates(exactMatches)
		return InvocationRef{}, &ErrInvocationAmbiguous{Input: input, Candidates: exactMatches}
	}

	// 2. Prefix match among eligible invocations
	var prefixMatches []InvocationRef
	for _, ref := range refs {
		if strings.HasPrefix(ref.InvocationID, input) && isEligible(ref) {
			prefixMatches = append(prefixMatches, ref)
		}
	}

	switch len(prefixMatches) {
	case 0:
		return InvocationRef{}, &ErrInvocationNotFound{Input: input}
	case 1:
		return prefixMatches[0], nil
	default:
		sortInvocationCandidates(prefixMatches)
		return InvocationRef{}, &ErrInvocationAmbiguous{Input: input, Candidates: prefixMatches}
	}
}

// sortInvocationCandidates sorts candidates deterministically by InvocationID.
func sortInvocationCandidates(refs []InvocationRef) {
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].InvocationID < refs[j].InvocationID
	})
}
