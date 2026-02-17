package s1gates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// --- fixture helpers ---

// setupTransitionRepo creates a temp repo with a release-gates source and gate
// items in various states suitable for transition tests.
func setupTransitionRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs/v2.1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs/issues"), 0o755))

	// Release-gates source.
	writeFixture(t, root, CanonicalGateSourcePath, releaseGatesFixture)

	// Gate A items (p0).
	writeFixture(t, root, "docs/issues/gate-a-open.md", gateAOpenFixture)
	writeFixture(t, root, "docs/issues/gate-a-rfv.md", gateARfvFixture)

	// Gate B items (p1).
	writeFixture(t, root, "docs/issues/gate-b-open.md", gateBOpenFixture)
	writeFixture(t, root, "docs/issues/gate-b-in-progress.md", gateBInProgressFixture)
	writeFixture(t, root, "docs/issues/gate-b-rfv.md", gateBRfvFixture)
	writeFixture(t, root, "docs/issues/gate-b-closed.md", gateBClosedFixture)
	writeFixture(t, root, "docs/issues/gate-b-design.md", gateBDesignFixture)
	writeFixture(t, root, "docs/issues/gate-b-gh-e2e.md", gateBGHE2EFixture)

	// Non-member issue (valid but not in release-gates).
	writeFixture(t, root, "docs/issues/non-member.md", nonMemberFixture)

	return root
}

