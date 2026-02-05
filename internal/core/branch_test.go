package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBranchName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		testName string
		name     string
		runID    string
		expect   string
	}{
		{
			testName: "basic",
			name:     "my-feature",
			runID:    "20260109013207-a3f2",
			expect:   "agency/my-feature-a3f2",
		},
		{
			testName: "simple name",
			name:     "fix-bug",
			runID:    "20260109013207-beef",
			expect:   "agency/fix-bug-beef",
		},
		{
			testName: "name with digits",
			name:     "fix-bug-123",
			runID:    "20260109013207-1234",
			expect:   "agency/fix-bug-123-1234",
		},
		{
			testName: "short name",
			name:     "ab",
			runID:    "20260109013207-abcd",
			expect:   "agency/ab-abcd",
		},
		{
			testName: "invalid runID format",
			name:     "test",
			runID:    "invalid",
			expect:   "agency/test-xxxx",
		},
		{
			testName: "long name",
			name:     "this-is-a-very-long-feature-name",
			runID:    "20260109013207-ffff",
			expect:   "agency/this-is-a-very-long-feature-name-ffff",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			got := BranchName(tt.name, tt.runID)
			assert.Equal(t, tt.expect, got)
		})
	}
}
