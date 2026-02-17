package s1gates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// canonicalRequest returns the standard valid evaluation request.
func canonicalRequest() GatesEvaluateRequest {
	return GatesEvaluateRequest{
		GateSetSource: CanonicalGateSourcePath,
		Slice:         "S1",
	}
}

func TestEvaluateGates_ReturnsAggregateStatusAndSliceReady(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "repo_gates_eval", "valid_blocked")
	result, err := EvaluateGates(canonicalRequest(), repoRoot)
	require.NoError(t, err)

	assert.Equal(t, "S1", result.Slice)

	// Gate A: 1 closed, 1 blocking.
	assert.Equal(t, "A", result.GateA.GateID)
	assert.Equal(t, 2, result.GateA.TotalItems)
	assert.Equal(t, 1, result.GateA.ClosedItems)
	assert.Equal(t, GateStatusBlocked, result.GateA.Status)
	assert.Equal(t, []string{"docs/issues/gate-a-2.md"}, result.GateA.BlockingItems)

	// Gate B: 1 closed, 1 blocking.
	assert.Equal(t, "B", result.GateB.GateID)
	assert.Equal(t, 2, result.GateB.TotalItems)
	assert.Equal(t, 1, result.GateB.ClosedItems)
	assert.Equal(t, GateStatusBlocked, result.GateB.Status)
	assert.Equal(t, []string{"docs/issues/gate-b-2.md"}, result.GateB.BlockingItems)

	assert.False(t, result.SliceReady)
}

func TestEvaluateGates_AllClosedSetsReadyAndNoBlockers(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "repo_gates_eval", "valid_all_closed")
	result, err := EvaluateGates(canonicalRequest(), repoRoot)
	require.NoError(t, err)

	assert.Equal(t, GateStatusReady, result.GateA.Status)
	assert.Equal(t, GateStatusReady, result.GateB.Status)
	assert.Empty(t, result.GateA.BlockingItems)
	assert.Empty(t, result.GateB.BlockingItems)
	assert.Equal(t, 2, result.GateA.ClosedItems)
	assert.Equal(t, 2, result.GateB.ClosedItems)
	assert.True(t, result.SliceReady)
}

func TestEvaluateGates_ClosedCountingRequiresClosedStateAndNoBlockingCode(t *testing.T) {
	t.Parallel()

	// Create fixture where one item is state=closed but missing closure evidence.
	repoRoot := t.TempDir()
	setupGatesRepo(t, repoRoot, []gateItemFixture{
		{path: "docs/issues/gate-a-1.md", priority: "p0", state: "closed", withEvidence: true},
	}, []gateItemFixture{
		{path: "docs/issues/gate-b-1.md", priority: "p1", state: "closed", withEvidence: false},
	})

	result, err := EvaluateGates(canonicalRequest(), repoRoot)
	require.NoError(t, err)

	// gate-b-1 is state=closed but evaluator blocks (no closure evidence).
	assert.Equal(t, GateStatusBlocked, result.GateB.Status)
	assert.Equal(t, 0, result.GateB.ClosedItems)
	assert.Equal(t, []string{"docs/issues/gate-b-1.md"}, result.GateB.BlockingItems)
}

func TestEvaluateGates_BlockingItemsPreserveCanonicalOrder(t *testing.T) {
	t.Parallel()

	// Create fixture with multiple blockers per gate to verify ordering.
	repoRoot := t.TempDir()
	setupGatesRepo(t, repoRoot, []gateItemFixture{
		{path: "docs/issues/a-first.md", priority: "p0", state: "open", withEvidence: false},
		{path: "docs/issues/a-second.md", priority: "p0", state: "in_progress", withEvidence: false},
		{path: "docs/issues/a-third.md", priority: "p0", state: "closed", withEvidence: true},
	}, []gateItemFixture{
		{path: "docs/issues/b-first.md", priority: "p1", state: "in_progress", withEvidence: false},
		{path: "docs/issues/b-second.md", priority: "p1", state: "open", withEvidence: false},
	})

	result, err := EvaluateGates(canonicalRequest(), repoRoot)
	require.NoError(t, err)

	// Gate A blockers in canonical source order.
	assert.Equal(t, []string{
		"docs/issues/a-first.md",
		"docs/issues/a-second.md",
	}, result.GateA.BlockingItems)

	// Gate B blockers in canonical source order.
	assert.Equal(t, []string{
		"docs/issues/b-first.md",
		"docs/issues/b-second.md",
	}, result.GateB.BlockingItems)
}

