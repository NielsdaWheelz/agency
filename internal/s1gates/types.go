// Package s1gates implements deterministic gate corpus intake and gate-item
// evaluation for v2.1 Slice S1 platform hardening gates.
package s1gates

import (
	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// Gate item priorities.
const (
	PriorityP0 = "p0"
	PriorityP1 = "p1"
)

// Gate item types.
const (
	TypeTechDebt    = "tech-debt"
	TypeBug         = "bug"
	TypeDesign      = "design"
	TypeEnhancement = "enhancement"
	TypeRefactor    = "refactor"
)

// Gate item states.
const (
	StateOpen                 = "open"
	StateInProgress           = "in_progress"
	StateReadyForVerification = "ready_for_verification"
	StateClosed               = "closed"
)

// Actor roles for gate-item transitions.
const (
	RoleMaintainer  = "maintainer"
	RoleReviewer    = "reviewer"
	RoleContributor = "contributor"
)

// Test evidence scopes.
const (
	ScopeTargeted = "targeted"
	ScopeSuite    = "suite"
	ScopeE2EOptIn = "e2e_opt_in"
	ScopeLint     = "lint"
	ScopeFormat   = "format"
	ScopeVet      = "vet"
)

// Test evidence results.
const (
	ResultPass = "pass"
	ResultFail = "fail"
)

// Missing requirement keys (deterministic, ordered by blocking_code precedence).
const (
	MissingAcceptanceIncomplete = "acceptance_incomplete"
	MissingClosureEvidenceBlock = "closure_evidence_block"
	MissingEvidenceRefs         = "evidence_refs_missing"
	MissingTargetedTestEvidence = "targeted_test_evidence"
	MissingSuiteTestEvidence    = "suite_test_evidence"
)

// AllowedSuiteCommands defines the commands valid for scope=suite evidence.
var AllowedSuiteCommands = map[string]bool{
	"go test ./...": true,
	"make test":     true,
	"make check":    true,
	"make verify":   true,
}

// ValidStates is the set of legal gate-item states.
var ValidStates = map[string]bool{
	StateOpen:                 true,
	StateInProgress:           true,
	StateReadyForVerification: true,
	StateClosed:               true,
}

// ValidPriorities is the set of legal gate-item priorities.
var ValidPriorities = map[string]bool{
	PriorityP0: true,
	PriorityP1: true,
}

// ValidTypes is the set of legal gate-item types.
var ValidTypes = map[string]bool{
	TypeTechDebt:    true,
	TypeBug:         true,
	TypeDesign:      true,
	TypeEnhancement: true,
	TypeRefactor:    true,
}

// ValidScopes is the set of legal test evidence scopes.
var ValidScopes = map[string]bool{
	ScopeTargeted: true,
	ScopeSuite:    true,
	ScopeE2EOptIn: true,
	ScopeLint:     true,
	ScopeFormat:   true,
	ScopeVet:      true,
}

// ValidResults is the set of legal test evidence results.
var ValidResults = map[string]bool{
	ResultPass: true,
	ResultFail: true,
}

// ValidActorRoles is the set of legal actor roles for transitions.
var ValidActorRoles = map[string]bool{
	RoleMaintainer:  true,
	RoleReviewer:    true,
	RoleContributor: true,
}

// EnforcementScopes defines scopes that count as contract enforcement evidence
// for type=design gate items.
var EnforcementScopes = map[string]bool{
	ScopeTargeted: true,
	ScopeSuite:    true,
	ScopeE2EOptIn: true,
	ScopeLint:     true,
	ScopeFormat:   true,
	ScopeVet:      true,
}

// FileExistsFn reports whether a file path exists.
type FileExistsFn func(path string) bool

// GateSet represents the resolved gate membership from the canonical source.
type GateSet struct {
	Slice      string   `json:"slice"`
	GateAItems []string `json:"gate_a_items"`
	GateBItems []string `json:"gate_b_items"`
	SourceRef  string   `json:"source_ref"`
}

// GateItemRef represents a resolved gate item with normalized metadata.
type GateItemRef struct {
	IssuePath          string   `json:"issue_path"`
	Priority           string   `json:"priority"`
	Type               string   `json:"type"`
	State              string   `json:"state"`
	AcceptanceComplete bool     `json:"acceptance_complete"`
	RequiresGHE2E      bool     `json:"requires_gh_e2e"`
	EvidenceRefs       []string `json:"evidence_refs"`
}

// TestEvidence represents a single test evidence entry.
type TestEvidence struct {
	IssuePath   string `json:"issue_path"`
	Command     string `json:"command"`
	Scope       string `json:"scope"`
	Result      string `json:"result"`
	ArtifactRef string `json:"artifact_ref"`
	RecordedAt  string `json:"recorded_at"`
}

// ClosureEvidence represents the closure evidence block for a gate item.
type ClosureEvidence struct {
	IssuePath        string         `json:"issue_path"`
	ImplementedRefs  []string       `json:"implemented_refs"`
	TargetedTestRefs []TestEvidence `json:"targeted_test_refs"`
	SuiteTestRefs    []TestEvidence `json:"suite_test_refs"`
	Notes            string         `json:"notes,omitempty"`
}

// GateItemEvaluation represents the evaluation result for a single gate item.
type GateItemEvaluation struct {
	IssuePath              string            `json:"issue_path"`
	State                  string            `json:"state"`
	RequiresGHE2E          bool              `json:"requires_gh_e2e"`
	AcceptanceComplete     bool              `json:"acceptance_complete"`
	TestsComplete          bool              `json:"tests_complete"`
	ClosureEvidencePresent bool              `json:"closure_evidence_present"`
	MissingRequirements    []string          `json:"missing_requirements"`
	EvidenceRefs           []string          `json:"evidence_refs"`
	BlockingCode           agencyerrors.Code `json:"blocking_code,omitempty"`
}

// GateTransition represents a gate-item state transition request.
type GateTransition struct {
	IssuePath    string   `json:"issue_path"`
	FromState    string   `json:"from_state"`
	ToState      string   `json:"to_state"`
	ActorRole    string   `json:"actor_role"`
	Reason       string   `json:"reason"`
	EvidenceRefs []string `json:"evidence_refs"`
}

// GateTransitionResult represents the outcome of a successful gate-item transition.
type GateTransitionResult struct {
	IssuePath string `json:"issue_path"`
	State     string `json:"state"`
}

// CanonicalGateSourcePath is the repo-relative path to the release-gates source.
const CanonicalGateSourcePath = "docs/v2.1/release-gates.md"

// CanonicalIssueMapPath is the repo-relative path to the issue-map source.
const CanonicalIssueMapPath = "docs/v2.1/issue-map.md"

// Gate IDs.
const (
	GateIDA = "A"
	GateIDB = "B"
)

// Change types for gate-set change proposals.
const (
	ChangeTypeAdd     = "add"
	ChangeTypeRemove  = "remove"
	ChangeTypeReplace = "replace"
	ChangeTypeReorder = "reorder"
)

// ValidGateIDs is the set of legal gate IDs for change validation.
var ValidGateIDs = map[string]bool{
	GateIDA: true,
	GateIDB: true,
}

// ValidChangeTypes is the set of legal change types for gate-set change proposals.
var ValidChangeTypes = map[string]bool{
	ChangeTypeAdd:     true,
	ChangeTypeRemove:  true,
	ChangeTypeReplace: true,
	ChangeTypeReorder: true,
}

// Gate status values.
const (
	GateStatusReady   = "ready"
	GateStatusBlocked = "blocked"
)

// GateStatus represents the aggregate evaluation status of a single gate.
type GateStatus struct {
	GateID        string   `json:"gate_id"`
	TotalItems    int      `json:"total_items"`
	ClosedItems   int      `json:"closed_items"`
	BlockingItems []string `json:"blocking_items"`
	Status        string   `json:"status"`
}

// GatesEvaluateRequest represents the request for aggregate gate evaluation.
type GatesEvaluateRequest struct {
	GateSetSource string `json:"gate_set_source"`
	Slice         string `json:"slice"`
}

// GatesEvaluateResult represents the aggregate gate evaluation result.
type GatesEvaluateResult struct {
	Slice      string      `json:"slice"`
	GateA      *GateStatus `json:"gate_a"`
	GateB      *GateStatus `json:"gate_b"`
	SliceReady bool        `json:"slice_ready"`
}

// GateSetChange represents a gate-set change proposal for validation.
type GateSetChange struct {
	GateID         string   `json:"gate_id"`
	ChangeType     string   `json:"change_type"`
	IssuePath      string   `json:"issue_path"`
	IssuePaths     []string `json:"issue_paths"`
	Reason         string   `json:"reason"`
	ApprovedBy     string   `json:"approved_by"`
	SyncedIssueMap bool     `json:"synced_issue_map"`
}

// GateSetChangeValidationResult represents the outcome of gate-set change validation.
type GateSetChangeValidationResult struct {
	Valid bool `json:"valid"`
}
