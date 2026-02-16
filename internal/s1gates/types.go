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