func writeFixture(t *testing.T, root, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(root, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
}

// --- fixtures ---

const releaseGatesFixture = `# Release Gates

## Gate A: P0 safety closure

1. ` + "`docs/issues/gate-a-open.md`" + `
2. ` + "`docs/issues/gate-a-rfv.md`" + `

## Gate B: parity-critical P1 closure

1. ` + "`docs/issues/gate-b-open.md`" + `
2. ` + "`docs/issues/gate-b-in-progress.md`" + `
3. ` + "`docs/issues/gate-b-rfv.md`" + `
4. ` + "`docs/issues/gate-b-closed.md`" + `
5. ` + "`docs/issues/gate-b-design.md`" + `
6. ` + "`docs/issues/gate-b-gh-e2e.md`" + `
`

const gateAOpenFixture = `# [p0][events][tech-debt] gate a open

labels: ` + "`p0`, `type:tech-debt`" + `
state: open

## summary
gate a open item

## acceptance criteria
- [ ] not done
`

const gateARfvFixture = `# [p0][store][tech-debt] gate a ready for verification

labels: ` + "`p0`, `type:tech-debt`" + `
state: ready_for_verification

## summary
gate a rfv item

## acceptance criteria
- [x] done

## closure evidence

` + "```json" + `
{
  "implemented_refs": ["pr:200", "commit:def456"],
  "targeted_test_refs": [
    {
      "issue_path": "docs/issues/gate-a-rfv.md",
      "command": "go test ./internal/store/...",
      "scope": "targeted",
      "result": "pass",
      "artifact_ref": "ci:build-200",
      "recorded_at": "2026-02-16T10:00:00Z"
    }
  ],
  "suite_test_refs": [
    {
      "issue_path": "docs/issues/gate-a-rfv.md",
      "command": "go test ./...",
      "scope": "suite",
      "result": "pass",
      "artifact_ref": "ci:build-201",
      "recorded_at": "2026-02-16T11:00:00Z"
    }
  ]
}
` + "```" + `
`

const gateBOpenFixture = `# [p1][cli][tech-debt] gate b open

labels: ` + "`p1`, `type:tech-debt`" + `
state: open

## summary
gate b open item

## acceptance criteria
- [ ] not done
`

const gateBInProgressFixture = `# [p1][daemon][bug] gate b in progress

labels: ` + "`p1`, `type:bug`" + `
state: in_progress

## summary
gate b in progress item

## acceptance criteria
- [x] done

## closure evidence

` + "```json" + `
{
  "implemented_refs": ["pr:300"],
  "targeted_test_refs": [
    {
      "issue_path": "docs/issues/gate-b-in-progress.md",
      "command": "go test ./internal/daemon/...",
      "scope": "targeted",
      "result": "pass",
      "artifact_ref": "ci:build-300",
      "recorded_at": "2026-02-16T10:00:00Z"
    }
  ],
  "suite_test_refs": [
    {
      "issue_path": "docs/issues/gate-b-in-progress.md",
      "command": "go test ./...",
      "scope": "suite",
      "result": "pass",
      "artifact_ref": "ci:build-301",
      "recorded_at": "2026-02-16T11:00:00Z"
    }
  ]
}
` + "```" + `
`

const gateBRfvFixture = `# [p1][cli][tech-debt] gate b ready for verification

labels: ` + "`p1`, `type:tech-debt`" + `
state: ready_for_verification

## summary
gate b rfv item

## acceptance criteria
- [x] done

## closure evidence

` + "```json" + `
{
  "implemented_refs": ["pr:301", "commit:ghi789"],
  "targeted_test_refs": [
    {
      "issue_path": "docs/issues/gate-b-rfv.md",
      "command": "go test ./internal/cli/...",
      "scope": "targeted",
      "result": "pass",
      "artifact_ref": "ci:build-302",
      "recorded_at": "2026-02-16T10:00:00Z"
    }
  ],
  "suite_test_refs": [
    {
      "issue_path": "docs/issues/gate-b-rfv.md",
      "command": "go test ./...",
      "scope": "suite",
      "result": "pass",
      "artifact_ref": "ci:build-303",
      "recorded_at": "2026-02-16T11:00:00Z"
    }
  ]
}
` + "```" + `
`

const gateBClosedFixture = `# [p1][daemon][bug] gate b closed

labels: ` + "`p1`, `type:bug`" + `
state: closed

## summary
gate b closed item

## acceptance criteria
- [x] done

## closure evidence

` + "```json" + `
{
  "implemented_refs": ["pr:400", "commit:jkl012"],
  "targeted_test_refs": [
    {
      "issue_path": "docs/issues/gate-b-closed.md",
      "command": "go test ./internal/daemon/...",
      "scope": "targeted",
      "result": "pass",
      "artifact_ref": "ci:build-400",
      "recorded_at": "2026-02-16T10:00:00Z"
    }
  ],
  "suite_test_refs": [
    {
      "issue_path": "docs/issues/gate-b-closed.md",
      "command": "go test ./...",
      "scope": "suite",
      "result": "pass",
      "artifact_ref": "ci:build-401",
      "recorded_at": "2026-02-16T11:00:00Z"
    }
  ]
}
` + "```" + `
`

const gateBDesignFixture = `# [p1][contracts][design] gate b design item

labels: ` + "`p1`, `type:design`" + `
state: ready_for_verification

## summary
design enforcement item

## acceptance criteria
- [x] done

## closure evidence

` + "```json" + `
{
  "implemented_refs": ["pr:500"],
  "targeted_test_refs": [],
  "suite_test_refs": []
}
` + "```" + `
`

const gateBGHE2EFixture = `# [p1][daemon][bug] gate b gh e2e item

labels: ` + "`p1`, `type:bug`, `requires:gh-e2e`" + `
state: ready_for_verification

## summary
gh e2e required item

## acceptance criteria
- [x] done

## closure evidence

` + "```json" + `
{
  "implemented_refs": ["pr:600"],
  "targeted_test_refs": [
    {
      "issue_path": "docs/issues/gate-b-gh-e2e.md",
      "command": "go test ./internal/daemon/...",
      "scope": "targeted",
      "result": "pass",
      "artifact_ref": "ci:build-600",
      "recorded_at": "2026-02-16T10:00:00Z"
    }
  ],
  "suite_test_refs": [
    {
      "issue_path": "docs/issues/gate-b-gh-e2e.md",
      "command": "go test ./...",
      "scope": "suite",
      "result": "pass",
      "artifact_ref": "ci:build-601",
      "recorded_at": "2026-02-16T11:00:00Z"
    }
  ]
}
` + "```" + `
`

const nonMemberFixture = `# [p1][misc][bug] non member issue

labels: ` + "`p1`, `type:bug`" + `
state: open

## summary
not in any gate

## acceptance criteria
- [ ] todo
`

// --- tests ---

func TestTransition_EnforcesLegalAndIllegalTransitions(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)

	legalCases := []struct {
		name     string
		path     string
		from, to string
	}{
		{"open->in_progress", "docs/issues/gate-b-open.md", StateOpen, StateInProgress},
		{"in_progress->rfv", "docs/issues/gate-b-in-progress.md", StateInProgress, StateReadyForVerification},
		{"rfv->closed (gate b reviewer)", "docs/issues/gate-b-rfv.md", StateReadyForVerification, StateClosed},
		{"closed->in_progress (reopen)", "docs/issues/gate-b-closed.md", StateClosed, StateInProgress},
	}

	for _, tc := range legalCases {
		tc := tc
		t.Run("legal_"+tc.name, func(t *testing.T) {
			t.Parallel()
			localRoot := copyRepo(t, root)

			req := GateTransition{
				IssuePath:    tc.path,
				FromState:    tc.from,
				ToState:      tc.to,
				ActorRole:    RoleMaintainer,
				Reason:       "test reason",
				EvidenceRefs: []string{"pr:999"},
			}
			result, err := TransitionGateItem(req, localRoot)
			require.NoError(t, err, "legal transition %s should succeed", tc.name)
			assert.Equal(t, tc.to, result.State)
			assert.Equal(t, tc.path, result.IssuePath)
		})
	}

	illegalEdges := []struct {
		name     string
		from, to string
	}{
		{"open->closed", StateOpen, StateClosed},
		{"open->rfv", StateOpen, StateReadyForVerification},
		{"in_progress->closed", StateInProgress, StateClosed},
		{"closed->rfv", StateClosed, StateReadyForVerification},
	}

	for _, tc := range illegalEdges {
		tc := tc
		t.Run("illegal_"+tc.name, func(t *testing.T) {
			t.Parallel()
			localRoot := copyRepo(t, root)

			// Pick an item that's actually in the from_state.
			path := pathForState(tc.from)
			// Overwrite to ensure correct from_state.
			overwriteState(t, localRoot, path, tc.from)

			req := GateTransition{
				IssuePath:    path,
				FromState:    tc.from,
				ToState:      tc.to,
				ActorRole:    RoleMaintainer,
				Reason:       "test",
				EvidenceRefs: []string{"pr:999"},
			}
			_, err := TransitionGateItem(req, localRoot)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateTransitionInvalid, agencyerrors.GetCode(err))
		})
	}
}

