package s1gates

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

func TestParseIssueMap_DeterministicIssueCounts(t *testing.T) {
	t.Parallel()

	content := `# Issue Map

## S1 Platform Hardening Gates

1. ` + "`docs/issues/alpha.md`" + `
2. ` + "`docs/issues/beta.md`" + `

## S2 Daemon Read Convergence

1. ` + "`docs/issues/gamma.md`" + `
2. ` + "`docs/issues/alpha.md`" + `

## S3 Chat Control Plane

1. ` + "`docs/issues/delta.md`" + `
`

	result, err := ParseIssueMap(content)
	require.NoError(t, err)

	// Unique paths in encounter order.
	assert.Equal(t, []string{
		"docs/issues/alpha.md",
		"docs/issues/beta.md",
		"docs/issues/gamma.md",
		"docs/issues/delta.md",
	}, result.Paths)

	// Occurrence counts.
	assert.Equal(t, 2, result.Counts["docs/issues/alpha.md"])
	assert.Equal(t, 1, result.Counts["docs/issues/beta.md"])
	assert.Equal(t, 1, result.Counts["docs/issues/gamma.md"])
	assert.Equal(t, 1, result.Counts["docs/issues/delta.md"])
}

func TestParseIssueMap_MalformedContentReturnsEGateSetInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{"empty content", ""},
		{"no H2 sections", "# Title\n\nSome text without sections.\n"},
		{"H2 but no numbered items", "## Section\n\nJust prose here.\n"},
		{"numbered items but no backtick paths", "## Section\n\n1. plain text item\n2. another item\n"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ParseIssueMap(tt.content)
			assert.Nil(t, result)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateSetInvalid, agencyerrors.GetCode(err))
		})
	}
}

func TestParseIssueMap_IgnoresFencedContent(t *testing.T) {
	t.Parallel()

	content := `# Issue Map

## S1 Gates

1. ` + "`docs/issues/real.md`" + `

` + "```" + `
1. ` + "`docs/issues/fenced.md`" + `
` + "```" + `

2. ` + "`docs/issues/also-real.md`" + `
`

	result, err := ParseIssueMap(content)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"docs/issues/real.md",
		"docs/issues/also-real.md",
	}, result.Paths)
	assert.Equal(t, 0, result.Counts["docs/issues/fenced.md"])
}
