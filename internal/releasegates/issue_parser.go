package releasegates

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

var titleRe = regexp.MustCompile(`^#\s+(.+)$`)
var titleTagRe = regexp.MustCompile(`\[([^\]]+)\]`)
var labelsRe = regexp.MustCompile(`(?i)^labels:\s*(.+)$`)
var stateRe = regexp.MustCompile(`(?i)^state:\s*(.+)$`)
var checkboxCheckedRe = regexp.MustCompile(`^\s*-\s+\[[xX]\]`)
var checkboxUncheckedRe = regexp.MustCompile(`^\s*-\s+\[\s\]`)
var h2Re = regexp.MustCompile(`^##\s+(.+)$`)

// ParseIssue parses an issue stub markdown into a normalized GateItemRef and
// optional ClosureEvidence.
func ParseIssue(content string, issuePath string) (*GateItemRef, *ClosureEvidence, error) {
	lines := strings.Split(content, "\n")

	title, titleTags := parseTitle(lines)
	if title == "" {
		return nil, nil, agencyerrors.New(agencyerrors.EGateItemInvalid, "missing title line")
	}

	labels := parseLabels(lines)
	state, err := parseStateLine(lines)
	if err != nil {
		return nil, nil, err
	}
	priority := derivePriority(labels, titleTags)
	if priority == "" {
		return nil, nil, agencyerrors.New(agencyerrors.EGateItemInvalid, "missing priority: could not resolve p0 or p1 from labels or title tags")
	}
	itemType := deriveType(labels)
	if itemType == "" {
		return nil, nil, agencyerrors.New(agencyerrors.EGateItemInvalid, "missing type: no valid type:* label found")
	}
	requiresGHE2E := hasLabel(labels, "requires:gh-e2e")

	sections := parseSections(content)
	acceptSection, hasAccept := sections["acceptance criteria"]
	if !hasAccept {
		return nil, nil, agencyerrors.New(agencyerrors.EGateItemInvalid, "missing ## acceptance criteria section")
	}

	acceptanceComplete := checkAcceptance(acceptSection)

	var ce *ClosureEvidence
	if ceSection, hasCE := sections["closure evidence"]; hasCE {
		var err error
		ce, err = parseClosureEvidenceBlock(ceSection, issuePath)
		if err != nil {
			return nil, nil, err
		}
	}

	var evidenceRefs []string
	if ce != nil {
		evidenceRefs = ce.ImplementedRefs
	}

	ref := &GateItemRef{
		IssuePath:          issuePath,
		Priority:           priority,
		Type:               itemType,
		State:              state,
		AcceptanceComplete: acceptanceComplete,
		RequiresGHE2E:      requiresGHE2E,
		EvidenceRefs:       evidenceRefs,
	}

	return ref, ce, nil
}

func parseTitle(lines []string) (string, []string) {
	for _, line := range lines {
		if m := titleRe.FindStringSubmatch(line); m != nil {
			title := m[1]
			tagMatches := titleTagRe.FindAllStringSubmatch(title, -1)
			var tags []string
			for _, tm := range tagMatches {
				tags = append(tags, strings.ToLower(strings.TrimSpace(tm[1])))
			}
			return title, tags
		}
	}
	return "", nil
}

func parseLabels(lines []string) []string {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := labelsRe.FindStringSubmatch(trimmed); m != nil {
			raw := m[1]
			parts := strings.Split(raw, ",")
			var labels []string
			for _, p := range parts {
				label := strings.TrimSpace(p)
				label = strings.Trim(label, "`")
				label = strings.TrimSpace(label)
				if label != "" {
					labels = append(labels, strings.ToLower(label))
				}
			}
			return labels
		}
	}
	return nil
}

func parseStateLine(lines []string) (string, error) {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := stateRe.FindStringSubmatch(trimmed); m != nil {
			s := strings.TrimSpace(strings.ToLower(m[1]))
			if ValidStates[s] {
				return s, nil
			}
			return "", agencyerrors.New(agencyerrors.EGateItemInvalid,
				fmt.Sprintf("invalid explicit state: %s", s))
		}
	}
	return StateOpen, nil
}

func derivePriority(labels []string, titleTags []string) string {
	for _, l := range labels {
		if ValidPriorities[l] {
			return l
		}
	}
	for _, t := range titleTags {
		if ValidPriorities[t] {
			return t
		}
	}
	return ""
}

func deriveType(labels []string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, "type:") {
			t := strings.TrimPrefix(l, "type:")
			if ValidTypes[t] {
				return t
			}
		}
	}
	return ""
}

func hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