func TestTransition_RejectsNoOpTransition(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)

	states := []struct {
		state string
		path  string
	}{
		{StateOpen, "docs/issues/gate-b-open.md"},
		{StateInProgress, "docs/issues/gate-b-in-progress.md"},
		{StateReadyForVerification, "docs/issues/gate-b-rfv.md"},
		{StateClosed, "docs/issues/gate-b-closed.md"},
	}

	for _, tc := range states {
		tc := tc
		t.Run(tc.state, func(t *testing.T) {
			t.Parallel()
			localRoot := copyRepo(t, root)

			req := GateTransition{
				IssuePath:    tc.path,
				FromState:    tc.state,
				ToState:      tc.state,
				ActorRole:    RoleMaintainer,
				Reason:       "no-op",
				EvidenceRefs: []string{"pr:999"},
			}
			_, err := TransitionGateItem(req, localRoot)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateTransitionInvalid, agencyerrors.GetCode(err))
		})
	}
}

func TestTransition_FromStateMustMatchCurrentState(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)
	localRoot := copyRepo(t, root)

	// gate-b-open.md is in state=open; claim from_state=in_progress.
	req := GateTransition{
		IssuePath:    "docs/issues/gate-b-open.md",
		FromState:    StateInProgress,
		ToState:      StateReadyForVerification,
		ActorRole:    RoleMaintainer,
		Reason:       "",
		EvidenceRefs: nil,
	}
	_, err := TransitionGateItem(req, localRoot)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateTransitionInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "does not match current state")
}

