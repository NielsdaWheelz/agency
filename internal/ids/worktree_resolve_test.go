package ids

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveWorktreeRef_ByName(t *testing.T) {
	t.Parallel()
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
		{WorktreeID: "20260131110000-c3d4", Name: "feature-b", State: "present"},
		{WorktreeID: "20260131120000-e5f6", Name: "feature-c", State: "archived"},
	}

	// Resolve by name (present)
	ref, err := ResolveWorktreeRef("feature-a", refs, ResolveWorktreeRefOpts{})
	require.NoError(t, err)
	assert.Equal(t, "20260131100000-a1b2", ref.WorktreeID)

	// Resolve by name (archived, excluded by default)
	_, err = ResolveWorktreeRef("feature-c", refs, ResolveWorktreeRefOpts{})
	require.Error(t, err, "expected error for archived worktree")
	assert.IsType(t, &ErrWorktreeNotFound{}, err)

	// Resolve by name (archived, included)
	ref, err = ResolveWorktreeRef("feature-c", refs, ResolveWorktreeRefOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Equal(t, "20260131120000-e5f6", ref.WorktreeID)
}

func TestResolveWorktreeRef_ByExactID(t *testing.T) {
	t.Parallel()
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
		{WorktreeID: "20260131110000-c3d4", Name: "feature-b", State: "archived"},
		{WorktreeID: "20260131120000-e5f6", Name: "feature-c", State: "present", Broken: true},
	}

	// Resolve by exact ID (present)
	ref, err := ResolveWorktreeRef("20260131100000-a1b2", refs, ResolveWorktreeRefOpts{})
	require.NoError(t, err)
	assert.Equal(t, "feature-a", ref.Name)

	// Exact ID should work for archived worktrees (escape hatch)
	ref, err = ResolveWorktreeRef("20260131110000-c3d4", refs, ResolveWorktreeRefOpts{})
	require.NoError(t, err)
	assert.Equal(t, "feature-b", ref.Name)

	// Exact ID should work for broken worktrees (escape hatch)
	ref, err = ResolveWorktreeRef("20260131120000-e5f6", refs, ResolveWorktreeRefOpts{})
	require.NoError(t, err)
	assert.True(t, ref.Broken, "expected Broken=true")
}

func TestResolveWorktreeRef_ByPrefix(t *testing.T) {
	t.Parallel()
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
		{WorktreeID: "20260131110000-c3d4", Name: "feature-b", State: "present"},
		{WorktreeID: "20260131120000-e5f6", Name: "feature-c", State: "archived"},
	}

	// Unique prefix
	ref, err := ResolveWorktreeRef("2026013110", refs, ResolveWorktreeRefOpts{})
	require.NoError(t, err)
	assert.Equal(t, "20260131100000-a1b2", ref.WorktreeID)

	// Ambiguous prefix
	_, err = ResolveWorktreeRef("202601311", refs, ResolveWorktreeRefOpts{})
	require.Error(t, err, "expected error for ambiguous prefix")
	assert.IsType(t, &ErrWorktreeAmbiguous{}, err)

	// Prefix excludes archived by default
	ref, err = ResolveWorktreeRef("2026013111", refs, ResolveWorktreeRefOpts{})
	require.NoError(t, err)
	assert.Equal(t, "20260131110000-c3d4", ref.WorktreeID)
}

func TestResolveWorktreeRef_NotFound(t *testing.T) {
	t.Parallel()
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
	}

	_, err := ResolveWorktreeRef("nonexistent", refs, ResolveWorktreeRefOpts{})
	require.Error(t, err)
	assert.IsType(t, &ErrWorktreeNotFound{}, err)
}

func TestResolveWorktreeRef_EmptyInput(t *testing.T) {
	t.Parallel()
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
	}

	_, err := ResolveWorktreeRef("", refs, ResolveWorktreeRefOpts{})
	require.Error(t, err, "expected error for empty input")
}

