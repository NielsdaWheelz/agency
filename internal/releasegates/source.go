package releasegates

import (
	"fmt"
	"os"
	"path/filepath"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// Source reads release-gate issue metadata and evidence from markdown issue stubs.
type Source struct {
	RepoRoot string
}

// NewSource creates a Source for the given repo root.
func NewSource(repoRoot string) *Source {
	return &Source{RepoRoot: repoRoot}
}

// GetItemRef reads and parses the issue stub and returns a normalized GateItemRef.
func (m *Source) GetItemRef(issuePath string) (*GateItemRef, error) {
	content, err := m.readIssue(issuePath)
	if err != nil {
		return nil, err
	}
	ref, _, err := ParseIssue(content, issuePath)
	return ref, err
}

// GetClosureEvidence reads and parses the issue stub's closure evidence block.
func (m *Source) GetClosureEvidence(issuePath string) (*ClosureEvidence, error) {
	content, err := m.readIssue(issuePath)
	if err != nil {
		return nil, err
	}
	_, ce, err := ParseIssue(content, issuePath)
	if err != nil {
		return nil, err
	}
	return ce, nil
}

// Evaluate returns a full GateItemEvaluation for the given issue path.
func (m *Source) Evaluate(issuePath string) (*GateItemEvaluation, error) {
	return EvaluateGateItem(issuePath, m.RepoRoot)
}

func (m *Source) readIssue(issuePath string) (string, error) {
	fullPath := filepath.Join(m.RepoRoot, issuePath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", agencyerrors.NewWithDetails(
				agencyerrors.EGateItemNotFound,
				fmt.Sprintf("issue path does not exist: %s", issuePath),
				map[string]string{"issue_path": issuePath},
			)
		}
		return "", agencyerrors.Wrap(agencyerrors.EGateItemNotFound,
			fmt.Sprintf("cannot read issue: %s", issuePath), err)
	}
	return string(data), nil
}
