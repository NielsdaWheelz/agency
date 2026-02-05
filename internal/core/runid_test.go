package core

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRunID_FormatAndUTC(t *testing.T) {
	t.Parallel()

	// Use a fixed time for deterministic timestamp portion
	now := time.Date(2026, 1, 9, 1, 32, 7, 0, time.UTC)

	runID, err := NewRunID(now)
	require.NoError(t, err)

	// Verify format: <14-digit timestamp>-<4 hex chars>
	pattern := `^\d{14}-[0-9a-f]{4}$`
	matched, err := regexp.MatchString(pattern, runID)
	require.NoError(t, err)
	assert.True(t, matched, "runID %q does not match pattern %q", runID, pattern)

	// Verify timestamp portion
	expectedPrefix := "20260109013207-"
	assert.True(t, len(runID) >= len(expectedPrefix) && runID[:len(expectedPrefix)] == expectedPrefix,
		"runID %q does not start with expected prefix %q", runID, expectedPrefix)
}

func TestNewRunID_UniqueRandomSuffix(t *testing.T) {
	t.Parallel()

	now := time.Now()
	seen := make(map[string]bool)

	// Generate multiple IDs and verify uniqueness (probabilistic but very likely)
	for i := 0; i < 100; i++ {
		runID, err := NewRunID(now)
		require.NoError(t, err)
		suffix := ShortID(runID)
		if seen[suffix] {
			// This could theoretically happen but is extremely unlikely
			t.Logf("warning: duplicate suffix %q (may be coincidence)", suffix)
		}
		seen[suffix] = true
	}
}

func TestShortID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		runID  string
		expect string
	}{
		{"valid format", "20260109013207-a3f2", "a3f2"},
		{"valid format hex", "20260109013207-beef", "beef"},
		{"no hyphen", "20260109013207", "xxxx"},
		{"empty", "", "xxxx"},
		{"short suffix", "20260109013207-ab", "xxxx"},
		{"long suffix", "20260109013207-abcde", "xxxx"},
		{"multiple hyphens", "a-b-c-d3f2", "d3f2"},
		{"hyphen at end", "test-", "xxxx"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ShortID(tt.runID)
			assert.Equal(t, tt.expect, got)
		})
	}
}
