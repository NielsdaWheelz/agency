package core

import "strings"

// ShellEscapePosix returns a single shell token using single-quote strategy,
// including surrounding single quotes.
// example: abc -> 'abc'
// example: a'b -> 'a'"'"'b'
// example: "" -> ”
func ShellEscapePosix(s string) string {
	if s == "" {
		return "''"
	}
	// Replace each single quote with: end quote, escaped single quote, start quote
	// 'a'b' => 'a'"'"'b'
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
