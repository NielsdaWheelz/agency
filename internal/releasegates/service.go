package releasegates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// Service orchestrates release-gate evaluation and reporting.
type Service struct {
	source *Source
}

// NewService creates a release-gates service with the given source.
func NewService(source *Source) *Service {
	return &Service{source: source}
}

// EvaluateReleaseReadiness returns the aggregate readiness result for a slice.
func (s *Service) EvaluateReleaseReadiness(req ReleaseReadinessRequest, repoRoot string) (*ReleaseReadinessResult, error) {
	gatesReq := GatesEvaluateRequest{
		GateSetSource: CanonicalGateSourcePath,
		Slice:         req.Slice,
	}

	result, err := RequireSliceReady(gatesReq, repoRoot)
	if err != nil {
		code := agencyerrors.GetCode(err)
		if code == agencyerrors.EGateBlocked {
			ae, _ := agencyerrors.AsAgencyError(err)
			return rebuildBlockedReadinessResult(req.Slice, ae), agencyerrors.NewWithDetails(
				agencyerrors.EGateBlocked,
				ae.Msg,
				ae.Details,
			)
		}
		return nil, err
	}

	return &ReleaseReadinessResult{
		Slice:      result.Slice,
		SliceReady: result.SliceReady,
		GateA:      result.GateA,
		GateB:      result.GateB,
	}, nil
}

// BuildClosureReport generates closure evidence for the closed items in each gate.
func (s *Service) BuildClosureReport(req ClosureReportRequest, repoRoot string) (*ClosureReportResult, error) {
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
		eval, evalErr := s.source.Evaluate(issuePath)
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

	gateASnapshot := s.buildGateSnapshot("A", gateSet.GateAItems, evaluations)
	gateBSnapshot := s.buildGateSnapshot("B", gateSet.GateBItems, evaluations)

	return &ClosureReportResult{
		Slice: req.Slice,
		GateA: gateASnapshot,
		GateB: gateBSnapshot,
	}, nil
}

func (s *Service) buildGateSnapshot(gateID string, items []string, evaluations map[string]*GateItemEvaluation) *GateClosureSnapshot {
	snap := &GateClosureSnapshot{
		GateID:         gateID,
		TotalItems:     len(items),
		BlockingItems:  []string{},
		ClosedEvidence: []ClosedItemEvidence{},
	}

	for _, issuePath := range items {
		eval := evaluations[issuePath]
		if eval.State == StateClosed && eval.BlockingCode == "" {
			snap.ClosedItems++
			ce, _ := s.source.GetClosureEvidence(issuePath)
			evidence := ClosedItemEvidence{
				IssuePath:       issuePath,
				ImplementedRefs: []string{},
				TargetedTests:   []TestEvidence{},
				SuiteTests:      []TestEvidence{},
			}
			if ce != nil {
				evidence.ImplementedRefs = ce.ImplementedRefs
				evidence.TargetedTests = ce.TargetedTestRefs
				evidence.SuiteTests = ce.SuiteTestRefs
			}
			if evidence.ImplementedRefs == nil {
				evidence.ImplementedRefs = []string{}
			}
			if evidence.TargetedTests == nil {
				evidence.TargetedTests = []TestEvidence{}
			}
			if evidence.SuiteTests == nil {
				evidence.SuiteTests = []TestEvidence{}
			}
			snap.ClosedEvidence = append(snap.ClosedEvidence, evidence)
		} else {
			snap.BlockingItems = append(snap.BlockingItems, issuePath)
		}
	}

	if snap.ClosedItems == snap.TotalItems {
		snap.Status = GateStatusReady
	} else {
		snap.Status = GateStatusBlocked
	}

	return snap
}

// section9TableRowRe matches a non-header, non-separator data row in a markdown table.
// Rows look like: | question text | default | owner | due |
var section9TableRowRe = regexp.MustCompile(`^\s*\|(.+)\|`)
var section9SepRowRe = regexp.MustCompile(`^\s*\|[\s\-|]+\|`)

