// Package ids provides identifier resolution for agency commands.
// This file implements repo name-based resolution.
package ids

import (
	"cmp"
	"slices"
	"strings"
)

// RepoRef represents a reference to a discovered repo in the registry.
type RepoRef struct {
	// RepoID is the 16-char hex identity.
	RepoID string

	// RepoKey is "github:owner/repo" or "path:<hash>".
	RepoKey string

	// Broken indicates repo.json is unreadable or invalid.
	Broken bool
}

// ErrRepoNotFound indicates no matching repo (name, key, id, or prefix).
type ErrRepoNotFound struct {
	Input string
}

func (e *ErrRepoNotFound) Error() string {
	return "repo not found: " + e.Input
}

// ErrRepoAmbiguous indicates input matched multiple repos.
type ErrRepoAmbiguous struct {
	Input      string
	Candidates []RepoRef
}

func (e *ErrRepoAmbiguous) Error() string {
	ids := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		ids[i] = c.RepoID
	}
	return "ambiguous repo identifier '" + e.Input + "' matches: " + strings.Join(ids, ", ")
}

// RepoShortName extracts the short name from a repo key.
// For "github:owner/repo" returns "repo".
// For "path:<hash>" or anything else, returns "".
func RepoShortName(repoKey string) string {
	after, ok := strings.CutPrefix(repoKey, "github:")
	if !ok {
		return ""
	}
	idx := strings.LastIndex(after, "/")
	if idx < 0 || idx == len(after)-1 {
		return ""
	}
	return after[idx+1:]
}

// ResolveRepoRef resolves an input identifier to a single repo reference.
//
// Resolution rules (4-tier, extending the 3-tier worktree/invocation pattern
// with an additional repo-key tier):
//  1. Short name match: derive short name from RepoKey (github:owner/repo → "repo").
//     If exactly 1 non-broken ref matches, return it.
//  2. Repo key match: exact match on full RepoKey, or "owner/repo" without prefix.
//     If exactly 1 matches, return it.
//  3. Exact ID match: RepoID == input (escape hatch, works for broken).
//     If exactly 1 matches, return it.
//  4. Prefix match: strings.HasPrefix(RepoID, input) among non-broken.
//     If exactly 1 matches, return it.
//
// Broken repos are excluded from name/key/prefix matching but reachable by exact ID.
func ResolveRepoRef(input string, refs []RepoRef) (RepoRef, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return RepoRef{}, &ErrRepoNotFound{Input: ""}
	}

	// Broken repos are only matched by exact ID (escape hatch).
	isEligible := func(ref RepoRef) bool {
		return !ref.Broken
	}

	// 1. Short name match among eligible refs
	var nameMatches []RepoRef
	for _, ref := range refs {
		if !isEligible(ref) {
			continue
		}
		shortName := RepoShortName(ref.RepoKey)
		if shortName != "" && shortName == input {
			nameMatches = append(nameMatches, ref)
		}
	}

	if len(nameMatches) == 1 {
		return nameMatches[0], nil
	}
	if len(nameMatches) > 1 {
		sortRepoCandidates(nameMatches)
		return RepoRef{}, &ErrRepoAmbiguous{Input: input, Candidates: nameMatches}
	}

	// 2. Repo key match: exact RepoKey or owner/repo format
	var keyMatches []RepoRef
	for _, ref := range refs {
		if !isEligible(ref) {
			continue
		}
		if ref.RepoKey == input {
			keyMatches = append(keyMatches, ref)
			continue
		}
		// Try owner/repo format (without "github:" prefix)
		ownerRepo, ok := strings.CutPrefix(ref.RepoKey, "github:")
		if ok && strings.Contains(ownerRepo, "/") && ownerRepo == input {
			keyMatches = append(keyMatches, ref)
		}
	}

	if len(keyMatches) == 1 {
		return keyMatches[0], nil
	}
	if len(keyMatches) > 1 {
		sortRepoCandidates(keyMatches)
		return RepoRef{}, &ErrRepoAmbiguous{Input: input, Candidates: keyMatches}
	}

	// 3. Exact ID match (works for all repos including broken)
	var exactMatches []RepoRef
	for _, ref := range refs {
		if ref.RepoID == input {
			exactMatches = append(exactMatches, ref)
		}
	}

	if len(exactMatches) == 1 {
		return exactMatches[0], nil
	}
	if len(exactMatches) > 1 {
		sortRepoCandidates(exactMatches)
		return RepoRef{}, &ErrRepoAmbiguous{Input: input, Candidates: exactMatches}
	}

	// 4. Prefix match among eligible refs
	var prefixMatches []RepoRef
	for _, ref := range refs {
		if !isEligible(ref) {
			continue
		}
		if strings.HasPrefix(ref.RepoID, input) {
			prefixMatches = append(prefixMatches, ref)
		}
	}

	switch len(prefixMatches) {
	case 0:
		return RepoRef{}, &ErrRepoNotFound{Input: input}
	case 1:
		return prefixMatches[0], nil
	default:
		sortRepoCandidates(prefixMatches)
		return RepoRef{}, &ErrRepoAmbiguous{Input: input, Candidates: prefixMatches}
	}
}

// sortRepoCandidates sorts candidates deterministically by RepoID.
func sortRepoCandidates(refs []RepoRef) {
	slices.SortFunc(refs, func(a, b RepoRef) int {
		return cmp.Compare(a.RepoID, b.RepoID)
	})
}
