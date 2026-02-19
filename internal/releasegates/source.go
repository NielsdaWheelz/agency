package releasegates

import (
	"fmt"
	"os"
	"path/filepath"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// IssueSource abstracts gate-item metadata and evidence retrieval. The long-term
// migration path moves from markdown issue stubs (S1 compat) to GitHub issues.
type IssueSource interface {
	// GetItemRef returns normalized metadata for an issue path.
	GetItemRef(issuePath string) (*GateItemRef, error)
	// GetClosureEvidence returns closure evidence for an issue path, or nil if absent.
	GetClosureEvidence(issuePath string) (*ClosureEvidence, error)
	// Evaluate returns a full evaluation for a single issue path.
	Evaluate(issuePath string) (*GateItemEvaluation, error)
}

// MarkdownIssueSource is the S1 compatibility adapter that reads markdown issue stubs.
type MarkdownIssueSource struct {
	RepoRoot string
}

// NewMarkdownIssueSource creates a MarkdownIssueSource for the given repo root.
func NewMarkdownIssueSource(repoRoot string) *MarkdownIssueSource {
	return &MarkdownIssueSource{RepoRoot: repoRoot}
}

// GetItemRef reads and parses the issue stub and returns a normalized GateItemRef.
func (m *MarkdownIssueSource) GetItemRef(issuePath string) (*GateItemRef, error) {
	content, err := m.readIssue(issuePath)
	if err != nil {
		return nil, err
	}
	ref, _, err := ParseIssue(content, issuePath)
	return ref, err
}

// GetClosureEvidence reads and parses the issue stub's closure evidence block.
func (m *MarkdownIssueSource) GetClosureEvidence(issuePath string) (*ClosureEvidence, error) {
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
func (m *MarkdownIssueSource) Evaluate(issuePath string) (*GateItemEvaluation, error) {
	return EvaluateGateItem(issuePath, m.RepoRoot)
}

func (m *MarkdownIssueSource) readIssue(issuePath string) (string, error) {
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
