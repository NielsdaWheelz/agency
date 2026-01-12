// Package tmux provides tmux integration for agency.
// This file implements ANSI escape code stripping.
package tmux

import "regexp"

// ansiEscapeRegex matches ANSI escape sequences including:
// - CSI sequences: ESC [ ... (parameters) ... (intermediate bytes) ... final byte
// - OSC sequences: ESC ] ... ST (where ST is ESC \ or BEL)
// - Single-character escapes: ESC followed by a single character
// - Other escape sequences
// - Lone ESC at end of string
//
// This is intentionally broad to catch all common terminal escape sequences.
var ansiEscapeRegex = regexp.MustCompile(
	// CSI sequences: ESC [ (params) (intermediate) final
	`\x1b\[[0-9;:<=>?]*[ -/]*[@-~]` +
		// OSC sequences: ESC ] ... (ST = ESC \ or BEL)
		`|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?` +
		// Single-char escapes (ESC followed by printable char)
		`|\x1b[@-_]` +
		// DCS, PM, APC sequences
		`|\x1b[PX^_][^\x1b]*\x1b\\` +
		// Remaining ESC sequences (catch-all for ESC + any char)
		`|\x1b.` +
		// Lone ESC at end of string or partial CSI
		`|\x1b\[?$`,
)

// StripANSI removes ANSI escape sequences from the input string.
// This is a pure function that never panics.
// It removes all ESC (\x1b) sequences including CSI, OSC, and other control sequences.
//
// The function is designed to be total and safe:
// - Empty input returns empty output
// - Input without escapes is returned unchanged
// - Malformed sequences are handled gracefully
func StripANSI(s string) string {
	if s == "" {
		return s
	}
	return ansiEscapeRegex.ReplaceAllString(s, "")
}
