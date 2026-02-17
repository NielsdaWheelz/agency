package s1gates

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

func TestParseIssue_NormalizesPriorityTypeAndState(t *testing.T) {
	t.Parallel()

	content := readTestdata(t, "issue_valid_p1_bug.md")
	ref, ce, err := ParseIssue(content, "docs/issues/test.md")
	require.NoError(t, err)

	assert.Equal(t, "docs/issues/test.md", ref.IssuePath)
	assert.Equal(t, PriorityP1, ref.Priority)
	assert.Equal(t, TypeBug, ref.Type)
	assert.Equal(t, StateReadyForVerification, ref.State)
	assert.True(t, ref.AcceptanceComplete, "all checkboxes are checked")
	assert.False(t, ref.RequiresGHE2E)
	assert.Nil(t, ce, "no closure evidence section")
}

func TestParseIssue_DefaultStateOpenWhenMissingStateMetadata(t *testing.T) {
	t.Parallel()

	content := readTestdata(t, "issue_no_state.md")
	ref, _, err := ParseIssue(content, "docs/issues/test.md")
	require.NoError(t, err)

	assert.Equal(t, StateOpen, ref.State)
	assert.Equal(t, PriorityP1, ref.Priority)
	assert.Equal(t, TypeTechDebt, ref.Type)
	assert.False(t, ref.AcceptanceComplete, "has unchecked checkbox")
}

func TestParseIssue_ParsesClosureEvidenceJSON(t *testing.T) {
	t.Parallel()

	content := readTestdata(t, "issue_with_closure.md")
	ref, ce, err := ParseIssue(content, "docs/issues/events-p0-event-system-hardening.md")
	require.NoError(t, err)

	assert.Equal(t, PriorityP0, ref.Priority)
	assert.Equal(t, TypeTechDebt, ref.Type)
	assert.Equal(t, StateClosed, ref.State)
	assert.True(t, ref.AcceptanceComplete)

	// Closure evidence parsed.
	require.NotNil(t, ce)
	assert.Equal(t, "docs/issues/events-p0-event-system-hardening.md", ce.IssuePath)
	assert.Equal(t, []string{"pr:101", "commit:abc123"}, ce.ImplementedRefs)
	require.Len(t, ce.TargetedTestRefs, 1)
	assert.Equal(t, ScopeTargeted, ce.TargetedTestRefs[0].Scope)
	assert.Equal(t, ResultPass, ce.TargetedTestRefs[0].Result)
	require.Len(t, ce.SuiteTestRefs, 1)
	assert.Equal(t, ScopeSuite, ce.SuiteTestRefs[0].Scope)
	assert.Equal(t, ResultPass, ce.SuiteTestRefs[0].Result)

	// Evidence refs populated from implemented_refs.
	assert.Equal(t, []string{"pr:101", "commit:abc123"}, ref.EvidenceRefs)
}

func TestParseIssue_MissingAcceptanceSectionReturnsEGateItemInvalid(t *testing.T) {
	t.Parallel()

	content := readTestdata(t, "issue_no_acceptance.md")
	ref, ce, err := ParseIssue(content, "docs/issues/test.md")
	assert.Nil(t, ref)
	assert.Nil(t, ce)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "acceptance criteria")
}

func TestParseIssue_PassEvidenceRequiresRecordedAt(t *testing.T) {
	t.Parallel()

	content := readTestdata(t, "issue_pass_no_recorded_at.md")
	ref, ce, err := ParseIssue(content, "docs/issues/test.md")
	assert.Nil(t, ref)
	assert.Nil(t, ce)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "recorded_at")
}

func TestParseIssue_SuiteScopeRequiresAllowedCommand(t *testing.T) {
	t.Parallel()

	content := readTestdata(t, "issue_bad_suite_cmd.md")
	ref, ce, err := ParseIssue(content, "docs/issues/test.md")
	assert.Nil(t, ref)
	assert.Nil(t, ce)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "npm test")
}

func TestParseIssue_PriorityFallsBackToTitleTag(t *testing.T) {
	t.Parallel()

	// Labels provide type but not priority; priority comes from title tag.
	content := "# [p0][events][tech-debt] event hardening\n\nlabels: `type:tech-debt`\n\n## summary\ntest\n\n## acceptance criteria\n- [ ] todo\n"
	ref, _, err := ParseIssue(content, "docs/issues/test.md")
	require.NoError(t, err)
	assert.Equal(t, PriorityP0, ref.Priority)
}

