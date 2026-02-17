package s1gates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// legalTransitions defines the allowed gate-item state transitions.
var legalTransitions = map[[2]string]bool{
	{StateOpen, StateInProgress}:                 true,
	{StateInProgress, StateReadyForVerification}: true,
	{StateReadyForVerification, StateClosed}:     true,
	{StateClosed, StateInProgress}:               true,
}

// TransitionGateItem validates and applies a gate-item state transition.
//
// Validation precedence (non-closure):
//  1. Issue artifact parse/load failure (E_GATE_ITEM_NOT_FOUND or E_GATE_ITEM_INVALID)
//  2. from_state mismatch with current parsed state (E_GATE_TRANSITION_INVALID)
//  3. Illegal lifecycle edge including self-transition (E_GATE_TRANSITION_INVALID)
//  4. Malformed/unknown actor_role (E_GATE_TRANSITION_INVALID)
//  5. Non-member gate issue path (E_GATE_TRANSITION_INVALID)
//
// Closure guard precedence (ready_for_verification -> closed):
//  1. PR-01 evaluator blocking_code
//  2. Request/parsed evidence-ref non-empty (E_GATE_ITEM_EVIDENCE_MISSING)
//  3. Gate A/B role policy (E_GATE_APPROVAL_REQUIRED)
//  4. GH e2e evidence (E_GATE_E2E_REQUIRED)
//  5. Design enforcement evidence (E_GATE_ITEM_TESTS_INCOMPLETE)
//
// Reopen guard (closed -> in_progress):
//   - Non-empty reason and evidence_refs (E_GATE_REOPEN_REASON_REQUIRED)
func TransitionGateItem(req GateTransition, repoRoot string) (*GateTransitionResult, error) {
	// --- Precedence 1: parse/load issue artifact ---
	fullPath := filepath.Join(repoRoot, req.IssuePath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, agencyerrors.NewWithDetails(
				agencyerrors.EGateItemNotFound,
				fmt.Sprintf("issue path does not exist: %s", req.IssuePath),
				map[string]string{"issue_path": req.IssuePath},
			)
		}
		return nil, agencyerrors.Wrap(agencyerrors.EGateItemNotFound,
			fmt.Sprintf("cannot read issue: %s", req.IssuePath), err)
	}

	ref, ce, err := ParseIssue(string(data), req.IssuePath)
	if err != nil {
		return nil, err
	}

	// --- Precedence 2: from_state mismatch ---
	if req.FromState != ref.State {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateTransitionInvalid,
			fmt.Sprintf("from_state %q does not match current state %q", req.FromState, ref.State),
			map[string]string{"issue_path": req.IssuePath, "current_state": ref.State},
		)
	}

	// --- Precedence 3: illegal lifecycle edge (including self-transition) ---
	edge := [2]string{req.FromState, req.ToState}
	if !legalTransitions[edge] {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateTransitionInvalid,
			fmt.Sprintf("illegal transition: %s -> %s", req.FromState, req.ToState),
			map[string]string{"issue_path": req.IssuePath},
		)
	}

	// --- Precedence 4: malformed/unknown actor_role ---
	if !ValidActorRoles[req.ActorRole] {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateTransitionInvalid,
			fmt.Sprintf("invalid actor_role: %s", req.ActorRole),
			map[string]string{"issue_path": req.IssuePath},
		)
	}

	// --- Precedence 5: non-member gate issue path ---
	gateID, err := resolveGateMembership(req.IssuePath, repoRoot)
	if err != nil {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateTransitionInvalid,
			fmt.Sprintf("cannot resolve gate membership: %v", err),
			map[string]string{"issue_path": req.IssuePath},
		)
	}
	if gateID == "" {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateTransitionInvalid,
			fmt.Sprintf("issue path is not a Gate A or Gate B member: %s", req.IssuePath),
			map[string]string{"issue_path": req.IssuePath},
		)
	}

	// --- Closure guards: ready_for_verification -> closed ---
	if req.FromState == StateReadyForVerification && req.ToState == StateClosed {
		if err := enforceClosureGuards(req, ref, ce, gateID, repoRoot); err != nil {
			return nil, err
		}
	}

	// --- Reopen guards: closed -> in_progress ---
	if req.FromState == StateClosed && req.ToState == StateInProgress {
		if req.Reason == "" || len(req.EvidenceRefs) == 0 {
			return nil, agencyerrors.NewWithDetails(
				agencyerrors.EGateReopenReasonRequired,
				"reopen transition requires non-empty reason and evidence_refs",
				map[string]string{"issue_path": req.IssuePath},
			)
		}
	}

	// --- Persist state ---
	if err := persistState(fullPath, req.ToState, string(data)); err != nil {
		return nil, agencyerrors.WrapWithDetails(
			agencyerrors.EGateTransitionInvalid,
			fmt.Sprintf("failed to persist state for %s", req.IssuePath),
			err,
			map[string]string{"issue_path": req.IssuePath},
		)
	}

	return &GateTransitionResult{
		IssuePath: req.IssuePath,
		State:     req.ToState,
	}, nil
}