func TestTransition_InvalidActorRoleReturnsTransitionInvalid(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)
	localRoot := copyRepo(t, root)

	req := GateTransition{
		IssuePath:    "docs/issues/gate-b-open.md",
		FromState:    StateOpen,
		ToState:      StateInProgress,
		ActorRole:    "owner",
		Reason:       "",
		EvidenceRefs: nil,
	}
	_, err := TransitionGateItem(req, localRoot)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateTransitionInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "invalid actor_role")
}

func TestTransition_NonReopenAllowsEmptyReason(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)
	localRoot := copyRepo(t, root)

	req := GateTransition{
		IssuePath:    "docs/issues/gate-b-open.md",
		FromState:    StateOpen,
		ToState:      StateInProgress,
		ActorRole:    RoleMaintainer,
		Reason:       "",
		EvidenceRefs: nil,
	}
	result, err := TransitionGateItem(req, localRoot)
	require.NoError(t, err)
	assert.Equal(t, StateInProgress, result.State)
}

func TestTransition_RejectsIssueOutsideGateMembership(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)
	localRoot := copyRepo(t, root)

	req := GateTransition{
		IssuePath:    "docs/issues/non-member.md",
		FromState:    StateOpen,
		ToState:      StateInProgress,
		ActorRole:    RoleMaintainer,
		Reason:       "",
		EvidenceRefs: nil,
	}
	_, err := TransitionGateItem(req, localRoot)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateTransitionInvalid, agencyerrors.GetCode(err))
	assert.Contains(t, err.Error(), "not a Gate A or Gate B member")
}

func TestTransition_CloseRequiresAcceptanceAndTestsAndEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		wantCode agencyerrors.Code
	}{
		{
			name: "missing_acceptance",
			fixture: `# [p1][cli][tech-debt] incomplete acceptance

labels: ` + "`p1`, `type:tech-debt`" + `
state: ready_for_verification

## summary
test

## acceptance criteria
- [ ] not done

## closure evidence

` + "```json" + `
{
  "implemented_refs": ["pr:100"],
  "targeted_test_refs": [
    {"issue_path":"x","command":"go test ./...","scope":"targeted","result":"pass","artifact_ref":"ci:1","recorded_at":"2026-01-01T00:00:00Z"}
  ],
  "suite_test_refs": [
    {"issue_path":"x","command":"go test ./...","scope":"suite","result":"pass","artifact_ref":"ci:2","recorded_at":"2026-01-01T00:00:00Z"}
  ]
}
` + "```" + `
`,
			wantCode: agencyerrors.EGateItemAcceptanceIncomplete,
		},
		{
			name: "missing_tests",
			fixture: `# [p1][cli][tech-debt] no tests

labels: ` + "`p1`, `type:tech-debt`" + `
state: ready_for_verification

## summary
test

## acceptance criteria
- [x] done

## closure evidence

` + "```json" + `
{
  "implemented_refs": ["pr:100"],
  "targeted_test_refs": [],
  "suite_test_refs": []
}
` + "```" + `
`,
			wantCode: agencyerrors.EGateItemTestsIncomplete,
		},
		{
			name: "missing_closure_block",
			fixture: `# [p1][cli][tech-debt] no closure block

labels: ` + "`p1`, `type:tech-debt`" + `
state: ready_for_verification

## summary
test

## acceptance criteria
- [x] done
`,
			wantCode: agencyerrors.EGateItemClosureBlockMissing,
		},
		{
			name: "missing_evidence_refs",
			fixture: `# [p1][cli][tech-debt] empty implemented refs

labels: ` + "`p1`, `type:tech-debt`" + `
state: ready_for_verification

## summary
test

## acceptance criteria
- [x] done

## closure evidence

` + "```json" + `
{
  "implemented_refs": [],
  "targeted_test_refs": [
    {"issue_path":"x","command":"go test ./...","scope":"targeted","result":"pass","artifact_ref":"ci:1","recorded_at":"2026-01-01T00:00:00Z"}
  ],
  "suite_test_refs": [
    {"issue_path":"x","command":"go test ./...","scope":"suite","result":"pass","artifact_ref":"ci:2","recorded_at":"2026-01-01T00:00:00Z"}
  ]
}
` + "```" + `
`,
			wantCode: agencyerrors.EGateItemEvidenceMissing,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := setupTransitionRepo(t)
			// Overwrite gate-b-rfv with deficient fixture.
			writeFixture(t, root, "docs/issues/gate-b-rfv.md", tc.fixture)

			req := GateTransition{
				IssuePath:    "docs/issues/gate-b-rfv.md",
				FromState:    StateReadyForVerification,
				ToState:      StateClosed,
				ActorRole:    RoleMaintainer,
				Reason:       "",
				EvidenceRefs: []string{"pr:100"},
			}
			_, err := TransitionGateItem(req, root)
			require.Error(t, err)
			assert.Equal(t, tc.wantCode, agencyerrors.GetCode(err),
				"expected %s for %s", tc.wantCode, tc.name)
		})
	}
}

