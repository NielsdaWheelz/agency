package releasegates

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "testdata")
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdataDir(), name))
	require.NoError(t, err)
	return string(data)
}

// --- Release Readiness ---

func TestEvaluateReleaseReadiness_UsesRequireSliceReady(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Join(testdataDir(), "repo_gates_eval", "valid_blocked")
	source := NewMarkdownIssueSource(repoRoot)
	svc := NewService(source)

	_, err := svc.EvaluateReleaseReadiness(ReleaseReadinessRequest{Slice: "S1"}, repoRoot)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateBlocked, agencyerrors.GetCode(err))

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.NotEmpty(t, ae.Details["gate_a_status"])
	assert.NotEmpty(t, ae.Details["gate_b_status"])
	assert.NotEmpty(t, ae.Details["gate_a_closed_items"])
	assert.NotEmpty(t, ae.Details["gate_a_total_items"])
	assert.NotEmpty(t, ae.Details["gate_b_closed_items"])
	assert.NotEmpty(t, ae.Details["gate_b_total_items"])
}

func TestEvaluateReleaseReadiness_Ready(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Join(testdataDir(), "repo_gates_eval", "valid_all_closed")
	source := NewMarkdownIssueSource(repoRoot)
	svc := NewService(source)

	result, err := svc.EvaluateReleaseReadiness(ReleaseReadinessRequest{Slice: "S1"}, repoRoot)
	require.NoError(t, err)
	assert.True(t, result.SliceReady)
	assert.Equal(t, GateStatusReady, result.GateA.Status)
	assert.Equal(t, GateStatusReady, result.GateB.Status)
}

// --- Migration Parity ---

func TestReleaseGatesMigration_BehaviorParityWithLegacy(t *testing.T) {
	t.Parallel()

	t.Run("source_parser_valid", func(t *testing.T) {
		content := readTestdata(t, "corpus_valid.md")
		repoRoot := filepath.Join(testdataDir(), "repo_valid")
		fileExists := RepoFileExists(repoRoot)

		gs, err := ParseGateSet(content, "docs/v2.1/release-gates.md", fileExists)
		require.NoError(t, err)
		assert.Equal(t, "S1", gs.Slice)
		assert.Len(t, gs.GateAItems, 2)
		assert.Len(t, gs.GateBItems, 2)
	})

	t.Run("evaluate_gates_blocked", func(t *testing.T) {
		repoRoot := filepath.Join(testdataDir(), "repo_gates_eval", "valid_blocked")
		result, err := EvaluateGates(GatesEvaluateRequest{
			GateSetSource: CanonicalGateSourcePath,
			Slice:         "S1",
		}, repoRoot)
		require.NoError(t, err)
		assert.False(t, result.SliceReady)
		assert.Equal(t, GateStatusBlocked, result.GateA.Status)
	})

	t.Run("evaluate_gates_all_closed", func(t *testing.T) {
		repoRoot := filepath.Join(testdataDir(), "repo_gates_eval", "valid_all_closed")
		result, err := EvaluateGates(GatesEvaluateRequest{
			GateSetSource: CanonicalGateSourcePath,
			Slice:         "S1",
		}, repoRoot)
		require.NoError(t, err)
		assert.True(t, result.SliceReady)
	})

	t.Run("require_ready_blocked", func(t *testing.T) {
		repoRoot := filepath.Join(testdataDir(), "repo_gates_eval", "valid_blocked")
		_, err := RequireSliceReady(GatesEvaluateRequest{
			GateSetSource: CanonicalGateSourcePath,
			Slice:         "S1",
		}, repoRoot)
		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateBlocked, agencyerrors.GetCode(err))
	})

	t.Run("change_validate_reason_required", func(t *testing.T) {
		repoRoot := filepath.Join(testdataDir(), "repo_change_validate", "valid_synced")
		_, err := ValidateGateSetChange(GateSetChange{
			GateID:     "B",
			ChangeType: "add",
			IssuePath:  "docs/issues/new-issue.md",
			Reason:     "",
		}, repoRoot)
		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateChangeReasonRequired, agencyerrors.GetCode(err))
	})
}

// --- Closure Report ---

