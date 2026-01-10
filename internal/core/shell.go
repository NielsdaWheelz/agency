package core

import "strings"

// ShellEscapePosix returns a single shell token using single-quote strategy,
// including surrounding single quotes.
// example: abc -> 'abc'
// example: a'b -> 'a'"'"'b'
// example: "" -> ''
func ShellEscapePosix(s string) string {
	if s == "" {
		return "''"
	}
	// Replace each single quote with: end quote, escaped single quote, start quote
	// 'a'b' => 'a'"'"'b'
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}

// BuildRunnerShellScript returns the shell *script* string to pass as argv to `sh -lc`.
// It must:
// - cd into worktreePath safely (using ShellEscapePosix)
// - then exec runnerCmd verbatim (runnerCmd is a shell snippet; user responsibility)
// Example output:
//
//	"cd '...path...' && exec <runnerCmd>"
//
// Note: runnerCmd is not trimmed or validated; empty is allowed here.
func BuildRunnerShellScript(worktreePath, runnerCmd string) string {
	escapedPath := ShellEscapePosix(worktreePath)
	return "cd " + escapedPath + " && exec " + runnerCmd
}