func TestParseIssue_LabelPriorityOverridesTitleTag(t *testing.T) {
	t.Parallel()

	// Title says [p0] but labels say p1 — label wins.
	content := "# [p0][events][tech-debt] event hardening\n\nlabels: `p1`, `type:tech-debt`\n\n## summary\ntest\n\n## acceptance criteria\n- [ ] todo\n"
	ref, _, err := ParseIssue(content, "docs/issues/test.md")
	require.NoError(t, err)
	assert.Equal(t, PriorityP1, ref.Priority)
}

func TestParseIssue_RequiresGHE2EFromLabel(t *testing.T) {
	t.Parallel()

	content := "# [p1][daemon][bug] pr mutation test\n\nlabels: `p1`, `type:bug`, `requires:gh-e2e`\n\n## summary\ntest\n\n## acceptance criteria\n- [ ] todo\n"
	ref, _, err := ParseIssue(content, "docs/issues/test.md")
	require.NoError(t, err)
	assert.True(t, ref.RequiresGHE2E)
}

func TestParseIssue_MissingTitleReturnsInvalid(t *testing.T) {
	t.Parallel()

	content := "no title here\n\n## acceptance criteria\n- [ ] todo\n"
	ref, ce, err := ParseIssue(content, "docs/issues/test.md")
	assert.Nil(t, ref)
	assert.Nil(t, ce)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemInvalid, agencyerrors.GetCode(err))
}

func TestParseIssue_InvalidScopeReturnsEGateItemInvalid(t *testing.T) {
	t.Parallel()

	content := `# [p0][events][tech-debt] event hardening

labels: ` + "`p0`, `type:tech-debt`" + `
state: closed

## summary
event hardening

## acceptance criteria
- [x] done

## closure evidence

` + "```json" + `
{
  "implemented_refs": ["pr:101"],
  "targeted_test_refs": [
    {
      "issue_path": "docs/issues/test.md",
      "command": "go test ./internal/events/...",
      "scope": "unknown_scope",
      "result": "pass",
      "artifact_ref": "ci:build-100",
      "recorded_at": "2026-02-15T10:00:00Z"
    }
  ],
  "suite_test_refs": []
}
` + "```" + `
`
	ref, ce, err := ParseIssue(content, "docs/issues/test.md")
	assert.Nil(t, ref)
	assert.Nil(t, ce)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "invalid scope")
}

func TestParseIssue_InvalidResultReturnsEGateItemInvalid(t *testing.T) {
	t.Parallel()

	content := `# [p0][events][tech-debt] event hardening

labels: ` + "`p0`, `type:tech-debt`" + `
state: closed

## summary
event hardening

## acceptance criteria
- [x] done

## closure evidence

` + "```json" + `
{
  "implemented_refs": ["pr:101"],
  "targeted_test_refs": [
    {
      "issue_path": "docs/issues/test.md",
      "command": "go test ./internal/events/...",
      "scope": "targeted",
      "result": "maybe",
      "artifact_ref": "ci:build-100",
      "recorded_at": "2026-02-15T10:00:00Z"
    }
  ],
  "suite_test_refs": []
}
` + "```" + `
`
	ref, ce, err := ParseIssue(content, "docs/issues/test.md")
	assert.Nil(t, ref)
	assert.Nil(t, ce)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "invalid result")
}

func TestParseIssue_InvalidExplicitStateReturnsEGateItemInvalid(t *testing.T) {
	t.Parallel()

	content := `# [p1][cli][tech-debt] some issue

labels: ` + "`p1`, `type:tech-debt`" + `
state: invalid_state

## summary
test

## acceptance criteria
- [ ] todo
`
	ref, ce, err := ParseIssue(content, "docs/issues/test.md")
	assert.Nil(t, ref)
	assert.Nil(t, ce)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "invalid explicit state")
}

func TestParseIssue_MissingPriorityReturnsEGateItemInvalid(t *testing.T) {
	t.Parallel()

	content := `# [events][tech-debt] event hardening

labels: ` + "`type:tech-debt`" + `

## summary
test

## acceptance criteria
- [ ] todo
`
	ref, ce, err := ParseIssue(content, "docs/issues/test.md")
	assert.Nil(t, ref)
	assert.Nil(t, ce)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "missing priority")
}

func TestParseIssue_MissingTypeReturnsEGateItemInvalid(t *testing.T) {
	t.Parallel()

	content := `# [p1][cli] some issue

labels: ` + "`p1`" + `

## summary
test

## acceptance criteria
- [ ] todo
`
	ref, ce, err := ParseIssue(content, "docs/issues/test.md")
	assert.Nil(t, ref)
	assert.Nil(t, ce)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "missing type")
}