func TestTransition_P0CloseRequiresMaintainer(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)

	roles := []string{RoleReviewer, RoleContributor}
	for _, role := range roles {
		role := role
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			localRoot := copyRepo(t, root)

			req := GateTransition{
				IssuePath:    "docs/issues/gate-a-rfv.md",
				FromState:    StateReadyForVerification,
				ToState:      StateClosed,
				ActorRole:    role,
				Reason:       "",
				EvidenceRefs: []string{"pr:200"},
			}
			_, err := TransitionGateItem(req, localRoot)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateApprovalRequired, agencyerrors.GetCode(err))
		})
	}
}

func TestTransition_GateBCloseByContributorRejected(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)
	localRoot := copyRepo(t, root)

	req := GateTransition{
		IssuePath:    "docs/issues/gate-b-rfv.md",
		FromState:    StateReadyForVerification,
		ToState:      StateClosed,
		ActorRole:    RoleContributor,
		Reason:       "",
		EvidenceRefs: []string{"pr:301"},
	}
	_, err := TransitionGateItem(req, localRoot)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateApprovalRequired, agencyerrors.GetCode(err))
}

func TestTransition_CloseSucceedsForGateBReviewer(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)
	localRoot := copyRepo(t, root)

	req := GateTransition{
		IssuePath:    "docs/issues/gate-b-rfv.md",
		FromState:    StateReadyForVerification,
		ToState:      StateClosed,
		ActorRole:    RoleReviewer,
		Reason:       "",
		EvidenceRefs: []string{"pr:301"},
	}
	result, err := TransitionGateItem(req, localRoot)
	require.NoError(t, err)
	assert.Equal(t, StateClosed, result.State)

	// Verify persisted state.
	data, err := os.ReadFile(filepath.Join(localRoot, "docs/issues/gate-b-rfv.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "state: closed")
}

func TestTransition_PersistsStateByReplacingExistingStateLine(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)
	localRoot := copyRepo(t, root)

	// gate-b-in-progress.md has state: in_progress
	req := GateTransition{
		IssuePath:    "docs/issues/gate-b-in-progress.md",
		FromState:    StateInProgress,
		ToState:      StateReadyForVerification,
		ActorRole:    RoleMaintainer,
		Reason:       "",
		EvidenceRefs: nil,
	}
	result, err := TransitionGateItem(req, localRoot)
	require.NoError(t, err)
	assert.Equal(t, StateReadyForVerification, result.State)

	data, err := os.ReadFile(filepath.Join(localRoot, "docs/issues/gate-b-in-progress.md"))
	require.NoError(t, err)
	content := string(data)

	// Exactly one state: line exists.
	assert.Equal(t, 1, strings.Count(content, "state: "), "should have exactly one state: line")
	assert.Contains(t, content, "state: ready_for_verification")
	assert.NotContains(t, content, "state: in_progress")
}

