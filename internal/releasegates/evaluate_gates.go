package releasegates

import (
	"fmt"
	"os"
	"path/filepath"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// EvaluateGates performs deterministic aggregate evaluation for Gate A, Gate B,
// and Slice S1 readiness.
func EvaluateGates(req GatesEvaluateRequest, repoRoot string) (*GatesEvaluateResult, error) {
	if req.Slice != "S1" {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateSetInvalid,
			fmt.Sprintf("unsupported slice: %q (expected S1)", req.Slice),
			map[string]string{"slice": req.Slice},
		)
	}
	if req.GateSetSource != CanonicalGateSourcePath {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateSetInvalid,
			fmt.Sprintf("non-canonical gate_set_source: %q", req.GateSetSource),
			map[string]string{"gate_set_source": req.GateSetSource},
		)
	}

	gateSet, err := LoadGateSet(repoRoot)
	if err != nil {
		return nil, err
	}

	issueMap, err := LoadIssueMap(repoRoot)
	if err != nil {
		return nil, err
	}

	allItems := make([]string, 0, len(gateSet.GateAItems)+len(gateSet.GateBItems))
	allItems = append(allItems, gateSet.GateAItems...)
	allItems = append(allItems, gateSet.GateBItems...)

	evaluations := make(map[string]*GateItemEvaluation, len(allItems))
	for _, issuePath := range allItems {
		eval, evalErr := EvaluateGateItem(issuePath, repoRoot)
		if evalErr != nil {
			code := agencyerrors.GetCode(evalErr)
			return nil, agencyerrors.NewWithDetails(
				agencyerrors.EGateSetInvalid,
				fmt.Sprintf("gate item artifact failure: %s", issuePath),
				map[string]string{
					"issue_path":         issuePath,
					"item_error_code":    string(code),
					"item_error_message": evalErr.Error(),
				},
			)
		}
		evaluations[issuePath] = eval
	}

	if driftErr := DetectDrift(allItems, issueMap); driftErr != nil {
		return nil, driftErr
	}

	gateA := ComputeGateStatus("A", gateSet.GateAItems, evaluations)
	gateB := ComputeGateStatus("B", gateSet.GateBItems, evaluations)

	return &GatesEvaluateResult{
		Slice:      "S1",
		GateA:      gateA,
		GateB:      gateB,
		SliceReady: gateA.Status == GateStatusReady && gateB.Status == GateStatusReady,
	}, nil
}

// LoadGateSet reads and parses the canonical release-gates source.
func LoadGateSet(repoRoot string) (*GateSet, error) {
	fullPath := filepath.Join(repoRoot, CanonicalGateSourcePath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateSetInvalid,
			fmt.Sprintf("cannot read gate source: %s", CanonicalGateSourcePath),
			map[string]string{"path": CanonicalGateSourcePath},
		)
	}
	return ParseGateSet(string(data), CanonicalGateSourcePath, RepoFileExists(repoRoot))
}

// LoadIssueMap reads and parses the canonical issue-map source.
func LoadIssueMap(repoRoot string) (*IssueMapResult, error) {
	fullPath := filepath.Join(repoRoot, CanonicalIssueMapPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateSetInvalid,
			fmt.Sprintf("cannot read issue-map: %s", CanonicalIssueMapPath),
			map[string]string{"path": CanonicalIssueMapPath},
		)
	}
	return ParseIssueMap(string(data))
}

// DetectDrift checks that each canonical gate issue path appears exactly once
// in the issue-map.
func DetectDrift(canonicalItems []string, issueMap *IssueMapResult) error {
	for _, issuePath := range canonicalItems {
		count := issueMap.Counts[issuePath]
		if count == 1 {
			continue
		}
		driftKind := "missing"
		if count > 1 {
			driftKind = "duplicate"
		}
		return agencyerrors.NewWithDetails(
			agencyerrors.EGateSetDrift,
			fmt.Sprintf("gate-source/issue-map drift: %s", issuePath),
			map[string]string{
				"issue_path":      issuePath,
				"issue_map_count": fmt.Sprintf("%d", count),
				"drift_kind":      driftKind,
			},
		)
	}
	return nil
}

// ComputeGateStatus computes the aggregate status for a single gate.
func ComputeGateStatus(gateID string, items []string, evaluations map[string]*GateItemEvaluation) *GateStatus {
	status := &GateStatus{
		GateID:        gateID,
		TotalItems:    len(items),
		ClosedItems:   0,
		BlockingItems: []string{},
	}

	for _, issuePath := range items {
		eval := evaluations[issuePath]
		if eval.State == StateClosed && eval.BlockingCode == "" {
			status.ClosedItems++
		} else {
			status.BlockingItems = append(status.BlockingItems, issuePath)
		}
	}

	if status.ClosedItems == status.TotalItems {
		status.Status = GateStatusReady
	} else {
		status.Status = GateStatusBlocked
	}

	return status
}
