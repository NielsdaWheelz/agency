package s1gates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

func TestParseGateSet_DeterministicMembershipFromCanonicalSource(t *testing.T) {
	t.Parallel()

	content := readTestdata(t, "corpus_valid.md")
	repoRoot := filepath.Join("testdata", "repo_valid")
	fileExists := RepoFileExists(repoRoot)

	gs, err := ParseGateSet(content, "docs/v2.1/release-gates.md", fileExists)
	require.NoError(t, err)

	assert.Equal(t, "S1", gs.Slice)
	assert.Equal(t, "docs/v2.1/release-gates.md", gs.SourceRef)

	// Gate A items match fixture order exactly.
	require.Len(t, gs.GateAItems, 2)
	assert.Equal(t, "docs/issues/gate-a-item-1.md", gs.GateAItems[0])
	assert.Equal(t, "docs/issues/gate-a-item-2.md", gs.GateAItems[1])

	// Gate B items match fixture order exactly.
	require.Len(t, gs.GateBItems, 2)
	assert.Equal(t, "docs/issues/gate-b-item-1.md", gs.GateBItems[0])
	assert.Equal(t, "docs/issues/gate-b-item-2.md", gs.GateBItems[1])
}

func TestParseGateSet_RejectsMissingIssuePathWithEGateSetInvalid(t *testing.T) {
	t.Parallel()

	content := readTestdata(t, "corpus_missing_path.md")
	repoRoot := filepath.Join("testdata", "repo_valid")
	fileExists := RepoFileExists(repoRoot)

	gs, err := ParseGateSet(content, "docs/v2.1/release-gates.md", fileExists)
	assert.Nil(t, gs)
	require.Error(t, err)

	assert.Equal(t, agencyerrors.EGateSetInvalid, agencyerrors.GetCode(err))

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "docs/issues/nonexistent-issue.md", ae.Details["issue_path"])
}

func TestParseGateSet_RejectsDuplicateIssueRefs(t *testing.T) {
	t.Parallel()

	content := readTestdata(t, "corpus_duplicate.md")
	// shared-item.md exists in repo_valid.
	repoRoot := filepath.Join("testdata", "repo_valid")
	fileExists := RepoFileExists(repoRoot)

	gs, err := ParseGateSet(content, "docs/v2.1/release-gates.md", fileExists)
	assert.Nil(t, gs)
	require.Error(t, err)

	assert.Equal(t, agencyerrors.EGateSetInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "duplicate")

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "docs/issues/shared-item.md", ae.Details["issue_path"])
}

func TestParseGateSet_EmptyContentReturnsError(t *testing.T) {
	t.Parallel()

	gs, err := ParseGateSet("", "docs/v2.1/release-gates.md", func(string) bool { return true })
	assert.Nil(t, gs)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateSetInvalid, agencyerrors.GetCode(err))
}

func TestParseGateSet_IgnoresGateCItems(t *testing.T) {
	t.Parallel()

	content := readTestdata(t, "corpus_valid.md")
	repoRoot := filepath.Join("testdata", "repo_valid")
	fileExists := RepoFileExists(repoRoot)

	gs, err := ParseGateSet(content, "docs/v2.1/release-gates.md", fileExists)
	require.NoError(t, err)

	// Gate C items are text descriptions without backtick paths;
	// they must not appear in A or B lists.
	for _, p := range gs.GateAItems {
		assert.NotContains(t, p, "Headless")
	}
	for _, p := range gs.GateBItems {
		assert.NotContains(t, p, "Headless")
	}
}

// readTestdata reads a file from the testdata directory.
func readTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(data)
}
