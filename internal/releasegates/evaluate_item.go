package releasegates

import (
	"fmt"
	"os"
	"path/filepath"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

var blockingCodePrecedence = []agencyerrors.Code{
	agencyerrors.EGateItemAcceptanceIncomplete,
	agencyerrors.EGateItemClosureBlockMissing,
	agencyerrors.EGateItemEvidenceMissing,
	agencyerrors.EGateItemTestsIncomplete,
}

var missingRequirementToCode = map[string]agencyerrors.Code{
	MissingAcceptanceIncomplete: agencyerrors.EGateItemAcceptanceIncomplete,
	MissingClosureEvidenceBlock: agencyerrors.EGateItemClosureBlockMissing,
	MissingEvidenceRefs:         agencyerrors.EGateItemEvidenceMissing,
	MissingTargetedTestEvidence: agencyerrors.EGateItemTestsIncomplete,
	MissingSuiteTestEvidence:    agencyerrors.EGateItemTestsIncomplete,
}

// EvaluateGateItem evaluates a single gate item by reading and parsing the
// issue stub at issuePath (relative to repoRoot).
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

	eval.TestsComplete = computeTestsComplete(ce)

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

	eval.BlockingCode = computeBlockingCode(eval.MissingRequirements)

	return eval, nil
}

func computeTestsComplete(ce *ClosureEvidence) bool {
	if ce == nil {
		return false
	}
	return hasTargetedEvidence(ce) && hasPassingSuiteEvidence(ce)
}

func hasTargetedEvidence(ce *ClosureEvidence) bool {
	if ce == nil {
		return false
	}
	return len(ce.TargetedTestRefs) > 0
}

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

func computeBlockingCode(missing []string) agencyerrors.Code {
	if len(missing) == 0 {
		return ""
	}

	presentCodes := make(map[agencyerrors.Code]bool)
	for _, m := range missing {
		if code, ok := missingRequirementToCode[m]; ok {
			presentCodes[code] = true
		}
	}

	for _, code := range blockingCodePrecedence {
		if presentCodes[code] {
			return code
		}
	}

	return ""
}
