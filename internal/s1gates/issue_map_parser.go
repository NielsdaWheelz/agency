package s1gates

import (
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// IssueMapResult represents the parsed result of an issue-map document.
type IssueMapResult struct {
	Paths  []string       // unique issue paths in first-encounter order
	Counts map[string]int // occurrence count per issue path
}

// ParseIssueMap parses an issue-map markdown document and returns deterministic
// issue occurrence counts. It accepts numbered list items with backtick-wrapped
// issue paths under H2 slice sections. Content inside fenced code blocks is
// ignored. Returns E_GATE_SET_INVALID if no issue references are found.
func ParseIssueMap(content string) (*IssueMapResult, error) {
	lines := strings.Split(content, "\n")

	counts := make(map[string]int)
	var paths []string
	inFence := false
	underH2 := false

	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
		}
		if inFence {
			continue
		}

		if anyH2Re.MatchString(line) {
			underH2 = true
			continue
		}

		if !underH2 {
			continue
		}

		if m := numberedItemRe.FindStringSubmatch(line); m != nil {
			issuePath := m[1]
			if counts[issuePath] == 0 {
				paths = append(paths, issuePath)
			}
			counts[issuePath]++
		}
	}

	if len(paths) == 0 {
		return nil, agencyerrors.New(agencyerrors.EGateSetInvalid,
			"issue-map contains no parseable issue references")
	}

	return &IssueMapResult{
		Paths:  paths,
		Counts: counts,
	}, nil
}
