package releasegates

import (
	"fmt"
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

const driftSyncSource = "release-gates_vs_issue-map"

// ValidateGateSetChange validates a gate-set change proposal deterministically.
func ValidateGateSetChange(req GateSetChange, repoRoot string) (*GateSetChangeValidationResult, error) {
	if strings.TrimSpace(req.Reason) == "" {
		return nil, agencyerrors.NewWithDetails(
			agencyerrors.EGateChangeReasonRequired,
			"change reason is required",
			map[string]string{
				"gate_id":     req.GateID,
				"change_type": req.ChangeType,
				"field":       "reason",
			},
		)
	}

	if !ValidGateIDs[req.GateID] || !ValidChangeTypes[req.ChangeType] {
		return nil, changeTargetError(req, "issue_path", "invalid_enum",
			"invalid gate_id or change_type enum value")
	}

	if err := validateTargetRequirements(req); err != nil {
		return nil, err
	}

	if err := validateTargetExclusivity(req); err != nil {
		return nil, err
	}

	gateSet, err := loadGateSetForChangeValidation(repoRoot)
	if err != nil {
		return nil, err
	}

	if err := validateMembershipIntent(req, gateSet, repoRoot); err != nil {
		return nil, err
	}

	if err := validateApproval(req); err != nil {
		return nil, err
	}

	if err := validateSync(req, repoRoot, gateSet); err != nil {
		return nil, err
	}

	return &GateSetChangeValidationResult{Valid: true}, nil
}

func validateTargetRequirements(req GateSetChange) error {
	switch req.ChangeType {
	case ChangeTypeAdd, ChangeTypeRemove:
		if strings.TrimSpace(req.IssuePath) == "" {
			return changeTargetError(req, targetKindFor(req.ChangeType), "missing",
				"issue_path is required for "+req.ChangeType)
		}
	case ChangeTypeReplace:
		if len(req.IssuePaths) != 2 {
			return changeTargetError(req, "issue_paths", "missing",
				"replace requires issue_paths with exactly 2 entries")
		}
	case ChangeTypeReorder:
		if len(req.IssuePaths) < 2 {
			return changeTargetError(req, "issue_paths", "missing",
				"reorder requires issue_paths with at least 2 entries")
		}
	}
	return nil
}

func validateTargetExclusivity(req GateSetChange) error {
	switch req.ChangeType {
	case ChangeTypeAdd, ChangeTypeRemove:
		if len(req.IssuePaths) > 0 {
			return changeTargetError(req, targetKindFor(req.ChangeType), "exclusivity",
				"issue_paths must be empty for "+req.ChangeType)
		}
	case ChangeTypeReplace, ChangeTypeReorder:
		if req.IssuePath != "" {
			return changeTargetError(req, "issue_paths", "exclusivity",
				"issue_path must be empty for "+req.ChangeType)
		}
	}
	return nil
}

func loadGateSetForChangeValidation(repoRoot string) (*GateSet, error) {
	gateSet, err := LoadGateSet(repoRoot)
	if err != nil {
		return nil, newDriftError("", "", "source_invalid",
			"canonical gate source is unreadable or malformed")
	}
	return gateSet, nil
}

func validateMembershipIntent(req GateSetChange, gateSet *GateSet, repoRoot string) error {
	membership := buildMembershipMap(gateSet)

	switch req.ChangeType {
	case ChangeTypeAdd:
		if !RepoFileExists(repoRoot)(req.IssuePath) {
			return changeTargetError(req, "issue_path", "membership_intent",
				fmt.Sprintf("issue_path does not exist: %s", req.IssuePath))
		}
		if _, exists := membership[req.IssuePath]; exists {
			return changeTargetError(req, "issue_path", "membership_intent",
				fmt.Sprintf("issue_path already in gate: %s", req.IssuePath))
		}

	case ChangeTypeRemove:
		gate, exists := membership[req.IssuePath]
		if !exists || gate != req.GateID {
			return changeTargetError(req, "issue_path", "membership_intent",
				fmt.Sprintf("issue_path is not a member of Gate %s: %s", req.GateID, req.IssuePath))
		}

	case ChangeTypeReplace:
		fromPath := req.IssuePaths[0]
		toPath := req.IssuePaths[1]

		gate, exists := membership[fromPath]
		if !exists || gate != req.GateID {
			return changeTargetError(req, "issue_paths", "membership_intent",
				fmt.Sprintf("from_issue_path is not a member of Gate %s: %s", req.GateID, fromPath))
		}
		if !RepoFileExists(repoRoot)(toPath) {
			return changeTargetError(req, "issue_paths", "membership_intent",
				fmt.Sprintf("to_issue_path does not exist: %s", toPath))
		}
		if _, exists := membership[toPath]; exists {
			return changeTargetError(req, "issue_paths", "membership_intent",
				fmt.Sprintf("to_issue_path already in gate: %s", toPath))
		}
		if fromPath == toPath {
			return changeTargetError(req, "issue_paths", "membership_intent",
				"from_issue_path and to_issue_path must be different")
		}

	case ChangeTypeReorder:
		return validateReorderMembership(req, gateSet)
	}
	return nil
}

func validateReorderMembership(req GateSetChange, gateSet *GateSet) error {
	var gateItems []string
	if req.GateID == GateIDA {
		gateItems = gateSet.GateAItems
	} else {
		gateItems = gateSet.GateBItems
	}

	if len(req.IssuePaths) != len(gateItems) {
		return changeTargetError(req, "issue_paths", "membership_reorder",
			"reorder must be a membership-preserving permutation")
	}

	expected := make(map[string]bool, len(gateItems))
	for _, p := range gateItems {
		expected[p] = true
	}

	seen := make(map[string]bool, len(req.IssuePaths))
	for _, p := range req.IssuePaths {
		if !expected[p] {
			return changeTargetError(req, "issue_paths", "membership_reorder",
				fmt.Sprintf("issue_path is not a member of Gate %s: %s", req.GateID, p))
		}
		if seen[p] {
			return changeTargetError(req, "issue_paths", "membership_reorder",
				fmt.Sprintf("duplicate issue_path in reorder: %s", p))
		}
		seen[p] = true
	}
	return nil
}

func validateApproval(req GateSetChange) error {
	if (req.ChangeType == ChangeTypeRemove || req.ChangeType == ChangeTypeReplace) &&
		strings.TrimSpace(req.ApprovedBy) == "" {
		return agencyerrors.NewWithDetails(
			agencyerrors.EGateChangeApprovalRequired,
			fmt.Sprintf("approval is required for %s", req.ChangeType),
			map[string]string{
				"gate_id":     req.GateID,
				"change_type": req.ChangeType,
				"field":       "approved_by",
			},
		)
	}
	return nil
}

func validateSync(req GateSetChange, repoRoot string, gateSet *GateSet) error {
	if !req.SyncedIssueMap {
		return newDriftError("", "", "unsynced_flag", "synced_issue_map must be true")
	}

	issueMap, err := LoadIssueMap(repoRoot)
	if err != nil {
		return newDriftError("", "", "source_invalid",
			"canonical issue-map is unreadable or malformed")
	}

	allItems := make([]string, 0, len(gateSet.GateAItems)+len(gateSet.GateBItems))
	allItems = append(allItems, gateSet.GateAItems...)
	allItems = append(allItems, gateSet.GateBItems...)

	for _, issuePath := range allItems {
		count := issueMap.Counts[issuePath]
		if count == 1 {
			continue
		}
		driftKind := "missing"
		if count > 1 {
			driftKind = "duplicate"
		}
		return newDriftError(issuePath, fmt.Sprintf("%d", count), driftKind,
			fmt.Sprintf("gate-source/issue-map drift: %s", issuePath))
	}
	return nil
}

func changeTargetError(req GateSetChange, targetKind, violation, msg string) error {
	return agencyerrors.NewWithDetails(
		agencyerrors.EGateChangeTargetRequired,
		msg,
		map[string]string{
			"gate_id":          req.GateID,
			"change_type":      req.ChangeType,
			"target_kind":      targetKind,
			"target_violation": violation,
		},
	)
}

func newDriftError(issuePath, issueMapCount, driftKind, msg string) error {
	return agencyerrors.NewWithDetails(
		agencyerrors.EGateSetDrift,
		msg,
		map[string]string{
			"issue_path":      issuePath,
			"issue_map_count": issueMapCount,
			"drift_kind":      driftKind,
			"sync_source":     driftSyncSource,
		},
	)
}

func targetKindFor(changeType string) string {
	switch changeType {
	case ChangeTypeReplace, ChangeTypeReorder:
		return "issue_paths"
	default:
		return "issue_path"
	}
}

func buildMembershipMap(gateSet *GateSet) map[string]string {
	m := make(map[string]string, len(gateSet.GateAItems)+len(gateSet.GateBItems))
	for _, p := range gateSet.GateAItems {
		m[p] = GateIDA
	}
	for _, p := range gateSet.GateBItems {
		m[p] = GateIDB
	}
	return m
}