func parseSections(content string) map[string]string {
	lines := strings.Split(content, "\n")
	sections := make(map[string]string)

	var currentName string
	var currentLines []string
	inFence := false

	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			if currentName != "" {
				currentLines = append(currentLines, line)
			}
			continue
		}

		if inFence {
			if currentName != "" {
				currentLines = append(currentLines, line)
			}
			continue
		}

		if m := h2Re.FindStringSubmatch(line); m != nil {
			if currentName != "" {
				sections[currentName] = strings.Join(currentLines, "\n")
			}
			currentName = normalizeHeading(m[1])
			currentLines = nil
			continue
		}

		if currentName != "" {
			currentLines = append(currentLines, line)
		}
	}

	if currentName != "" {
		sections[currentName] = strings.Join(currentLines, "\n")
	}

	return sections
}

func normalizeHeading(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.Join(strings.Fields(h), " ")
	h = strings.TrimRight(h, ":.-")
	return h
}

func checkAcceptance(sectionContent string) bool {
	lines := strings.Split(sectionContent, "\n")
	checked := 0
	unchecked := 0
	for _, line := range lines {
		if checkboxCheckedRe.MatchString(line) {
			checked++
		} else if checkboxUncheckedRe.MatchString(line) {
			unchecked++
		}
	}
	return checked > 0 && unchecked == 0
}

func parseClosureEvidenceBlock(sectionContent string, issuePath string) (*ClosureEvidence, error) {
	jsonContent, found := extractFirstFencedJSON(sectionContent)
	if !found {
		return nil, agencyerrors.New(agencyerrors.EGateItemInvalid, "closure evidence section has no fenced JSON block")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonContent), &raw); err != nil {
		return nil, agencyerrors.New(agencyerrors.EGateItemInvalid, fmt.Sprintf("invalid closure evidence JSON: %v", err))
	}

	requiredKeys := []string{"implemented_refs", "targeted_test_refs", "suite_test_refs"}
	for _, key := range requiredKeys {
		if _, ok := raw[key]; !ok {
			return nil, agencyerrors.New(agencyerrors.EGateItemInvalid, fmt.Sprintf("closure evidence missing required key: %s", key))
		}
	}

	var ce ClosureEvidence
	if err := json.Unmarshal([]byte(jsonContent), &ce); err != nil {
		return nil, agencyerrors.New(agencyerrors.EGateItemInvalid, fmt.Sprintf("closure evidence schema error: %v", err))
	}
	ce.IssuePath = issuePath

	for i, te := range ce.TargetedTestRefs {
		if !ValidScopes[te.Scope] {
			return nil, agencyerrors.New(agencyerrors.EGateItemInvalid,
				fmt.Sprintf("targeted_test_refs[%d]: invalid scope: %s", i, te.Scope))
		}
		if !ValidResults[te.Result] {
			return nil, agencyerrors.New(agencyerrors.EGateItemInvalid,
				fmt.Sprintf("targeted_test_refs[%d]: invalid result: %s", i, te.Result))
		}
		if te.Result == ResultPass && te.RecordedAt == "" {
			return nil, agencyerrors.New(agencyerrors.EGateItemInvalid,
				fmt.Sprintf("targeted_test_refs[%d]: pass evidence requires recorded_at", i))
		}
	}

	for i, te := range ce.SuiteTestRefs {
		if !ValidScopes[te.Scope] {
			return nil, agencyerrors.New(agencyerrors.EGateItemInvalid,
				fmt.Sprintf("suite_test_refs[%d]: invalid scope: %s", i, te.Scope))
		}
		if !ValidResults[te.Result] {
			return nil, agencyerrors.New(agencyerrors.EGateItemInvalid,
				fmt.Sprintf("suite_test_refs[%d]: invalid result: %s", i, te.Result))
		}
		if !AllowedSuiteCommands[te.Command] {
			return nil, agencyerrors.New(agencyerrors.EGateItemInvalid,
				fmt.Sprintf("suite_test_refs[%d]: command not in allowed suite list: %s", i, te.Command))
		}
		if te.Result == ResultPass && te.RecordedAt == "" {
			return nil, agencyerrors.New(agencyerrors.EGateItemInvalid,
				fmt.Sprintf("suite_test_refs[%d]: pass evidence requires recorded_at", i))
		}
	}

	return &ce, nil
}

func extractFirstFencedJSON(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	var blockLines []string
	inBlock := false

	for _, line := range lines {
		if !inBlock {
			trimmed := strings.TrimSpace(line)
			if trimmed == "```json" || trimmed == "```" {
				inBlock = true
				continue
			}
		} else {
			trimmed := strings.TrimSpace(line)
			if trimmed == "```" {
				return strings.Join(blockLines, "\n"), true
			}
			blockLines = append(blockLines, line)
		}
	}

	return "", false
}
