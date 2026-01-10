package core

import "testing"

func TestBranchName(t *testing.T) {
	tests := []struct {
		name   string
		title  string
		runID  string
		expect string
	}{
		{
			name:   "basic",
			title:  "Test Run",
			runID:  "20260109013207-a3f2",
			expect: "agency/test-run-a3f2",
		},
		{
			name:   "empty title",
			title:  "",
			runID:  "20260109013207-beef",
			expect: "agency/untitled-beef",
		},
		{
			name:   "long title truncated",
			title:  "this is a very long title that exceeds thirty characters",
			runID:  "20260109013207-1234",
			expect: "agency/this-is-a-very-long-title-that-1234",
		},
		{
			name:   "special chars in title",
			title:  "Fix bug #123 (urgent!!!)",
			runID:  "20260109013207-abcd",
			expect: "agency/fix-bug-123-urgent-abcd",
		},
		{
			name:   "invalid runID format",
			title:  "test",
			runID:  "invalid",
			expect: "agency/test-xxxx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BranchName(tt.title, tt.runID)
			if got != tt.expect {
				t.Errorf("BranchName(%q, %q) = %q, want %q", tt.title, tt.runID, got, tt.expect)
			}
		})
	}
}
