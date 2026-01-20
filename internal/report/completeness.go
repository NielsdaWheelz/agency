// Package report provides parsing and validation for agency report files.
package report

import (
	"regexp"
	"strings"
)

// CompletenessResult holds the result of report completeness validation.
type CompletenessResult struct {
	// Complete is true if all required sections have content.
	Complete bool

	// MissingSections lists the names of required sections that are empty or missing.
	MissingSections []string

	// SummaryFound indicates the summary section was found and has content.
	SummaryFound bool

	// HowToTestFound indicates the how-to-test section was found and has content.
	HowToTestFound bool
}

// RequiredSections defines the canonical names of required sections.
var RequiredSections = []string{"summary", "how to test"}

// headingAliases maps normalized heading names to their canonical names.
var headingAliases = map[string]string{
	"summary":     "summary",
	"overview":    "summary",
	"how to test": "how to test",
	"how-to-test": "how to test",
	"testing":     "how to test",
	"tests":       "how to test",
}

// headingPattern matches exactly ## followed by whitespace and title text.
// Excludes ### and deeper headings.
var headingPattern = regexp.MustCompile(`^##\s+(.+)$`)

// fencePattern matches the start of a fenced code block (``` with optional language).
var fencePattern = regexp.MustCompile(`^\x60\x60\x60`)

// CheckCompleteness validates whether a report has all required sections filled.
// A report is complete if both "summary" and "how to test" sections contain
// at least one non-whitespace character.
func CheckCompleteness(content string) *CompletenessResult {
	sections := parseSections(content)

	result := &CompletenessResult{
		MissingSections: make([]string, 0),
	}

	// Check summary (or overview alias)
	summaryContent := getSectionContent(sections, "summary")
	result.SummaryFound = strings.TrimSpace(summaryContent) != ""

	// Check how to test (or aliases)
	howToTestContent := getSectionContent(sections, "how to test")
	result.HowToTestFound = strings.TrimSpace(howToTestContent) != ""

	// Build missing sections list
	if !result.SummaryFound {
		result.MissingSections = append(result.MissingSections, "summary")
	}
	if !result.HowToTestFound {
		result.MissingSections = append(result.MissingSections, "how to test")
	}

	result.Complete = len(result.MissingSections) == 0
	return result
}

// section represents a parsed section from the report.
type section struct {
	name    string // canonical name
	content string // content between this heading and the next
}

// parseSections parses markdown content into sections.
// Only ## headings are treated as section boundaries (### and deeper are included in content).
// Headings inside fenced code blocks are ignored.
func parseSections(content string) []section {
	lines := strings.Split(content, "\n")
	sections := make([]section, 0)

	var currentSection *section
	inFencedBlock := false

	for _, line := range lines {
		// Track fenced code blocks
		if fencePattern.MatchString(line) {
			inFencedBlock = !inFencedBlock
		}

		// Skip heading detection inside fenced blocks
		if inFencedBlock {
			if currentSection != nil {
				currentSection.content += line + "\n"
			}
			continue
		}

		// Check for ## heading (not ### or deeper)
		if match := headingPattern.FindStringSubmatch(line); match != nil {
			// Save previous section
			if currentSection != nil {
				sections = append(sections, *currentSection)
			}

			// Start new section
			normalized := normalizeHeading(match[1])
			canonical := resolveAlias(normalized)
			currentSection = &section{
				name:    canonical,
				content: "",
			}
		} else if currentSection != nil {
			// Add content to current section
			currentSection.content += line + "\n"
		}
	}

	// Save final section
	if currentSection != nil {
		sections = append(sections, *currentSection)
	}

	return sections
}

// normalizeHeading applies normalization rules to a heading title.
// - lowercase
// - trim leading/trailing whitespace
// - collapse multiple consecutive spaces to single space
// - strip trailing punctuation (: . -)
func normalizeHeading(title string) string {
	// Lowercase
	result := strings.ToLower(title)

	// Trim leading/trailing whitespace
	result = strings.TrimSpace(result)

	// Collapse multiple spaces to single space
	result = strings.Join(strings.Fields(result), " ")

	// Strip trailing punctuation
	result = strings.TrimRight(result, ":.-")

	return result
}

// resolveAlias maps a normalized heading name to its canonical name.
// Returns the input if no alias mapping exists.
func resolveAlias(normalized string) string {
	if canonical, ok := headingAliases[normalized]; ok {
		return canonical
	}
	return normalized
}

// getSectionContent returns the content for a canonical section name.
// Uses first occurrence if duplicates exist.
func getSectionContent(sections []section, canonicalName string) string {
	for _, s := range sections {
		if s.name == canonicalName {
			return s.content
		}
	}
	return ""
}