func TestEvaluateGates_ReopenedIssueReblocksGate(t *testing.T) {
	t.Parallel()

	// Start with all items closed (writable copy for mutation).
	repoRoot := copyFixtureRepo(t, filepath.Join("testdata", "repo_gates_eval", "valid_all_closed"))

	// Initial: should be ready.
	result1, err := EvaluateGates(canonicalRequest(), repoRoot)
	require.NoError(t, err)
	assert.True(t, result1.SliceReady)
	assert.Equal(t, GateStatusReady, result1.GateB.Status)

	// Reopen one Gate B item.
	_, err = TransitionGateItem(GateTransition{
		IssuePath:    "docs/issues/gate-b-1.md",
		FromState:    StateClosed,
		ToState:      StateInProgress,
		ActorRole:    RoleMaintainer,
		Reason:       "regression found",
		EvidenceRefs: []string{"regression:evidence-1"},
	}, repoRoot)
	require.NoError(t, err)

	// Post-reopen: Gate B should be blocked.
	result2, err := EvaluateGates(canonicalRequest(), repoRoot)
	require.NoError(t, err)
	assert.False(t, result2.SliceReady)
	assert.Equal(t, GateStatusBlocked, result2.GateB.Status)
	assert.Contains(t, result2.GateB.BlockingItems, "docs/issues/gate-b-1.md")
}

func TestEvaluateGates_DriftMissingGateIssueReturnsEGateSetDrift(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "repo_gates_eval", "drift_missing")
	result, err := EvaluateGates(canonicalRequest(), repoRoot)
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateSetDrift, agencyerrors.GetCode(err))

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "docs/issues/gate-b-2.md", ae.Details["issue_path"])
	assert.Equal(t, "0", ae.Details["issue_map_count"])
	assert.Equal(t, "missing", ae.Details["drift_kind"])
}

func TestEvaluateGates_DriftDuplicateGateIssueReturnsEGateSetDrift(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "repo_gates_eval", "drift_duplicate")
	result, err := EvaluateGates(canonicalRequest(), repoRoot)
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateSetDrift, agencyerrors.GetCode(err))

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "docs/issues/gate-a-1.md", ae.Details["issue_path"])
	assert.Equal(t, "2", ae.Details["issue_map_count"])
	assert.Equal(t, "duplicate", ae.Details["drift_kind"])
}

func TestEvaluateGates_DriftDetailsUseFirstCanonicalMismatch(t *testing.T) {
	t.Parallel()

	// Create fixture with multiple drift mismatches; verify first canonical is reported.
	repoRoot := t.TempDir()
	setupGatesRepo(t, repoRoot, []gateItemFixture{
		{path: "docs/issues/a-1.md", priority: "p0", state: "open", withEvidence: false},
		{path: "docs/issues/a-2.md", priority: "p0", state: "open", withEvidence: false},
	}, []gateItemFixture{
		{path: "docs/issues/b-1.md", priority: "p1", state: "open", withEvidence: false},
	})

	// Overwrite issue-map to be missing a-2 and b-1 (two mismatches).
	issueMapContent := "# Issue Map\n\n## S1\n\n1. `docs/issues/a-1.md`\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(repoRoot, CanonicalIssueMapPath),
		[]byte(issueMapContent), 0o644,
	))

	result, err := EvaluateGates(canonicalRequest(), repoRoot)
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateSetDrift, agencyerrors.GetCode(err))

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	// First canonical mismatch is a-2 (Gate A item, canonical order).
	assert.Equal(t, "docs/issues/a-2.md", ae.Details["issue_path"])
	assert.Equal(t, "missing", ae.Details["drift_kind"])
}

func TestEvaluateGates_ItemArtifactFailureMapsToEGateSetInvalid(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "repo_gates_eval", "item_malformed")
	result, err := EvaluateGates(canonicalRequest(), repoRoot)
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateSetInvalid, agencyerrors.GetCode(err))

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "docs/issues/gate-a-2.md", ae.Details["issue_path"])
	assert.Equal(t, string(agencyerrors.EGateItemInvalid), ae.Details["item_error_code"])
}

