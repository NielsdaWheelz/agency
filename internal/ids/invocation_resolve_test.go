package ids

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveInvocationRef_BrokenExcludedExceptExactID(t *testing.T) {
	t.Parallel()
	refs := []InvocationRef{
		{
			InvocationID:          "20260131120000-beef",
			RepoID:                "test-repo",
			IntegrationWorktreeID: "20260131110000-a1b2",
			InvocationName:        "broken-agent",
			Status:                "running",
			Broken:                true,
		},
	}

	_, err := ResolveInvocationRef("broken-agent", refs, ResolveInvocationRefOpts{IncludeFinished: true})
	require.Error(t, err)
	var notFound *ErrInvocationNotFound
	require.ErrorAs(t, err, &notFound)

	_, err = ResolveInvocationRef("2026013112", refs, ResolveInvocationRefOpts{IncludeFinished: true})
	require.Error(t, err)
	require.ErrorAs(t, err, &notFound)

	ref, err := ResolveInvocationRef("20260131120000-beef", refs, ResolveInvocationRefOpts{IncludeFinished: true})
	require.NoError(t, err)
	if !ref.Broken {
		t.Fatalf("resolved ref Broken = false, want true")
	}
	if ref.InvocationID != "20260131120000-beef" {
		t.Fatalf("resolved invocation_id = %q, want %q", ref.InvocationID, "20260131120000-beef")
	}
}