func TestTransition_PersistsStateByInsertingAfterLabelsWhenStateMissing(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)

	// Create an issue with labels: but no state: line.
	noStateFixture := `# [p1][cli][tech-debt] no state line

labels: ` + "`p1`, `type:tech-debt`" + `

## summary
test

## acceptance criteria
- [ ] not done
`
	writeFixture(t, root, "docs/issues/gate-b-open.md", noStateFixture)

	req := GateTransition{
		IssuePath:    "docs/issues/gate-b-open.md",
		FromState:    StateOpen,
		ToState:      StateInProgress,
		ActorRole:    RoleMaintainer,
		Reason:       "",
		EvidenceRefs: nil,
	}
	result, err := TransitionGateItem(req, root)
	require.NoError(t, err)
	assert.Equal(t, StateInProgress, result.State)

	data, err := os.ReadFile(filepath.Join(root, "docs/issues/gate-b-open.md"))
	require.NoError(t, err)
	content := string(data)

	// New state: line inserted after labels: line.
	lines := strings.Split(content, "\n")
	labelsIdx := -1
	stateIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "labels:") {
			labelsIdx = i
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "state:") {
			stateIdx = i
		}
	}
	require.NotEqual(t, -1, labelsIdx, "labels: line should exist")
	require.NotEqual(t, -1, stateIdx, "state: line should be inserted")
	assert.Equal(t, labelsIdx+1, stateIdx, "state: should immediately follow labels:")
	assert.Contains(t, lines[stateIdx], "state: in_progress")
}

func TestTransition_ReopenRequiresReasonAndEvidence(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)

	tests := []struct {
		name         string
		reason       string
		evidenceRefs []string
	}{
		{"missing_reason", "", []string{"pr:999"}},
		{"missing_evidence", "regression found", nil},
		{"missing_both", "", nil},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			localRoot := copyRepo(t, root)

			req := GateTransition{
				IssuePath:    "docs/issues/gate-b-closed.md",
				FromState:    StateClosed,
				ToState:      StateInProgress,
				ActorRole:    RoleMaintainer,
				Reason:       tc.reason,
				EvidenceRefs: tc.evidenceRefs,
			}
			_, err := TransitionGateItem(req, localRoot)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateReopenReasonRequired, agencyerrors.GetCode(err))
		})
	}
}

func TestTransition_RequiresGHE2EWhenFlagged(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)
	localRoot := copyRepo(t, root)

	// gate-b-gh-e2e.md has requires:gh-e2e but no e2e_opt_in evidence.
	req := GateTransition{
		IssuePath:    "docs/issues/gate-b-gh-e2e.md",
		FromState:    StateReadyForVerification,
		ToState:      StateClosed,
		ActorRole:    RoleMaintainer,
		Reason:       "",
		EvidenceRefs: []string{"pr:600"},
	}
	_, err := TransitionGateItem(req, localRoot)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateE2ERequired, agencyerrors.GetCode(err))
}

func TestTransition_DesignCloseRequiresEnforcementEvidence(t *testing.T) {
	t.Parallel()
	root := setupTransitionRepo(t)
	localRoot := copyRepo(t, root)

	// gate-b-design.md has type=design but empty targeted/suite refs (no enforcement evidence).
	req := GateTransition{
		IssuePath:    "docs/issues/gate-b-design.md",
		FromState:    StateReadyForVerification,
		ToState:      StateClosed,
		ActorRole:    RoleMaintainer,
		Reason:       "",
		EvidenceRefs: []string{"pr:500"},
	}
	_, err := TransitionGateItem(req, localRoot)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateItemTestsIncomplete, agencyerrors.GetCode(err))
}