func TestBuildClosureReport_ClosedItemsOnlyAndCanonicalOrder(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Join(testdataDir(), "repo_gates_eval", "valid_blocked")
	source := NewMarkdownIssueSource(repoRoot)
	svc := NewService(source)

	result, err := svc.BuildClosureReport(ClosureReportRequest{Slice: "S1"}, repoRoot)
	require.NoError(t, err)
	assert.Equal(t, "S1", result.Slice)

	assert.NotNil(t, result.GateA)
	assert.NotNil(t, result.GateB)
	assert.Equal(t, "A", result.GateA.GateID)
	assert.Equal(t, "B", result.GateB.GateID)

	totalClosed := result.GateA.ClosedItems + result.GateB.ClosedItems
	totalEvidence := len(result.GateA.ClosedEvidence) + len(result.GateB.ClosedEvidence)
	assert.Equal(t, totalClosed, totalEvidence, "closed items count must match evidence entries")

	assert.Greater(t, len(result.GateA.BlockingItems)+len(result.GateB.BlockingItems), 0, "blocked repo should have blocking items")
}

func TestBuildClosureReport_StableSchema(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Join(testdataDir(), "repo_gates_eval", "valid_all_closed")
	source := NewMarkdownIssueSource(repoRoot)
	svc := NewService(source)

	result, err := svc.BuildClosureReport(ClosureReportRequest{Slice: "S1"}, repoRoot)
	require.NoError(t, err)

	assert.Equal(t, GateStatusReady, result.GateA.Status)
	assert.Equal(t, GateStatusReady, result.GateB.Status)
	assert.Empty(t, result.GateA.BlockingItems)
	assert.Empty(t, result.GateB.BlockingItems)

	for _, ev := range result.GateA.ClosedEvidence {
		assert.NotEmpty(t, ev.IssuePath)
		assert.NotNil(t, ev.ImplementedRefs)
		assert.NotNil(t, ev.TargetedTests)
		assert.NotNil(t, ev.SuiteTests)
	}
}

// --- Issue Source ---

func TestIssueSource_MarkdownAdapterContract(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Join(testdataDir(), "repo_valid")
	source := NewMarkdownIssueSource(repoRoot)

	ref, err := source.GetItemRef("docs/issues/shared-item.md")
	require.NoError(t, err)
	assert.Equal(t, "docs/issues/shared-item.md", ref.IssuePath)
	assert.NotEmpty(t, ref.Priority)
	assert.NotEmpty(t, ref.Type)
	assert.NotEmpty(t, ref.State)

	eval, err := source.Evaluate("docs/issues/shared-item.md")
	require.NoError(t, err)
	assert.Equal(t, ref.IssuePath, eval.IssuePath)
	assert.Equal(t, ref.State, eval.State)
}

// --- Freeze Readiness ---

func TestEvaluateFreezeReadiness_UnresolvedRowsBlockFreeze(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Join(testdataDir(), "repo_release", "freeze_blocked")
	source := NewMarkdownIssueSource(repoRoot)
	svc := NewService(source)

	result, err := svc.EvaluateFreezeReadiness(FreezeReadinessRequest{}, repoRoot)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateBlocked, agencyerrors.GetCode(err))
	assert.NotNil(t, result)
	assert.False(t, result.FreezeReady)
	assert.Equal(t, 1, result.UnresolvedCount)
	assert.NotEmpty(t, result.FirstQuestion)

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "false", ae.Details["freeze_ready"])
	assert.NotEmpty(t, ae.Details["unresolved_count"])
	assert.NotEmpty(t, ae.Details["spec_path"])
	assert.NotEmpty(t, ae.Details["first_question"])
}

func TestEvaluateFreezeReadiness_EmptyRowsAllowFreeze(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Join(testdataDir(), "repo_release", "freeze_ready")
	source := NewMarkdownIssueSource(repoRoot)
	svc := NewService(source)

	result, err := svc.EvaluateFreezeReadiness(FreezeReadinessRequest{}, repoRoot)
	require.NoError(t, err)
	assert.True(t, result.FreezeReady)
	assert.Equal(t, 0, result.UnresolvedCount)
}

func TestEvaluateFreezeReadiness_ParseFailureReturnsEGateSetInvalid(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Join(testdataDir(), "repo_release", "freeze_malformed")
	source := NewMarkdownIssueSource(repoRoot)
	svc := NewService(source)

	_, err := svc.EvaluateFreezeReadiness(FreezeReadinessRequest{}, repoRoot)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateSetInvalid, agencyerrors.GetCode(err))

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.NotEmpty(t, ae.Details["spec_path"])
}