func TestResolveWorktreeRef_BrokenExcluded(t *testing.T) {
	t.Parallel()
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present", Broken: true},
	}

	// Name match excludes broken
	_, err := ResolveWorktreeRef("feature-a", refs, ResolveWorktreeRefOpts{})
	require.Error(t, err, "expected error for broken worktree name match")

	// Prefix match excludes broken
	_, err = ResolveWorktreeRef("202601311", refs, ResolveWorktreeRefOpts{})
	require.Error(t, err, "expected error for broken worktree prefix match")

	// Exact ID works (escape hatch)
	ref, err := ResolveWorktreeRef("20260131100000-a1b2", refs, ResolveWorktreeRefOpts{})
	require.NoError(t, err)
	assert.True(t, ref.Broken, "expected Broken=true")
}

// TestResolve_AmbiguousWorktreeID verifies that an ambiguous worktree ID prefix
// returns an ErrWorktreeAmbiguous error with the correct candidates, and that
// candidates are sorted deterministically by WorktreeID.
func TestResolve_AmbiguousWorktreeID(t *testing.T) {
	t.Parallel()
	refs := []WorktreeRef{
		{WorktreeID: "20260201120000-aa11", Name: "feature-one", State: "present"},
		{WorktreeID: "20260201120000-aa22", Name: "feature-two", State: "present"},
		{WorktreeID: "20260201120000-bb33", Name: "feature-three", State: "present"},
	}

	// Prefix "20260201120000-aa" matches both aa11 and aa22
	_, err := ResolveWorktreeRef("20260201120000-aa", refs, ResolveWorktreeRefOpts{})
	require.Error(t, err)

	ambErr, ok := err.(*ErrWorktreeAmbiguous)
	require.True(t, ok, "expected *ErrWorktreeAmbiguous, got %T", err)
	assert.Equal(t, "20260201120000-aa", ambErr.Input)
	require.Len(t, ambErr.Candidates, 2, "expected 2 candidates")

	// Candidates should be sorted by WorktreeID
	assert.Equal(t, "20260201120000-aa11", ambErr.Candidates[0].WorktreeID)
	assert.Equal(t, "20260201120000-aa22", ambErr.Candidates[1].WorktreeID)

	// Error message should contain both candidate IDs
	errMsg := err.Error()
	assert.Contains(t, errMsg, "20260201120000-aa11")
	assert.Contains(t, errMsg, "20260201120000-aa22")
}

// TestResolve_AmbiguousWorktreeID_NameMatch verifies that name collision among
// non-archived worktrees also returns ErrWorktreeAmbiguous.
func TestResolve_AmbiguousWorktreeID_NameMatch(t *testing.T) {
	t.Parallel()

	// Two worktrees with the same name (this can happen if worktrees were
	// created by different repos or through a store anomaly)
	refs := []WorktreeRef{
		{WorktreeID: "20260201100000-aaaa", Name: "same-name", State: "present"},
		{WorktreeID: "20260201110000-bbbb", Name: "same-name", State: "present"},
	}

	_, err := ResolveWorktreeRef("same-name", refs, ResolveWorktreeRefOpts{})
	require.Error(t, err)

	ambErr, ok := err.(*ErrWorktreeAmbiguous)
	require.True(t, ok, "expected *ErrWorktreeAmbiguous, got %T", err)
	assert.Len(t, ambErr.Candidates, 2)
}

func TestCheckWorktreeNameUnique(t *testing.T) {
	t.Parallel()
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
		{WorktreeID: "20260131110000-c3d4", Name: "feature-b", State: "archived"},
	}

	// Name in use by present worktree
	err := CheckWorktreeNameUnique("feature-a", refs)
	require.Error(t, err, "expected error for duplicate name")

	// Name in use by archived worktree (allowed)
	err = CheckWorktreeNameUnique("feature-b", refs)
	require.NoError(t, err)

	// New name (allowed)
	err = CheckWorktreeNameUnique("feature-c", refs)
	require.NoError(t, err)
}