// enforceClosureGuards applies the closure-specific guard chain in deterministic order.
func enforceClosureGuards(req GateTransition, ref *GateItemRef, ce *ClosureEvidence, gateID string, repoRoot string) error {
	// Guard 1: PR-01 evaluator blocking_code (acceptance/tests/closure-block/evidence preconditions)
	eval, err := EvaluateGateItem(req.IssuePath, repoRoot)
	if err != nil {
		return err
	}
	if eval.BlockingCode != "" {
		return agencyerrors.NewWithDetails(
			eval.BlockingCode,
			fmt.Sprintf("closure precondition not met for %s", req.IssuePath),
			map[string]string{"issue_path": req.IssuePath},
		)
	}

	// Guard 2: request evidence_refs and parsed implemented_refs non-empty
	implementedEmpty := ce == nil || len(ce.ImplementedRefs) == 0
	if len(req.EvidenceRefs) == 0 || implementedEmpty {
		return agencyerrors.NewWithDetails(
			agencyerrors.EGateItemEvidenceMissing,
			fmt.Sprintf("closure requires non-empty evidence_refs for %s", req.IssuePath),
			map[string]string{"issue_path": req.IssuePath},
		)
	}

	// Guard 3: Gate A/B role policy
	if gateID == "A" && req.ActorRole != RoleMaintainer {
		return agencyerrors.NewWithDetails(
			agencyerrors.EGateApprovalRequired,
			fmt.Sprintf("Gate A closure requires maintainer role, got %s", req.ActorRole),
			map[string]string{"issue_path": req.IssuePath, "gate": gateID},
		)
	}
	if gateID == "B" && req.ActorRole == RoleContributor {
		return agencyerrors.NewWithDetails(
			agencyerrors.EGateApprovalRequired,
			fmt.Sprintf("Gate B closure requires maintainer or reviewer role, got %s", req.ActorRole),
			map[string]string{"issue_path": req.IssuePath, "gate": gateID},
		)
	}

	// Guard 4: GH e2e evidence when requires_gh_e2e=true
	if ref.RequiresGHE2E {
		if !hasPassingE2EEvidence(ce) {
			return agencyerrors.NewWithDetails(
				agencyerrors.EGateE2ERequired,
				fmt.Sprintf("closure requires passing e2e_opt_in evidence for %s", req.IssuePath),
				map[string]string{"issue_path": req.IssuePath},
			)
		}
	}

	// Guard 5: design enforcement evidence when type=design
	if ref.Type == TypeDesign {
		if !hasEnforcementEvidence(ce) {
			return agencyerrors.NewWithDetails(
				agencyerrors.EGateItemTestsIncomplete,
				fmt.Sprintf("design item closure requires enforcement evidence for %s", req.IssuePath),
				map[string]string{"issue_path": req.IssuePath},
			)
		}
	}

	return nil
}

// resolveGateMembership reads the canonical release-gates source and returns
// the gate ID ("A" or "B") for the given issue path, or "" if the path is not
// a member of either gate. Returns an error if the source cannot be read/parsed.
func resolveGateMembership(issuePath string, repoRoot string) (string, error) {
	sourcePath := filepath.Join(repoRoot, CanonicalGateSourcePath)
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", fmt.Errorf("cannot read gate source: %w", err)
	}

	gs, err := ParseGateSet(string(data), CanonicalGateSourcePath, RepoFileExists(repoRoot))
	if err != nil {
		return "", fmt.Errorf("cannot parse gate source: %w", err)
	}

	for _, p := range gs.GateAItems {
		if p == issuePath {
			return "A", nil
		}
	}
	for _, p := range gs.GateBItems {
		if p == issuePath {
			return "B", nil
		}
	}

	return "", nil
}

// hasPassingE2EEvidence checks whether closure evidence contains at least one
// passing evidence row with scope=e2e_opt_in.
func hasPassingE2EEvidence(ce *ClosureEvidence) bool {
	if ce == nil {
		return false
	}
	for _, te := range ce.TargetedTestRefs {
		if te.Scope == ScopeE2EOptIn && te.Result == ResultPass {
			return true
		}
	}
	for _, te := range ce.SuiteTestRefs {
		if te.Scope == ScopeE2EOptIn && te.Result == ResultPass {
			return true
		}
	}
	return false
}

// hasEnforcementEvidence checks whether closure evidence contains at least one
// passing evidence row with an enforcement-eligible scope.
func hasEnforcementEvidence(ce *ClosureEvidence) bool {
	if ce == nil {
		return false
	}
	for _, te := range ce.TargetedTestRefs {
		if te.Result == ResultPass && EnforcementScopes[te.Scope] {
			return true
		}
	}
	for _, te := range ce.SuiteTestRefs {
		if te.Result == ResultPass && EnforcementScopes[te.Scope] {
			return true
		}
	}
	return false
}

// persistState writes the new state value into the issue stub file.
// It replaces an existing state: line, or inserts one after labels: (or after
// the title if labels: is absent).
func persistState(fullPath string, newState string, content string) error {
	lines := strings.Split(content, "\n")
	newStateLine := fmt.Sprintf("state: %s", newState)

	// Try to replace existing state: line.
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if stateRe.MatchString(trimmed) {
			lines[i] = newStateLine
			replaced = true
			break
		}
	}

	if !replaced {
		// Insert after labels: line, or after title if labels absent.
		insertIdx := findInsertIndex(lines)
		lines = insertLineAfter(lines, insertIdx, newStateLine)
	}

	return os.WriteFile(fullPath, []byte(strings.Join(lines, "\n")), 0o644)
}

// findInsertIndex returns the line index after which a new state: line should be inserted.
// Prefers position after labels: line; falls back to after the title line.
func findInsertIndex(lines []string) int {
	titleIdx := -1
	labelsIdx := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if titleIdx == -1 && titleRe.MatchString(trimmed) {
			titleIdx = i
		}
		if labelsRe.MatchString(trimmed) {
			labelsIdx = i
			break
		}
	}

	if labelsIdx >= 0 {
		return labelsIdx
	}
	if titleIdx >= 0 {
		return titleIdx
	}
	return 0
}

// insertLineAfter inserts a new line after the given index.
func insertLineAfter(lines []string, afterIdx int, newLine string) []string {
	insertAt := afterIdx + 1
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:insertAt]...)
	result = append(result, newLine)
	result = append(result, lines[insertAt:]...)
	return result
}
