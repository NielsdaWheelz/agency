package core

import "testing"

func TestSlugify_Table(t *testing.T) {
	tests := []struct {
		name   string
		title  string
		maxLen int
		expect string
	}{
		{"hello world", "  Hello, World!!  ", 30, "hello-world"},
		{"all hyphens and underscores", "---___---", 30, "untitled"},
		{"spaces become hyphens", "a  b   c", 30, "a-b-c"},
		{"underscores become hyphens", "a__b__c", 30, "a-b-c"},
		{"emoji stripped", "a🥴b", 30, "ab"},
		{"empty string", "", 30, "untitled"},
		{"only spaces", "   ", 30, "untitled"},
		{"only special chars", "!@#$%^&*()", 30, "untitled"},
		{"numbers preserved", "test123", 30, "test123"},
		{"mixed case", "TeSt CaSe", 30, "test-case"},
		{"leading trailing hyphens stripped", "---test---", 30, "test"},
		{"tabs and newlines", "a\tb\nc", 30, "a-b-c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slugify(tt.title, tt.maxLen)
			if got != tt.expect {
				t.Errorf("Slugify(%q, %d) = %q, want %q", tt.title, tt.maxLen, got, tt.expect)
			}
		})
	}
}

func TestSlugify_Truncation(t *testing.T) {
	// Long input that needs truncation
	longTitle := "abcdefghijklmnopqrstuvwxyz0123456789"
	got := Slugify(longTitle, 30)

	if len(got) > 30 {
		t.Errorf("Slugify should truncate to maxLen=30, got len=%d: %q", len(got), got)
	}

	// Should be "abcdefghijklmnopqrstuvwxyz0123" (first 30 chars)
	expected := "abcdefghijklmnopqrstuvwxyz0123"
	if got != expected {
		t.Errorf("Slugify(%q, 30) = %q, want %q", longTitle, got, expected)
	}
}

func TestSlugify_TruncationDoesNotEndWithHyphen(t *testing.T) {
	// This title will have a hyphen right at position 30 after cleanup
	title := "this is a test title for truncation"
	got := Slugify(title, 30)

	if len(got) > 30 {
		t.Errorf("Slugify should truncate to maxLen=30, got len=%d: %q", len(got), got)
	}

	// Should not end with hyphen
	if len(got) > 0 && got[len(got)-1] == '-' {
		t.Errorf("Slugify result should not end with hyphen: %q", got)
	}

	// Should not start with hyphen
	if len(got) > 0 && got[0] == '-' {
		t.Errorf("Slugify result should not start with hyphen: %q", got)
	}
}

func TestSlugify_MaxLenZeroOrNegative(t *testing.T) {
	tests := []struct {
		name   string
		maxLen int
	}{
		{"zero", 0},
		{"negative", -1},
		{"very negative", -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slugify("test", tt.maxLen)
			if got != "untitled" {
				t.Errorf("Slugify(\"test\", %d) = %q, want \"untitled\"", tt.maxLen, got)
			}
		})
	}
}

func TestSlugify_EdgeCases(t *testing.T) {
	// Edge case: truncation at hyphen position
	// "a-b-c-d-e-f-g" with maxLen=5 should become "a-b-c" after trim
	got := Slugify("a b c d e f g", 5)
	if len(got) > 5 {
		t.Errorf("Slugify should truncate, got len=%d: %q", len(got), got)
	}
	// After truncation to 5 chars and retrim, should be "a-b-c"
	// Actually "a-b-c-d-e-f-g" truncated to 5 is "a-b-c", then trim hyphens leaves "a-b-c"
	// Wait, "a-b-c" is exactly 5 chars: a - b - c
	if got[len(got)-1] == '-' {
		t.Errorf("Slugify result should not end with hyphen: %q", got)
	}
}
