package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSlugify_Table(t *testing.T) {
	t.Parallel()

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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Slugify(tt.title, tt.maxLen)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestSlugify_Truncation(t *testing.T) {
	t.Parallel()

	// Long input that needs truncation
	longTitle := "abcdefghijklmnopqrstuvwxyz0123456789"
	got := Slugify(longTitle, 30)

	assert.True(t, len(got) <= 30, "Slugify should truncate to maxLen=30, got len=%d: %q", len(got), got)

	// Should be "abcdefghijklmnopqrstuvwxyz0123" (first 30 chars)
	expected := "abcdefghijklmnopqrstuvwxyz0123"
	assert.Equal(t, expected, got)
}

func TestSlugify_TruncationDoesNotEndWithHyphen(t *testing.T) {
	t.Parallel()

	// This title will have a hyphen right at position 30 after cleanup
	title := "this is a test title for truncation"
	got := Slugify(title, 30)

	assert.True(t, len(got) <= 30, "Slugify should truncate to maxLen=30, got len=%d: %q", len(got), got)

	// Should not end with hyphen
	if len(got) > 0 {
		assert.NotEqual(t, byte('-'), got[len(got)-1], "Slugify result should not end with hyphen: %q", got)
	}

	// Should not start with hyphen
	if len(got) > 0 {
		assert.NotEqual(t, byte('-'), got[0], "Slugify result should not start with hyphen: %q", got)
	}
}

func TestSlugify_MaxLenZeroOrNegative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		maxLen int
	}{
		{"zero", 0},
		{"negative", -1},
		{"very negative", -100},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Slugify("test", tt.maxLen)
			assert.Equal(t, "untitled", got)
		})
	}
}

func TestSlugify_EdgeCases(t *testing.T) {
	t.Parallel()

	// Edge case: truncation at hyphen position
	// "a-b-c-d-e-f-g" with maxLen=5 should become "a-b-c" after trim
	got := Slugify("a b c d e f g", 5)
	assert.True(t, len(got) <= 5, "Slugify should truncate, got len=%d: %q", len(got), got)
	// After truncation to 5 chars and retrim, should be "a-b-c"
	// Actually "a-b-c-d-e-f-g" truncated to 5 is "a-b-c", then trim hyphens leaves "a-b-c"
	// Wait, "a-b-c" is exactly 5 chars: a - b - c
	assert.NotEqual(t, byte('-'), got[len(got)-1], "Slugify result should not end with hyphen: %q", got)
}
