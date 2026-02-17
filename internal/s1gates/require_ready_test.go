package s1gates

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

func TestRequireSliceReady_ReturnsEGateBlockedWhenSliceNotReady(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "repo_gates_eval", "valid_blocked")
	result, err := RequireSliceReady(canonicalRequest(), repoRoot)
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateBlocked, agencyerrors.GetCode(err))

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, GateStatusBlocked, ae.Details["gate_a_status"])
	assert.Equal(t, GateStatusBlocked, ae.Details["gate_b_status"])
}

func TestRequireSliceReady_ReturnsResultWhenSliceReady(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "repo_gates_eval", "valid_all_closed")
	result, err := RequireSliceReady(canonicalRequest(), repoRoot)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.SliceReady)
	assert.Equal(t, GateStatusReady, result.GateA.Status)
	assert.Equal(t, GateStatusReady, result.GateB.Status)
}

func TestRequireSliceReady_BlockerDetailsAreDeterministic(t *testing.T) {
	t.Parallel()

	// Create fixture with multiple blockers per gate for deterministic detail check.
	repoRoot := t.TempDir()
	setupGatesRepo(t, repoRoot, []gateItemFixture{
		{path: "docs/issues/a-1.md", priority: "p0", state: "open", withEvidence: false},
		{path: "docs/issues/a-2.md", priority: "p0", state: "open", withEvidence: false},
	}, []gateItemFixture{
		{path: "docs/issues/b-1.md", priority: "p1", state: "in_progress", withEvidence: false},
		{path: "docs/issues/b-2.md", priority: "p1", state: "open", withEvidence: false},
		{path: "docs/issues/b-3.md", priority: "p1", state: "closed", withEvidence: true},
	})

	result, err := RequireSliceReady(canonicalRequest(), repoRoot)
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateBlocked, agencyerrors.GetCode(err))

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)

	// Fixed detail key set.
	assert.Equal(t, GateStatusBlocked, ae.Details["gate_a_status"])
	assert.Equal(t, GateStatusBlocked, ae.Details["gate_b_status"])
	assert.Equal(t, "0", ae.Details["gate_a_closed_items"])
	assert.Equal(t, "2", ae.Details["gate_a_total_items"])
	assert.Equal(t, "1", ae.Details["gate_b_closed_items"])
	assert.Equal(t, "3", ae.Details["gate_b_total_items"])

	// Canonical-order |‑joined blocker paths.
	assert.Equal(t, "docs/issues/a-1.md|docs/issues/a-2.md", ae.Details["gate_a_blocking_items"])
	assert.Equal(t, "docs/issues/b-1.md|docs/issues/b-2.md", ae.Details["gate_b_blocking_items"])
}