// EvaluateFreezeReadiness parses Section 9 of the S1 spec and determines freeze eligibility.
func (s *Service) EvaluateFreezeReadiness(req FreezeReadinessRequest, repoRoot string) (*FreezeReadinessResult, error) {
	specPath := req.SpecPath
	if specPath == "" {
		specPath = CanonicalS1SpecPath
	}

	fullPath := filepath.Join(repoRoot, specPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateSetInvalid,
			fmt.Sprintf("cannot read spec source: %s", specPath),
			map[string]string{"spec_path": specPath},
		)
	}

	unresolvedRows, firstQuestion := parseSection9UnresolvedRows(string(data))

	if unresolvedRows > 0 {
		return &FreezeReadinessResult{
				FreezeReady:     false,
				UnresolvedCount: unresolvedRows,
				SpecPath:        specPath,
				FirstQuestion:   firstQuestion,
			}, agencyerrors.NewWithDetails(
				agencyerrors.EGateBlocked,
				"freeze blocked: unresolved defaults in Section 9",
				map[string]string{
					"freeze_ready":     "false",
					"unresolved_count": fmt.Sprintf("%d", unresolvedRows),
					"spec_path":        specPath,
					"first_question":   firstQuestion,
				},
			)
	}

	return &FreezeReadinessResult{
		FreezeReady:     true,
		UnresolvedCount: 0,
		SpecPath:        specPath,
	}, nil
}

// parseSection9UnresolvedRows finds Section 9, then counts non-empty data rows
// in the first table. "none" or "n/a" in all cells is not counted as unresolved.
func parseSection9UnresolvedRows(content string) (int, string) {
	lines := strings.Split(content, "\n")

	inSection9 := false
	foundTable := false
	headerRowSeen := false
	count := 0
	firstQuestion := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inSection9 {
			if strings.HasPrefix(trimmed, "## 9.") || strings.HasPrefix(trimmed, "## 9 ") {
				inSection9 = true
			}
			continue
		}

		if strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "## 9") {
			break
		}

		if !foundTable {
			if section9TableRowRe.MatchString(trimmed) {
				foundTable = true
				headerRowSeen = false
			} else {
				continue
			}
		}

		if !section9TableRowRe.MatchString(trimmed) {
			if foundTable && headerRowSeen {
				break
			}
			continue
		}

		if section9SepRowRe.MatchString(trimmed) {
			headerRowSeen = true
			continue
		}

		if !headerRowSeen {
			headerRowSeen = true
			continue
		}

		cells := strings.Split(trimmed, "|")
		isPlaceholder := true
		questionCell := ""
		for i, cell := range cells {
			c := strings.TrimSpace(cell)
			if c == "" {
				continue
			}
			if i == 1 {
				questionCell = c
			}
			lower := strings.ToLower(c)
			if lower != "none" && lower != "n/a" && lower != "" {
				isPlaceholder = false
			}
		}
		if isPlaceholder {
			continue
		}

		count++
		if firstQuestion == "" {
			firstQuestion = questionCell
		}
	}

	return count, firstQuestion
}

type blockedGateStatus struct {
	TotalItems    int
	ClosedItems   int
	BlockingItems []string
}

func rebuildBlockedReadinessResult(slice string, ae *agencyerrors.AgencyError) *ReleaseReadinessResult {
	gateA := rebuildBlockedGateStatus(ae.Details, "a")
	gateB := rebuildBlockedGateStatus(ae.Details, "b")

	return &ReleaseReadinessResult{
		Slice:      slice,
		SliceReady: false,
		GateA: &GateStatus{
			GateID:        "A",
			Status:        ae.Details["gate_a_status"],
			TotalItems:    gateA.TotalItems,
			ClosedItems:   gateA.ClosedItems,
			BlockingItems: gateA.BlockingItems,
		},
		GateB: &GateStatus{
			GateID:        "B",
			Status:        ae.Details["gate_b_status"],
			TotalItems:    gateB.TotalItems,
			ClosedItems:   gateB.ClosedItems,
			BlockingItems: gateB.BlockingItems,
		},
	}
}

func rebuildBlockedGateStatus(details map[string]string, prefix string) blockedGateStatus {
	var result blockedGateStatus
	_, _ = fmt.Sscanf(details["gate_"+prefix+"_total_items"], "%d", &result.TotalItems)
	_, _ = fmt.Sscanf(details["gate_"+prefix+"_closed_items"], "%d", &result.ClosedItems)
	blocking := details["gate_"+prefix+"_blocking_items"]
	if blocking != "" {
		result.BlockingItems = strings.Split(blocking, "|")
	} else {
		result.BlockingItems = []string{}
	}
	return result
}
