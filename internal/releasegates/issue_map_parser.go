package releasegates

import (
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// ParseIssueMap parses an issue-map markdown document and returns deterministic
// issue occurrence counts.
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
