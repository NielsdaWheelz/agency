package s1gates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

func TestEvaluateGateItem_ReturnsNormalizedModel(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "repo_eval")
	eval, err := EvaluateGateItem("docs/issues/complete.md", repoRoot)
	require.NoError(t, err)

	assert.Equal(t, "docs/issues/complete.md", eval.IssuePath)
	assert.Equal(t, StateClosed, eval.State)
	assert.True(t, eval.AcceptanceComplete)
	assert.True(t, eval.TestsComplete)
	assert.True(t, eval.ClosureEvidencePresent)
	assert.Empty(t, eval.MissingRequirements)
	assert.Equal(t, agencyerrors.Code(""), eval.BlockingCode)
	assert.Equal(t, []string{"pr:101", "commit:abc123"}, eval.EvidenceRefs)
	assert.False(t, eval.RequiresGHE2E)
}

func TestEvaluateGateItem_NotFoundReturnsEGateItemNotFound(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "repo_eval")
	eval, err := EvaluateGateItem("docs/issues/does-not-exist.md", repoRoot)
	assert.Nil(t, eval)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemNotFound, agencyerrors.GetCode(err))
}

func TestEvaluateGateItem_MalformedIssueReturnsEGateItemInvalid(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "repo_eval")
	eval, err := EvaluateGateItem("docs/issues/malformed.md", repoRoot)
	assert.Nil(t, eval)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemInvalid, agencyerrors.GetCode(err))
}

func TestEvaluateGateItem_IncompleteRequirementsMapToBlockingCode(t *testing.T) {
	t.Parallel()

	// incomplete.md has acceptance complete but no closure evidence.
	repoRoot := filepath.Join("testdata", "repo_eval")
	eval, err := EvaluateGateItem("docs/issues/incomplete.md", repoRoot)
	require.NoError(t, err)

	assert.True(t, eval.AcceptanceComplete)
	assert.False(t, eval.ClosureEvidencePresent)
	assert.False(t, eval.TestsComplete)

	// Missing requirements include closure block and test evidence.
	assert.Contains(t, eval.MissingRequirements, MissingClosureEvidenceBlock)
	assert.Contains(t, eval.MissingRequirements, MissingSuiteTestEvidence)
	assert.Contains(t, eval.MissingRequirements, MissingTargetedTestEvidence)

	// Blocking code follows precedence: closure block > tests incomplete.
	assert.Equal(t, agencyerrors.EGateItemClosureBlockMissing, eval.BlockingCode)
}

func TestEvaluateGateItem_ClosedWithoutEvidenceRefsReportsEvMissing(t *testing.T) {
	t.Parallel()

	// Use a fixture with state=closed but no closure evidence block.
	// We need to create this inline since no fixture matches exactly.
	repoRoot := t.TempDir()
	issueDir := filepath.Join(repoRoot, "docs", "issues")
	require.NoError(t, createDir(issueDir))

	content := `# [p0][events][tech-debt] closed without evidence

labels: ` + "`p0`, `type:tech-debt`" + `
state: closed

## summary
closed but no evidence

## acceptance criteria
- [x] done
`
	require.NoError(t, writeFile(filepath.Join(issueDir, "closed-no-ev.md"), content))

	eval, err := EvaluateGateItem("docs/issues/closed-no-ev.md", repoRoot)
	require.NoError(t, err)

	assert.Equal(t, StateClosed, eval.State)
	assert.Contains(t, eval.MissingRequirements, MissingEvidenceRefs)
	assert.Contains(t, eval.MissingRequirements, MissingClosureEvidenceBlock)
}

func TestEvaluateGateItem_AcceptanceIncompleteTakesPrecedence(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	issueDir := filepath.Join(repoRoot, "docs", "issues")
	require.NoError(t, createDir(issueDir))

	content := `# [p1][cli][tech-debt] incomplete acceptance

labels: ` + "`p1`, `type:tech-debt`" + `

## summary
incomplete

## acceptance criteria
- [ ] not done yet
- [x] this one is done
`
	require.NoError(t, writeFile(filepath.Join(issueDir, "half-done.md"), content))

	eval, err := EvaluateGateItem("docs/issues/half-done.md", repoRoot)
	require.NoError(t, err)

	assert.False(t, eval.AcceptanceComplete)
	assert.Contains(t, eval.MissingRequirements, MissingAcceptanceIncomplete)
	// Acceptance incomplete has highest precedence.
	assert.Equal(t, agencyerrors.EGateItemAcceptanceIncomplete, eval.BlockingCode)
}

// createDir creates all directories in the path.
func createDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// writeFile writes content to a file.
func writeFile(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
