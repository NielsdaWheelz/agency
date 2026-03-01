package releasegates

import (
	"regexp"
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

var issueMapHeadingRe = regexp.MustCompile(`(?i)^##\s+Issue\s+Map\b`)

// ParseIssueMap parses the Issue Map section from the canonical constitution
// and returns deterministic issue occurrence counts.
func ParseIssueMap(content string) (*IssueMapResult, error) {
	lines := strings.Split(content, "\n")

	counts := make(map[string]int)
	var paths []string
	inFence := false
	inIssueMapSection := false

	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
		}
		if inFence {
			continue
		}

		if issueMapHeadingRe.MatchString(line) {
			inIssueMapSection = true
			continue
		}

		if !inIssueMapSection {
			continue
		}

		if anyH2Re.MatchString(line) {
			break
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
			"issue-map section contains no parseable issue references")
	}

	return &IssueMapResult{
		Paths:  paths,
		Counts: counts,
	}, nil
}