func TestEvaluateGates_ItemArtifactFailureDetailsIncludeItemCause(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "repo_gates_eval", "item_malformed")
	_, err := EvaluateGates(canonicalRequest(), repoRoot)
	require.Error(t, err)

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)

	// All three required detail keys must be present.
	assert.NotEmpty(t, ae.Details["issue_path"])
	assert.NotEmpty(t, ae.Details["item_error_code"])
	assert.NotEmpty(t, ae.Details["item_error_message"])
}

func TestEvaluateGates_InvalidRequestReturnsEGateSetInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  GatesEvaluateRequest
	}{
		{
			"wrong slice",
			GatesEvaluateRequest{GateSetSource: CanonicalGateSourcePath, Slice: "S2"},
		},
		{
			"non-canonical source",
			GatesEvaluateRequest{GateSetSource: "other/path.md", Slice: "S1"},
		},
		{
			"empty slice",
			GatesEvaluateRequest{GateSetSource: CanonicalGateSourcePath, Slice: ""},
		},
	}

	repoRoot := filepath.Join("testdata", "repo_gates_eval", "valid_blocked")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := EvaluateGates(tt.req, repoRoot)
			assert.Nil(t, result)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateSetInvalid, agencyerrors.GetCode(err))
		})
	}
}

