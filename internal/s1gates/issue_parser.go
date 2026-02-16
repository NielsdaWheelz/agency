package s1gates

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

// titleRe matches the issue title line.
var titleRe = regexp.MustCompile(`^#\s+(.+)$`)

// titleTagRe extracts [tag] entries from the title.
var titleTagRe = regexp.MustCompile(`\[([^\]]+)\]`)

// labelsRe matches the labels metadata line.
var labelsRe = regexp.MustCompile(`(?i)^labels:\s*(.+)$`)

// stateRe matches the optional state metadata line.
var stateRe = regexp.MustCompile(`(?i)^state:\s*(.+)$`)

// checkboxCheckedRe matches a checked checkbox.
var checkboxCheckedRe = regexp.MustCompile(`^\s*-\s+\[[xX]\]`)

// checkboxUncheckedRe matches an unchecked checkbox.
var checkboxUncheckedRe = regexp.MustCompile(`^\s*-\s+\[\s\]`)

// h2Re matches ## headings (not deeper).
var h2Re = regexp.MustCompile(`^##\s+(.+)$`)

// ParseIssue parses an issue stub markdown into a normalized GateItemRef and
// optional ClosureEvidence. The issue must contain a title line and an
// ## acceptance criteria section; missing sections produce E_GATE_ITEM_INVALID.
func ParseIssue(content string, issuePath string) (*GateItemRef, *ClosureEvidence, error) {
	lines := strings.Split(content, "\n")

	// Extract metadata from header area.
	title, titleTags := parseTitle(lines)
	if title == "" {
		return nil, nil, agencyerrors.New(agencyerrors.EGateItemInvalid, "missing title line")
	}

	labels := parseLabels(lines)
	state := parseStateLine(lines)
	priority := derivePriority(labels, titleTags)
	itemType := deriveType(labels)
	requiresGHE2E := hasLabel(labels, "requires:gh-e2e")

	// Parse sections.
	sections := parseSections(content)
	acceptSection, hasAccept := sections["acceptance criteria"]
	if !hasAccept {
		return nil, nil, agencyerrors.New(agencyerrors.EGateItemInvalid, "missing ## acceptance criteria section")
	}

	acceptanceComplete := checkAcceptance(acceptSection)

	// Parse optional closure evidence.
	var ce *ClosureEvidence
	if ceSection, hasCE := sections["closure evidence"]; hasCE {
		var err error
		ce, err = parseClosureEvidenceBlock(ceSection, issuePath)
		if err != nil {
			return nil, nil, err
		}
	}

	// Build evidence refs from closure evidence.
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

// parseTitle extracts the title text and square-bracket tags from the first # heading.
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

// parseLabels extracts and normalizes labels from the labels: metadata line.
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

// parseStateLine extracts the state from the optional state: metadata line.
func parseStateLine(lines []string) string {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := stateRe.FindStringSubmatch(trimmed); m != nil {
			s := strings.TrimSpace(strings.ToLower(m[1]))
			if ValidStates[s] {
				return s
			}
		}
	}
	return StateOpen
}

// derivePriority extracts priority from labels first, then title tags as fallback.
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

// deriveType extracts the type from labels with "type:" prefix.
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

// hasLabel checks whether the given label string is present.
func hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

// parseSections splits markdown into ## sections, returning a map of
// normalized heading -> content. Headings inside fenced blocks are ignored.
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
			// Save previous section.
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

// normalizeHeading lowercases, trims, and strips trailing punctuation from a heading.
func normalizeHeading(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.Join(strings.Fields(h), " ")
	h = strings.TrimRight(h, ":.-")
	return h
}

// checkAcceptance returns true if all checkboxes in the section are checked.
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

// parseClosureEvidenceBlock extracts and validates the first fenced JSON block
// from the closure evidence section content.
func parseClosureEvidenceBlock(sectionContent string, issuePath string) (*ClosureEvidence, error) {
	jsonContent, found := extractFirstFencedJSON(sectionContent)
	if !found {
		return nil, agencyerrors.New(agencyerrors.EGateItemInvalid, "closure evidence section has no fenced JSON block")
	}

	// Verify required top-level keys exist.
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

	// Unmarshal into typed struct.
	var ce ClosureEvidence
	if err := json.Unmarshal([]byte(jsonContent), &ce); err != nil {
		return nil, agencyerrors.New(agencyerrors.EGateItemInvalid, fmt.Sprintf("closure evidence schema error: %v", err))
	}
	ce.IssuePath = issuePath

	// Validate test evidence entries.
	for i, te := range ce.TargetedTestRefs {
		if te.Result == ResultPass && te.RecordedAt == "" {
			return nil, agencyerrors.New(agencyerrors.EGateItemInvalid,
				fmt.Sprintf("targeted_test_refs[%d]: pass evidence requires recorded_at", i))
		}
	}

	for i, te := range ce.SuiteTestRefs {
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

// extractFirstFencedJSON finds the first fenced code block (optionally tagged
// json) and returns its content.
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
