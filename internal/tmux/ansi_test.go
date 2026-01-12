package tmux

import (
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "plain text",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "plain text with newlines",
			input:    "line1\nline2\nline3",
			expected: "line1\nline2\nline3",
		},
		{
			name:     "color codes red text",
			input:    "\x1b[31mred text\x1b[0m",
			expected: "red text",
		},
		{
			name:     "color codes green text",
			input:    "\x1b[32mgreen\x1b[0m normal",
			expected: "green normal",
		},
		{
			name:     "bold and colors",
			input:    "\x1b[1;33mbold yellow\x1b[0m",
			expected: "bold yellow",
		},
		{
			name:     "256 color codes",
			input:    "\x1b[38;5;196mred256\x1b[0m",
			expected: "red256",
		},
		{
			name:     "RGB color codes",
			input:    "\x1b[38;2;255;0;0mrgb red\x1b[0m",
			expected: "rgb red",
		},
		{
			name:     "cursor movement up",
			input:    "line1\x1b[Aup",
			expected: "line1up",
		},
		{
			name:     "cursor movement to column",
			input:    "text\x1b[10Gat column 10",
			expected: "textat column 10",
		},
		{
			name:     "clear line",
			input:    "partial\x1b[2Kcleared",
			expected: "partialcleared",
		},
		{
			name:     "clear screen",
			input:    "before\x1b[2Jafter",
			expected: "beforeafter",
		},
		{
			name:     "OSC title set (BEL terminated)",
			input:    "\x1b]0;Window Title\x07text after",
			expected: "text after",
		},
		{
			name:     "OSC title set (ST terminated)",
			input:    "\x1b]0;Window Title\x1b\\text after",
			expected: "text after",
		},
		{
			name:     "mixed escapes and text",
			input:    "\x1b[32m$ \x1b[0mls -la\x1b[0m\nfile1.txt\n\x1b[34mdir/\x1b[0m",
			expected: "$ ls -la\nfile1.txt\ndir/",
		},
		{
			name:     "save and restore cursor",
			input:    "\x1b7saved\x1b8restored",
			expected: "savedrestored",
		},
		{
			name:     "alternate screen buffer",
			input:    "\x1b[?1049hcontent\x1b[?1049l",
			expected: "content",
		},
		{
			name:     "scroll region",
			input:    "\x1b[1;24rscrolling\x1b[r",
			expected: "scrolling",
		},
		{
			name:     "single shift",
			input:    "text\x1bNmore",
			expected: "textmore",
		},
		{
			name:     "multiple consecutive escapes",
			input:    "\x1b[31m\x1b[1m\x1b[4mbold red underline\x1b[0m",
			expected: "bold red underline",
		},
		{
			name:     "real tmux output example",
			input:    "\x1b[?1049h\x1b[22;0;0t\x1b[?1h\x1b=\x1b[?2004h$ echo hello\r\nhello\r\n$",
			expected: "$ echo hello\r\nhello\r\n$",
		},
		{
			name:     "malformed escape at end",
			input:    "text\x1b",
			expected: "text",
		},
		{
			name:     "partial CSI at end",
			input:    "text\x1b[",
			expected: "text",
		},
		{
			name:     "only escape codes",
			input:    "\x1b[31m\x1b[0m",
			expected: "",
		},
		{
			name:     "hyperlink escape (OSC 8)",
			input:    "\x1b]8;;https://example.com\x1b\\link text\x1b]8;;\x1b\\",
			expected: "link text",
		},
		{
			name:     "unicode text with escapes",
			input:    "\x1b[32m你好\x1b[0m 世界",
			expected: "你好 世界",
		},
		{
			name:     "emoji with escapes",
			input:    "\x1b[33m🚀\x1b[0m launch",
			expected: "🚀 launch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripANSI(tt.input)
			if result != tt.expected {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestStripANSI_NoPanic(t *testing.T) {
	// Test that StripANSI never panics, even with weird input
	testCases := []string{
		"",
		"\x1b",
		"\x1b[",
		"\x1b[31",
		"\x1b[31;",
		"\x1b]",
		"\x1b]8",
		"\x1b]8;",
		string([]byte{0x1b, 0x00}),
		string([]byte{0x1b, 0xff}),
		strings.Repeat("\x1b[31m", 1000),
		strings.Repeat("a", 10000),
		strings.Repeat("\x1b[", 1000),
	}

	for i, input := range testCases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("StripANSI panicked on input %d: %v", i, r)
				}
			}()
			_ = StripANSI(input)
		}()
	}
}

func TestStripANSI_NoEscapeBytes(t *testing.T) {
	// Test that output contains no ESC bytes
	inputs := []string{
		"\x1b[31mred\x1b[0m",
		"\x1b]0;title\x07",
		"\x1b[?1049h\x1b[?1049l",
		"normal\x1b[1mbold\x1b[0m",
	}

	for _, input := range inputs {
		result := StripANSI(input)
		if strings.Contains(result, "\x1b") {
			t.Errorf("StripANSI(%q) still contains ESC byte: %q", input, result)
		}
	}
}
