package report

import (
	"testing"
)

func TestCheckCompleteness_EmptyTemplate(t *testing.T) {
	// Per spec: section is complete if it contains ≥1 non-whitespace character
	// Template placeholder "- ..." counts as content, so this is complete
	content := `# my-feature

## summary
- ...

## how to test
- ...
`
	result := CheckCompleteness(content)
	// "- ..." is non-whitespace content, so both sections are found
	if !result.Complete {
		t.Errorf("template with placeholder content should be complete, missing: %v", result.MissingSections)
	}
}

func TestCheckCompleteness_TrulyEmptySections(t *testing.T) {
	// Sections with only whitespace should be incomplete
	content := `# my-feature

## summary

## how to test

`
	result := CheckCompleteness(content)
	if result.Complete {
		t.Error("empty sections should be incomplete")
	}
	if len(result.MissingSections) != 2 {
		t.Errorf("expected 2 missing sections, got %d: %v", len(result.MissingSections), result.MissingSections)
	}
}

func TestCheckCompleteness_SummaryFilledOnly(t *testing.T) {
	// Per spec: section with any non-whitespace is complete
	// So "- ..." counts as content
	content := `# my-feature

## summary
This is a real summary with actual content.

## how to test

`
	result := CheckCompleteness(content)
	if result.Complete {
		t.Error("should be incomplete when only summary is filled")
	}
	if result.SummaryFound != true {
		t.Error("summary should be found")
	}
	if result.HowToTestFound != false {
		t.Error("how to test should not be found (whitespace only)")
	}
	if len(result.MissingSections) != 1 || result.MissingSections[0] != "how to test" {
		t.Errorf("expected only 'how to test' missing, got: %v", result.MissingSections)
	}
}

func TestCheckCompleteness_HowToTestFilledOnly(t *testing.T) {
	// Per spec: section with any non-whitespace is complete
	// Summary is whitespace-only, how-to-test has content
	content := `# my-feature

## summary

## how to test
Run the following command:
` + "```" + `bash
go test ./internal/report/...
` + "```" + `
`
	result := CheckCompleteness(content)
	if result.Complete {
		t.Error("should be incomplete when only how-to-test is filled")
	}
	if result.SummaryFound != false {
		t.Error("summary should not be found (whitespace only)")
	}
	if result.HowToTestFound != true {
		t.Error("how to test should be found")
	}
	if len(result.MissingSections) != 1 || result.MissingSections[0] != "summary" {
		t.Errorf("expected only 'summary' missing, got: %v", result.MissingSections)
	}
}

func TestCheckCompleteness_BothFilled(t *testing.T) {
	content := `# my-feature

## summary
Added user authentication with JWT tokens.

## how to test
Run the test suite:
` + "```" + `bash
go test ./...
` + "```" + `
`
	result := CheckCompleteness(content)
	if !result.Complete {
		t.Error("should be complete when both sections are filled")
	}
	if len(result.MissingSections) != 0 {
		t.Errorf("expected no missing sections, got: %v", result.MissingSections)
	}
}

func TestCheckCompleteness_WhitespaceOnlyContent(t *testing.T) {
	// Whitespace-only should count as empty
	content := `# my-feature

## summary
   
	

## how to test
  	  
`
	result := CheckCompleteness(content)
	if result.Complete {
		t.Error("whitespace-only content should be treated as empty")
	}
	if result.SummaryFound {
		t.Error("whitespace-only summary should not be found")
	}
	if result.HowToTestFound {
		t.Error("whitespace-only how-to-test should not be found")
	}
}

func TestCheckCompleteness_ReorderedSections(t *testing.T) {
	// Sections in different order should still be recognized
	content := `# my-feature

## how to test
Run tests with: go test ./...

## summary
Implemented feature X.
`
	result := CheckCompleteness(content)
	if !result.Complete {
		t.Error("should be complete regardless of section order")
	}
}

func TestCheckCompleteness_AliasHeadings(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "overview instead of summary",
			content: `# test

## overview
This is the overview.

## how to test
Run tests.
`,
			want: true,
		},
		{
			name: "testing instead of how to test",
			content: `# test

## summary
Summary here.

## testing
Run the tests.
`,
			want: true,
		},
		{
			name: "tests instead of how to test",
			content: `# test

## summary
Summary here.

## tests
Test instructions.
`,
			want: true,
		},
		{
			name: "how-to-test hyphenated",
			content: `# test

## summary
Summary here.

## how-to-test
Test instructions.
`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckCompleteness(tt.content)
			if result.Complete != tt.want {
				t.Errorf("Complete = %v, want %v", result.Complete, tt.want)
			}
		})
	}
}

