package s1gates

import (
	"fmt"
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// RequireSliceReady evaluates gates and returns E_GATE_BLOCKED if the slice
// is not ready. If the slice is ready, returns the aggregate result unchanged.
// Non-blocked errors from EvaluateGates are propagated unchanged.
func RequireSliceReady(req GatesEvaluateRequest, repoRoot string) (*GatesEvaluateResult, error) {
	result, err := EvaluateGates(req, repoRoot)
	if err != nil {
		return nil, err
	}

	if !result.SliceReady {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateBlocked,
			"slice S1 is not ready: gates are blocked",
			map[string]string{
				"gate_a_status":         result.GateA.Status,
				"gate_b_status":         result.GateB.Status,
				"gate_a_closed_items":   fmt.Sprintf("%d", result.GateA.ClosedItems),
				"gate_a_total_items":    fmt.Sprintf("%d", result.GateA.TotalItems),
				"gate_b_closed_items":   fmt.Sprintf("%d", result.GateB.ClosedItems),
				"gate_b_total_items":    fmt.Sprintf("%d", result.GateB.TotalItems),
				"gate_a_blocking_items": strings.Join(result.GateA.BlockingItems, "|"),
				"gate_b_blocking_items": strings.Join(result.GateB.BlockingItems, "|"),
			},
		)
	}

	return result, nil
}
