package ids

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRepoRef_ByShortName(t *testing.T) {
	t.Parallel()
	refs := []RepoRef{
		{RepoID: "769749d77af0806f", RepoKey: "github:NielsdaWheelz/agency"},
		{RepoID: "aabbccdd11223344", RepoKey: "github:otherowner/otherrepo"},
	}

	ref, err := ResolveRepoRef("agency", refs)
	require.NoError(t, err)
	assert.Equal(t, "769749d77af0806f", ref.RepoID)
}

func TestResolveRepoRef_ByShortName_Ambiguous(t *testing.T) {
	t.Parallel()
	refs := []RepoRef{
		{RepoID: "aaaa111122223333", RepoKey: "github:owner1/myrepo"},
		{RepoID: "bbbb444455556666", RepoKey: "github:owner2/myrepo"},
	}

	_, err := ResolveRepoRef("myrepo", refs)
	require.Error(t, err)
	ambErr, ok := err.(*ErrRepoAmbiguous)
	require.True(t, ok, "expected *ErrRepoAmbiguous, got %T", err)
	assert.Equal(t, "myrepo", ambErr.Input)
	assert.Len(t, ambErr.Candidates, 2)
	// Sorted by RepoID
	assert.Equal(t, "aaaa111122223333", ambErr.Candidates[0].RepoID)
	assert.Equal(t, "bbbb444455556666", ambErr.Candidates[1].RepoID)
	// Error message includes candidate IDs
	assert.Contains(t, err.Error(), "aaaa111122223333")
	assert.Contains(t, err.Error(), "bbbb444455556666")
}

func TestResolveRepoRef_ByOwnerSlashRepo(t *testing.T) {
	t.Parallel()
	refs := []RepoRef{
		{RepoID: "769749d77af0806f", RepoKey: "github:NielsdaWheelz/agency"},
		{RepoID: "aabbccdd11223344", RepoKey: "github:otherowner/agency"},
	}

	ref, err := ResolveRepoRef("NielsdaWheelz/agency", refs)
	require.NoError(t, err)
	assert.Equal(t, "769749d77af0806f", ref.RepoID)
}

func TestResolveRepoRef_ByFullRepoKey(t *testing.T) {
	t.Parallel()
	refs := []RepoRef{
		{RepoID: "769749d77af0806f", RepoKey: "github:NielsdaWheelz/agency"},
	}

	ref, err := ResolveRepoRef("github:NielsdaWheelz/agency", refs)
	require.NoError(t, err)
	assert.Equal(t, "769749d77af0806f", ref.RepoID)
}

func TestResolveRepoRef_ByExactID(t *testing.T) {
	t.Parallel()
	refs := []RepoRef{
		{RepoID: "769749d77af0806f", RepoKey: "github:NielsdaWheelz/agency"},
		{RepoID: "aabbccdd11223344", RepoKey: "github:otherowner/otherrepo", Broken: true},
	}

	// Normal ref by exact ID
	ref, err := ResolveRepoRef("769749d77af0806f", refs)
	require.NoError(t, err)
	assert.Equal(t, "github:NielsdaWheelz/agency", ref.RepoKey)

	// Broken ref reachable by exact ID
	ref, err = ResolveRepoRef("aabbccdd11223344", refs)
	require.NoError(t, err)
	assert.True(t, ref.Broken)
	assert.Equal(t, "aabbccdd11223344", ref.RepoID)
}

func TestResolveRepoRef_ByPrefix(t *testing.T) {
	t.Parallel()
	refs := []RepoRef{
		{RepoID: "769749d77af0806f", RepoKey: "github:NielsdaWheelz/agency"},
		{RepoID: "aabbccdd11223344", RepoKey: "github:otherowner/otherrepo"},
	}

	// Unique prefix
	ref, err := ResolveRepoRef("769749d", refs)
	require.NoError(t, err)
	assert.Equal(t, "769749d77af0806f", ref.RepoID)

	// Single char unique prefix
	ref, err = ResolveRepoRef("7", refs)
	require.NoError(t, err)
	assert.Equal(t, "769749d77af0806f", ref.RepoID)
}

func TestResolveRepoRef_ByPrefix_Ambiguous(t *testing.T) {
	t.Parallel()
	refs := []RepoRef{
		{RepoID: "aa11111111111111", RepoKey: "github:owner1/repo1"},
		{RepoID: "aa22222222222222", RepoKey: "github:owner2/repo2"},
	}

	_, err := ResolveRepoRef("aa", refs)
	require.Error(t, err)
	ambErr, ok := err.(*ErrRepoAmbiguous)
	require.True(t, ok, "expected *ErrRepoAmbiguous, got %T", err)
	assert.Len(t, ambErr.Candidates, 2)
	assert.Equal(t, "aa11111111111111", ambErr.Candidates[0].RepoID)
	assert.Equal(t, "aa22222222222222", ambErr.Candidates[1].RepoID)
}

func TestResolveRepoRef_NotFound(t *testing.T) {
	t.Parallel()
	refs := []RepoRef{
		{RepoID: "769749d77af0806f", RepoKey: "github:NielsdaWheelz/agency"},
	}

	_, err := ResolveRepoRef("nonexistent", refs)
	require.Error(t, err)
	notFound, ok := err.(*ErrRepoNotFound)
	require.True(t, ok, "expected *ErrRepoNotFound, got %T", err)
	assert.Equal(t, "nonexistent", notFound.Input)
}

