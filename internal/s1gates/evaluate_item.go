package s1gates

import (
	"fmt"
	"os"
	"path/filepath"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// blockingCodePrecedence defines the priority order for blocking codes.
// Lower index = higher priority.
var blockingCodePrecedence = []agencyerrors.Code{
	agencyerrors.EGateItemAcceptanceIncomplete,
	agencyerrors.EGateItemClosureBlockMissing,
	agencyerrors.EGateItemEvidenceMissing,
	agencyerrors.EGateItemTestsIncomplete,
}

// missingRequirementToCode maps missing requirement keys to their error codes.
var missingRequirementToCode = map[string]agencyerrors.Code{
	MissingAcceptanceIncomplete: agencyerrors.EGateItemAcceptanceIncomplete,
	MissingClosureEvidenceBlock: agencyerrors.EGateItemClosureBlockMissing,
	MissingEvidenceRefs:         agencyerrors.EGateItemEvidenceMissing,
	MissingTargetedTestEvidence: agencyerrors.EGateItemTestsIncomplete,
	MissingSuiteTestEvidence:    agencyerrors.EGateItemTestsIncomplete,
}

// EvaluateGateItem evaluates a single gate item by reading and parsing the
// issue stub at issuePath (relative to repoRoot). It returns a normalized
// GateItemEvaluation with missing requirements and blocking code.
//
// Returns E_GATE_ITEM_NOT_FOUND if the issue file does not exist.
// Returns E_GATE_ITEM_INVALID if the issue cannot be parsed.
func EvaluateGateItem(issuePath string, repoRoot string) (*GateItemEvaluation, error) {
	fullPath := filepath.Join(repoRoot, issuePath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, agencyerrors.NewWithDetails(
				agencyerrors.EGateItemNotFound,
				fmt.Sprintf("issue path does not exist: %s", issuePath),
				map[string]string{"issue_path": issuePath},
			)
		}
		return nil, agencyerrors.Wrap(agencyerrors.EGateItemNotFound,
			fmt.Sprintf("cannot read issue: %s", issuePath), err)
	}

	ref, ce, err := ParseIssue(string(data), issuePath)
	if err != nil {
		return nil, err
	}

	eval := &GateItemEvaluation{
		IssuePath:              issuePath,
		State:                  ref.State,
		RequiresGHE2E:          ref.RequiresGHE2E,
		AcceptanceComplete:     ref.AcceptanceComplete,
		ClosureEvidencePresent: ce != nil,
		EvidenceRefs:           ref.EvidenceRefs,
		MissingRequirements:    []string{},
	}

	// Compute tests_complete.
	eval.TestsComplete = computeTestsComplete(ce)

	// Compute missing requirements in deterministic order.
	if !eval.AcceptanceComplete {
		eval.MissingRequirements = append(eval.MissingRequirements, MissingAcceptanceIncomplete)
	}
	if !eval.ClosureEvidencePresent {
		eval.MissingRequirements = append(eval.MissingRequirements, MissingClosureEvidenceBlock)
	}
	if ref.State == StateClosed && len(eval.EvidenceRefs) == 0 {
		eval.MissingRequirements = append(eval.MissingRequirements, MissingEvidenceRefs)
	}
	if !hasTargetedEvidence(ce) {
		eval.MissingRequirements = append(eval.MissingRequirements, MissingTargetedTestEvidence)
	}
	if !hasPassingSuiteEvidence(ce) {
		eval.MissingRequirements = append(eval.MissingRequirements, MissingSuiteTestEvidence)
	}

	// Compute blocking code from highest-priority missing requirement.
	eval.BlockingCode = computeBlockingCode(eval.MissingRequirements)

	return eval, nil
}

// computeTestsComplete returns true only when closure evidence contains at
// least one targeted evidence row AND at least one passing suite evidence row
// with an allowed suite command.
func computeTestsComplete(ce *ClosureEvidence) bool {
	if ce == nil {
		return false
	}
	return hasTargetedEvidence(ce) && hasPassingSuiteEvidence(ce)
}

// hasTargetedEvidence checks if closure evidence has at least one targeted test row.
func hasTargetedEvidence(ce *ClosureEvidence) bool {
	if ce == nil {
		return false
	}
	return len(ce.TargetedTestRefs) > 0
}

// hasPassingSuiteEvidence checks if closure evidence has at least one passing
// suite row using an allowed suite command.
func hasPassingSuiteEvidence(ce *ClosureEvidence) bool {
	if ce == nil {
		return false
	}
	for _, te := range ce.SuiteTestRefs {
		if te.Result == ResultPass && AllowedSuiteCommands[te.Command] {
			return true
		}
	}
	return false
}

// computeBlockingCode selects the highest-priority blocking code from missing requirements.
func computeBlockingCode(missing []string) agencyerrors.Code {
	if len(missing) == 0 {
		return ""
	}

	// Build set of codes present in missing requirements.
	presentCodes := make(map[agencyerrors.Code]bool)
	for _, m := range missing {
		if code, ok := missingRequirementToCode[m]; ok {
			presentCodes[code] = true
		}
	}

	// Return highest priority code.
	for _, code := range blockingCodePrecedence {
		if presentCodes[code] {
			return code
		}
	}

	return ""
}