func TestCheckCompleteness_HeadingsInsideFencedCodeBlocks(t *testing.T) {
	// Headings inside code blocks should NOT be treated as section boundaries
	content := `# my-feature

## summary
Here's some example markdown:

` + "```" + `markdown
## This is NOT a real heading
Some content
` + "```" + `

Real summary content here.

## how to test
Test instructions.
`
	result := CheckCompleteness(content)
	if !result.Complete {
		t.Error("should be complete - headings in code blocks should be ignored")
	}
	if !result.SummaryFound {
		t.Error("summary should be found with content outside code block")
	}
}

func TestCheckCompleteness_CaseVariations(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "uppercase SUMMARY",
			content: `# test
## SUMMARY
Content.
## HOW TO TEST
Test.
`,
		},
		{
			name: "mixed case Summary",
			content: `# test
## Summary
Content.
## How To Test
Test.
`,
		},
		{
			name: "mixed case with trailing",
			content: `# test
## SUMMARY:
Content.
## How to Test:
Test.
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckCompleteness(tt.content)
			if !result.Complete {
				t.Errorf("should recognize case variations as complete, got missing: %v", result.MissingSections)
			}
		})
	}
}

func TestCheckCompleteness_TrailingPunctuation(t *testing.T) {
	tests := []struct {
		name    string
		heading string
	}{
		{"colon", "## summary:"},
		{"period", "## summary."},
		{"dash", "## summary-"},
		{"multiple", "## summary:.."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := `# test
` + tt.heading + `
Content here.
## how to test
Test instructions.
`
			result := CheckCompleteness(content)
			if !result.SummaryFound {
				t.Errorf("heading %q should match summary after stripping punctuation", tt.heading)
			}
		})
	}
}

func TestCheckCompleteness_DuplicateHeadings(t *testing.T) {
	// First occurrence wins
	content := `# test

## summary
First summary.

## how to test
Test instructions.

## summary
Second summary (should be ignored).
`
	result := CheckCompleteness(content)
	if !result.Complete {
		t.Error("should be complete - first occurrence of summary has content")
	}
}

func TestCheckCompleteness_TripleLevelHeadingsIncluded(t *testing.T) {
	// ### headings should be included in parent section content, not treated as boundaries
	content := `# my-feature

## summary
Overview.

### subsection
More details that are part of the summary.

## how to test
Instructions.

### detailed steps
1. Step one
2. Step two
`
	result := CheckCompleteness(content)
	if !result.Complete {
		t.Error("should be complete - ### headings don't create section boundaries")
	}
}

func TestNormalizeHeading(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Summary", "summary"},
		{"SUMMARY", "summary"},
		{"  summary  ", "summary"},
		{"summary:", "summary"},
		{"summary.", "summary"},
		{"summary-", "summary"},
		{"summary:..", "summary"},
		{"How To Test", "how to test"},
		{"how  to   test", "how to test"},
		{"  How  To  Test:  ", "how to test"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeHeading(tt.input)
			if got != tt.want {
				t.Errorf("normalizeHeading(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveAlias(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"summary", "summary"},
		{"overview", "summary"},
		{"how to test", "how to test"},
		{"how-to-test", "how to test"},
		{"testing", "how to test"},
		{"tests", "how to test"},
		{"unknown", "unknown"}, // no alias, returns input
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := resolveAlias(tt.input)
			if got != tt.want {
				t.Errorf("resolveAlias(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCheckCompleteness_RealWorldTemplate(t *testing.T) {
	// Test against the actual template format from worktree.go
	// NOTE: Per spec, any non-whitespace content counts as complete.
	// The template placeholder text like "- what changed" IS non-whitespace content,
	// so the template itself passes the completeness check.
	template := `# feature-name

runner: read ` + "`" + `.agency/INSTRUCTIONS.md` + "`" + ` before starting.

## summary
- what changed (high level)
- why (intent)

## scope
- completed
- explicitly not done / deferred

## decisions
- important choices + rationale
- tradeoffs

## deviations
- where it diverged from spec + why

## problems encountered
- failing tests, tricky bugs, constraints

## how to test
- exact commands
- expected output

## review notes
- files deserving scrutiny
- potential risks

## follow-ups
- blockers or questions
`
	// Template has placeholder content which is non-whitespace, so it's "complete"
	result := CheckCompleteness(template)
	if !result.Complete {
		t.Errorf("template with placeholder content should be complete per spec, missing: %v", result.MissingSections)
	}

	// Test truly empty sections
	emptyTemplate := `# feature-name

## summary

## how to test

`
	result = CheckCompleteness(emptyTemplate)
	if result.Complete {
		t.Error("truly empty template should not be complete")
	}
	if len(result.MissingSections) != 2 {
		t.Errorf("expected 2 missing sections, got: %v", result.MissingSections)
	}
}