func TestEvaluateGates_ErrorPrecedenceIsDeterministic(t *testing.T) {
	t.Parallel()

	t.Run("gate_source_invalid_beats_all", func(t *testing.T) {
		t.Parallel()

		// Repo with broken release-gates.md.
		repoRoot := t.TempDir()
		gatesDir := filepath.Join(repoRoot, "docs", "v2.1")
		require.NoError(t, os.MkdirAll(gatesDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(gatesDir, "release-gates.md"), []byte("# empty\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(gatesDir, "issue-map.md"), []byte("# empty\n"), 0o644))

		_, err := EvaluateGates(canonicalRequest(), repoRoot)
		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateSetInvalid, agencyerrors.GetCode(err))
	})

	t.Run("issue_map_invalid_beats_item_and_drift", func(t *testing.T) {
		t.Parallel()

		// Valid gate source but broken issue-map.
		repoRoot := t.TempDir()
		setupGatesRepo(t, repoRoot, []gateItemFixture{
			{path: "docs/issues/a-1.md", priority: "p0", state: "open", withEvidence: false},
		}, []gateItemFixture{
			{path: "docs/issues/b-1.md", priority: "p1", state: "open", withEvidence: false},
		})
		// Overwrite issue-map with empty content.
		require.NoError(t, os.WriteFile(
			filepath.Join(repoRoot, CanonicalIssueMapPath),
			[]byte("# empty\n"), 0o644,
		))

		_, err := EvaluateGates(canonicalRequest(), repoRoot)
		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateSetInvalid, agencyerrors.GetCode(err))
	})

	t.Run("item_artifact_invalid_beats_drift", func(t *testing.T) {
		t.Parallel()

		// Item artifact failure + drift both present; artifact should win.
		repoRoot := t.TempDir()
		issuesDir := filepath.Join(repoRoot, "docs", "issues")
		gatesDir := filepath.Join(repoRoot, "docs", "v2.1")
		require.NoError(t, os.MkdirAll(issuesDir, 0o755))
		require.NoError(t, os.MkdirAll(gatesDir, 0o755))

		// gate-a-1 is malformed (missing acceptance).
		require.NoError(t, os.WriteFile(filepath.Join(issuesDir, "gate-a-1.md"),
			[]byte("# [p0][events][tech-debt] malformed\n\nlabels: `p0`, `type:tech-debt`\n\n## summary\nno acceptance\n"), 0o644))
		// gate-b-1 is valid.
		writeValidIssueStub(t, issuesDir, "gate-b-1.md", "p1", "open", false)

		// release-gates references both.
		require.NoError(t, os.WriteFile(filepath.Join(gatesDir, "release-gates.md"),
			[]byte("# Gates\n\n## Gate A\n\n1. `docs/issues/gate-a-1.md`\n\n## Gate B\n\n1. `docs/issues/gate-b-1.md`\n"), 0o644))
		// issue-map is also missing gate-b-1 (drift), but artifact error should take precedence.
		require.NoError(t, os.WriteFile(filepath.Join(gatesDir, "issue-map.md"),
			[]byte("## S1\n\n1. `docs/issues/gate-a-1.md`\n"), 0o644))

		_, err := EvaluateGates(canonicalRequest(), repoRoot)
		require.Error(t, err)
		// Artifact failure (E_GATE_SET_INVALID) beats drift (E_GATE_SET_DRIFT).
		assert.Equal(t, agencyerrors.EGateSetInvalid, agencyerrors.GetCode(err))
	})
}

// --- test helpers ---

type gateItemFixture struct {
	path         string
	priority     string
	state        string
	withEvidence bool
}

// setupGatesRepo creates a minimal fixture repo with release-gates.md, issue-map.md,
// and issue stub files for the given gate items.
func setupGatesRepo(t *testing.T, repoRoot string, gateA, gateB []gateItemFixture) {
	t.Helper()

	issuesDir := filepath.Join(repoRoot, "docs", "issues")
	gatesDir := filepath.Join(repoRoot, "docs", "v2.1")
	require.NoError(t, os.MkdirAll(issuesDir, 0o755))
	require.NoError(t, os.MkdirAll(gatesDir, 0o755))

	// Build release-gates.md.
	var gates string
	gates += "# Release Gates\n\n## Gate A: P0 safety closure\n\n"
	for i, item := range gateA {
		gates += fmtNumberedItem(i+1, item.path)
	}
	gates += "\n## Gate B: parity-critical P1 closure\n\n"
	for i, item := range gateB {
		gates += fmtNumberedItem(i+1, item.path)
	}
	require.NoError(t, os.WriteFile(filepath.Join(gatesDir, "release-gates.md"), []byte(gates), 0o644))

	// Build issue-map.md.
	var issueMap string
	issueMap += "# Issue Map\n\n## S1 Platform Hardening Gates\n\n"
	idx := 1
	for _, item := range gateA {
		issueMap += fmtNumberedItem(idx, item.path)
		idx++
	}
	for _, item := range gateB {
		issueMap += fmtNumberedItem(idx, item.path)
		idx++
	}
	require.NoError(t, os.WriteFile(filepath.Join(gatesDir, "issue-map.md"), []byte(issueMap), 0o644))

	// Write issue stubs.
	for _, item := range gateA {
		writeValidIssueStub(t, issuesDir, filepath.Base(item.path), item.priority, item.state, item.withEvidence)
	}
	for _, item := range gateB {
		writeValidIssueStub(t, issuesDir, filepath.Base(item.path), item.priority, item.state, item.withEvidence)
	}
}

func fmtNumberedItem(n int, path string) string {
	return fmtNumberedItemStr(n, path) + "\n"
}

func fmtNumberedItemStr(n int, path string) string {
	return fmtNum(n) + ". `" + path + "`"
}

func fmtNum(n int) string {
	s := ""
	if n < 10 {
		s = ""
	}
	return s + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// writeValidIssueStub writes a minimal valid issue stub to the given directory.
func writeValidIssueStub(t *testing.T, dir, filename, priority, state string, withEvidence bool) {
	t.Helper()

	itemType := "tech-debt"
	area := "core"

	content := "# [" + priority + "][" + area + "][" + itemType + "] " + filename + "\n\n"
	content += "labels: `" + priority + "`, `type:" + itemType + "`\n"
	content += "state: " + state + "\n\n"
	content += "## summary\ntest issue\n\n"
	content += "## acceptance criteria\n- [x] done\n"

	if withEvidence {
		issuePath := "docs/issues/" + filename
		content += "\n## closure evidence\n\n```json\n"
		content += `{
  "implemented_refs": ["pr:1"],
  "targeted_test_refs": [
    {"issue_path": "` + issuePath + `", "command": "go test ./internal/...", "scope": "targeted", "result": "pass", "artifact_ref": "ci:1", "recorded_at": "2026-01-01T00:00:00Z"}
  ],
  "suite_test_refs": [
    {"issue_path": "` + issuePath + `", "command": "go test ./...", "scope": "suite", "result": "pass", "artifact_ref": "ci:2", "recorded_at": "2026-01-01T00:00:00Z"}
  ]
}`
		content += "\n```\n"
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644))
}

// copyFixtureRepo copies a fixture repo to a temp dir for writable tests.
func copyFixtureRepo(t *testing.T, src string) string {
	t.Helper()

	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, info.Mode())
	})
	require.NoError(t, err)
	return dst
}