func TestTransition_ErrorPrecedenceIsDeterministic(t *testing.T) {
	t.Parallel()

	t.Run("from_state_mismatch_before_illegal_edge", func(t *testing.T) {
		t.Parallel()
		root := setupTransitionRepo(t)
		localRoot := copyRepo(t, root)

		// gate-b-open is in state=open; claim from_state=in_progress with illegal edge in_progress->closed.
		req := GateTransition{
			IssuePath:    "docs/issues/gate-b-open.md",
			FromState:    StateInProgress,
			ToState:      StateClosed,
			ActorRole:    RoleMaintainer,
			Reason:       "",
			EvidenceRefs: nil,
		}
		_, err := TransitionGateItem(req, localRoot)
		require.Error(t, err)
		// from_state mismatch (precedence 2) takes priority over illegal edge (precedence 3).
		assert.Equal(t, agencyerrors.EGateTransitionInvalid, agencyerrors.GetCode(err))
		assert.Contains(t, err.Error(), "does not match current state")
	})

	t.Run("illegal_edge_before_invalid_role", func(t *testing.T) {
		t.Parallel()
		root := setupTransitionRepo(t)
		localRoot := copyRepo(t, root)

		// gate-b-open is open; try open->closed (illegal) with bad role.
		req := GateTransition{
			IssuePath:    "docs/issues/gate-b-open.md",
			FromState:    StateOpen,
			ToState:      StateClosed,
			ActorRole:    "owner",
			Reason:       "",
			EvidenceRefs: nil,
		}
		_, err := TransitionGateItem(req, localRoot)
		require.Error(t, err)
		// Illegal edge (precedence 3) before invalid role (precedence 4).
		assert.Equal(t, agencyerrors.EGateTransitionInvalid, agencyerrors.GetCode(err))
		assert.Contains(t, err.Error(), "illegal transition")
	})

	t.Run("closure_blocking_code_before_role_policy", func(t *testing.T) {
		t.Parallel()
		root := setupTransitionRepo(t)

		// Create rfv item with incomplete acceptance (so blocking code applies) and contributor role.
		fixture := `# [p1][cli][tech-debt] both failures

labels: ` + "`p1`, `type:tech-debt`" + `
state: ready_for_verification

## summary
test

## acceptance criteria
- [ ] not done
`
		writeFixture(t, root, "docs/issues/gate-b-rfv.md", fixture)

		req := GateTransition{
			IssuePath:    "docs/issues/gate-b-rfv.md",
			FromState:    StateReadyForVerification,
			ToState:      StateClosed,
			ActorRole:    RoleContributor,
			Reason:       "",
			EvidenceRefs: []string{"pr:301"},
		}
		_, err := TransitionGateItem(req, root)
		require.Error(t, err)
		// Blocking code (closure guard 1) before role policy (closure guard 3).
		assert.Equal(t, agencyerrors.EGateItemAcceptanceIncomplete, agencyerrors.GetCode(err))
	})
}

// --- copy helper ---

// copyRepo creates a deep copy of the fixture repo in a new temp dir.
func copyRepo(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
	require.NoError(t, err)
	return dst
}

// pathForState returns a gate member issue path whose fixture is in the given state.
func pathForState(state string) string {
	switch state {
	case StateOpen:
		return "docs/issues/gate-b-open.md"
	case StateInProgress:
		return "docs/issues/gate-b-in-progress.md"
	case StateReadyForVerification:
		return "docs/issues/gate-b-rfv.md"
	case StateClosed:
		return "docs/issues/gate-b-closed.md"
	default:
		return "docs/issues/gate-b-open.md"
	}
}

// overwriteState replaces the state: line (or inserts one) in the fixture at the given path.
func overwriteState(t *testing.T, root, relPath, newState string) {
	t.Helper()
	fullPath := filepath.Join(root, relPath)
	data, err := os.ReadFile(fullPath)
	require.NoError(t, err)

	content := string(data)
	lines := strings.Split(content, "\n")
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if stateRe.MatchString(trimmed) {
			lines[i] = "state: " + newState
			replaced = true
			break
		}
	}
	if !replaced {
		// Insert after labels or title.
		idx := findInsertIndex(lines)
		lines = insertLineAfter(lines, idx, "state: "+newState)
	}
	require.NoError(t, os.WriteFile(fullPath, []byte(strings.Join(lines, "\n")), 0o644))
}