func TestResolveRepoRef_EmptyInput(t *testing.T) {
	t.Parallel()
	refs := []RepoRef{
		{RepoID: "769749d77af0806f", RepoKey: "github:NielsdaWheelz/agency"},
	}

	_, err := ResolveRepoRef("", refs)
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)

	// Whitespace-only input is treated as empty
	_, err = ResolveRepoRef("   ", refs)
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)
}

func TestResolveRepoRef_BrokenExcluded(t *testing.T) {
	t.Parallel()
	refs := []RepoRef{
		{RepoID: "769749d77af0806f", RepoKey: "github:NielsdaWheelz/agency", Broken: true},
	}

	// Short name match excludes broken
	_, err := ResolveRepoRef("agency", refs)
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)

	// Key match excludes broken
	_, err = ResolveRepoRef("github:NielsdaWheelz/agency", refs)
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)

	// Prefix match excludes broken
	_, err = ResolveRepoRef("769749d", refs)
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)

	// Exact ID works (escape hatch)
	ref, err := ResolveRepoRef("769749d77af0806f", refs)
	require.NoError(t, err)
	assert.True(t, ref.Broken)
}

func TestResolveRepoRef_PathBasedRepo(t *testing.T) {
	t.Parallel()
	refs := []RepoRef{
		{RepoID: "abcdef1234567890", RepoKey: "path:a1b2c3d4"},
		{RepoID: "769749d77af0806f", RepoKey: "github:NielsdaWheelz/agency"},
	}

	// Path-based repos have no short name — can't match by name
	_, err := ResolveRepoRef("a1b2c3d4", refs)
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)

	// But can match by full repo key
	ref, err := ResolveRepoRef("path:a1b2c3d4", refs)
	require.NoError(t, err)
	assert.Equal(t, "abcdef1234567890", ref.RepoID)

	// And by exact ID
	ref, err = ResolveRepoRef("abcdef1234567890", refs)
	require.NoError(t, err)
	assert.Equal(t, "path:a1b2c3d4", ref.RepoKey)

	// And by prefix
	ref, err = ResolveRepoRef("abcdef", refs)
	require.NoError(t, err)
	assert.Equal(t, "abcdef1234567890", ref.RepoID)
}

func TestRepoShortName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		repoKey  string
		expected string
	}{
		{"github:NielsdaWheelz/agency", "agency"},
		{"github:owner/repo", "repo"},
		{"github:owner/nested/repo", "repo"},
		{"path:a1b2c3d4", ""},
		{"", ""},
		{"github:", ""},
		{"github:owner/", ""},
		{"github:noslash", ""},
	}

	for _, tt := range tests {
		t.Run(tt.repoKey, func(t *testing.T) {
			assert.Equal(t, tt.expected, RepoShortName(tt.repoKey))
		})
	}
}

func TestResolveRepoRef_PriorityOrder(t *testing.T) {
	t.Parallel()

	// Verify that name match takes priority over prefix match.
	// "aabb" is both a short name (step 1) and a valid hex prefix (step 4).
	refs := []RepoRef{
		{RepoID: "1111111111111111", RepoKey: "github:owner/aabb"},
		{RepoID: "aabb222233334444", RepoKey: "github:owner/other"},
	}

	// "aabb" should match by short name (step 1), not prefix (step 4)
	ref, err := ResolveRepoRef("aabb", refs)
	require.NoError(t, err)
	assert.Equal(t, "1111111111111111", ref.RepoID)
}

func TestResolveRepoRef_NilAndEmptyRefs(t *testing.T) {
	t.Parallel()

	// nil refs — falls through to not found
	_, err := ResolveRepoRef("anything", nil)
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)

	// empty refs — same behavior
	_, err = ResolveRepoRef("anything", []RepoRef{})
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)
}

func TestResolveRepoRef_EmptyRepoKey(t *testing.T) {
	t.Parallel()

	// A ref with empty RepoKey — only reachable by ID/prefix, not by name
	refs := []RepoRef{
		{RepoID: "abcdef1234567890", RepoKey: ""},
	}

	// Short name match finds nothing (RepoShortName("") == "")
	_, err := ResolveRepoRef("anything", refs)
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)

	// But exact ID still works
	ref, err := ResolveRepoRef("abcdef1234567890", refs)
	require.NoError(t, err)
	assert.Equal(t, "abcdef1234567890", ref.RepoID)

	// And prefix works
	ref, err = ResolveRepoRef("abcdef", refs)
	require.NoError(t, err)
	assert.Equal(t, "abcdef1234567890", ref.RepoID)
}

func TestResolveRepoRef_MalformedGithubKey(t *testing.T) {
	t.Parallel()

	// Malformed "github:noslash" key — repoOwnerSlashName returns ""
	// so it cannot be matched by owner/repo format in tier 2
	refs := []RepoRef{
		{RepoID: "abcdef1234567890", RepoKey: "github:noslash"},
	}

	_, err := ResolveRepoRef("noslash", refs)
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)
}

func TestResolveRepoRef_CaseSensitive(t *testing.T) {
	t.Parallel()

	refs := []RepoRef{
		{RepoID: "769749d77af0806f", RepoKey: "github:NielsdaWheelz/agency"},
	}

	// Short name is case-sensitive
	_, err := ResolveRepoRef("Agency", refs)
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)

	// owner/repo is case-sensitive
	_, err = ResolveRepoRef("nielsdawheelz/agency", refs)
	require.Error(t, err)
	assert.IsType(t, &ErrRepoNotFound{}, err)
}
