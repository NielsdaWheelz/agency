// Package errors provides error formatting for agency CLI output.
package errors

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// PrintOptions controls error output formatting.
type PrintOptions struct {
	// Verbose enables detailed error output with more context keys and longer tails.
	Verbose bool

	// Tailer provides output tail lines for verify failures.
	// If nil, PrintWithOptions reads verify.log directly (bounded I/O).
	Tailer func(logPath string, maxLines int) ([]string, error)
}

// Context key whitelist (default mode, in order per spec)
var defaultContextKeys = []string{
	"op",
	"run_id",
	"repo",
	"worktree",
	"script",
	"command",
	"branch",
	"parent",
	"pr",
	"exit_code",
	"duration",
	"log",
	"record",
}

// Additional context keys for verbose mode
var verboseContextKeys = []string{
	"op",
	"run_id",
	"repo_id",
	"repo",
	"worktree",
	"worktree_path",
	"script",
	"command",
	"branch",
	"parent",
	"parent_branch",
	"pr",
	"pr_number",
	"pr_url",
	"exit_code",
	"duration",
	"duration_ms",
	"log",
	"record",
	"signal",
	"timed_out",
	"cancelled",
	"origin_url",
	"host",
	"hint",
}

// Truncation limits per spec
const (
	defaultMaxLines = 20
	defaultMaxChars = 8 * 1024 // 8 KB
	verboseMaxLines = 100
	verboseMaxChars = 64 * 1024 // 64 KB

	maxValueLen      = 256 // Max chars for single-line context values
	maxExtraValueLen = 128 // Max chars for extra section values
	maxOutputLineLen = 512 // Max chars per line in output blocks
)

// Format formats an error for display without I/O.
// This is a pure function - it never reads files or performs network I/O.
// Returns the formatted string ready for printing.
func Format(err error, opts PrintOptions) string {
	if err == nil {
		return ""
	}

	var sb strings.Builder

	ae, isAgency := AsAgencyError(err)
	if !isAgency {
		// Fallback for non-AgencyError errors
		sb.WriteString(err.Error())
		sb.WriteString("\n")
		return sb.String()
	}

	// Line 1: error_code
	sb.WriteString("error_code: ")
	sb.WriteString(string(ae.Code))
	sb.WriteString("\n")

	// Line 2: message
	sb.WriteString(ae.Msg)
	sb.WriteString("\n")

	// Blank line before context
	sb.WriteString("\n")

	// Context block
	contextKeys := defaultContextKeys
	if opts.Verbose {
		contextKeys = verboseContextKeys
	}

	// Build set of printed keys
	printedKeys := make(map[string]bool)

	// Print context keys in order
	for _, key := range contextKeys {
		if ae.Details == nil {
			continue
		}
		val, ok := ae.Details[key]
		if !ok || val == "" {
			continue
		}
		// Skip hint - printed separately at the end
		if key == "hint" {
			continue
		}
		printedKeys[key] = true
		sb.WriteString(key)
		sb.WriteString(": ")
		sb.WriteString(sanitizeValue(val, maxValueLen))
		sb.WriteString("\n")
	}

	// In verbose mode, print extra keys under extra: section
	if opts.Verbose && ae.Details != nil {
		var extraKeys []string
		for key := range ae.Details {
			if !printedKeys[key] && key != "hint" && key != "stderr" {
				extraKeys = append(extraKeys, key)
			}
		}
		if len(extraKeys) > 0 {
			sort.Strings(extraKeys)
			sb.WriteString("\nextra:\n")
			for _, key := range extraKeys {
				val := ae.Details[key]
				if val == "" {
					continue
				}
				sb.WriteString("  ")
				sb.WriteString(key)
				sb.WriteString(": ")
				sb.WriteString(sanitizeValue(val, maxExtraValueLen))
				sb.WriteString("\n")
			}
		}
	}

	// Hint line (if present)
	if ae.Details != nil {
		if hint, ok := ae.Details["hint"]; ok && hint != "" {
			sb.WriteString("\nhint: ")
			sb.WriteString(hint)
			sb.WriteString("\n")
		}
	}

	// Try lines (suggestions for common errors)
	tryLines := deriveTryLines(ae)
	for _, try := range tryLines {
		sb.WriteString("try: ")
		sb.WriteString(try)
		sb.WriteString("\n")
	}

	return sb.String()
}

// PrintWithOptions writes a formatted error to w with the given options.
// May perform bounded I/O to read verify logs for E_SCRIPT_FAILED errors.
func PrintWithOptions(w io.Writer, err error, opts PrintOptions) {
	if err == nil {
		return
	}

	// Get the base formatted output
	output := Format(err, opts)

	ae, isAgency := AsAgencyError(err)

	// Check if this is a verify failure that needs output tail
	if isAgency && isVerifyFailure(ae) {
		logPath := ""
		if ae.Details != nil {
			logPath = ae.Details["log"]
		}
		if logPath != "" {
			maxLines := defaultMaxLines
			maxChars := defaultMaxChars
			if opts.Verbose {
				maxLines = verboseMaxLines
				maxChars = verboseMaxChars
			}

			var lines []string
			var tailErr error

			if opts.Tailer != nil {
				lines, tailErr = opts.Tailer(logPath, maxLines)
			} else {
				lines, tailErr = readTail(logPath, maxLines, maxChars)
			}

			if tailErr == nil && len(lines) > 0 {
				// Insert output block before hint line
				output = insertOutputBlock(output, lines, maxLines)
			}
		}
	}

	_, _ = io.WriteString(w, output)
}

// sanitizeValue sanitizes a value for single-line context output.
// - Trims trailing whitespace first
// - Normalizes CRLF to LF
// - Replaces newlines with literal \n
// - Truncates to maxLen chars
func sanitizeValue(val string, maxLen int) string {
	// Trim trailing whitespace first (before escaping)
	val = strings.TrimRight(val, " \t\r\n")

	// Normalize CRLF to LF
	val = strings.ReplaceAll(val, "\r\n", "\n")

	// Replace newlines with literal \n
	val = strings.ReplaceAll(val, "\n", "\\n")

	// Truncate if too long
	if len(val) > maxLen {
		return val[:maxLen] + "…"
	}

	return val
}

// isVerifyFailure checks if an error is a verify failure per spec:
// Code == E_SCRIPT_FAILED AND (log ends with verify.log OR script ends with agency_verify.sh)
func isVerifyFailure(ae *AgencyError) bool {
	if ae.Code != EScriptFailed {
		return false
	}
	if ae.Details == nil {
		return false
	}

	logPath := ae.Details["log"]
	scriptPath := ae.Details["script"]

	if strings.HasSuffix(logPath, "verify.log") {
		return true
	}
	if strings.HasSuffix(scriptPath, "agency_verify.sh") {
		return true
	}

	return false
}

// readTail reads the last maxLines lines from a file, up to maxChars total.
// Returns the lines (without trailing newlines) and any error.
func readTail(path string, maxLines, maxChars int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	// Read file in chunks from the end
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := stat.Size()
	if size == 0 {
		return nil, nil
	}

	// Limit how much we read
	readSize := int64(maxChars)
	if readSize > size {
		readSize = size
	}

	// Seek to the position we want to start reading
	_, err = f.Seek(size-readSize, 0)
	if err != nil {
		return nil, err
	}

	// Read lines
	var allLines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Truncate long lines
		if len(line) > maxOutputLineLen {
			line = line[:maxOutputLineLen] + "…"
		}
		// Trim trailing whitespace per line
		line = strings.TrimRight(line, " \t\r")
		allLines = append(allLines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Return last maxLines
	if len(allLines) > maxLines {
		return allLines[len(allLines)-maxLines:], nil
	}

	return allLines, nil
}

// insertOutputBlock inserts the output tail block before the hint line in the formatted output.
func insertOutputBlock(output string, lines []string, maxLines int) string {
	// Build the output block
	var block strings.Builder
	if len(lines) >= maxLines {
		block.WriteString(fmt.Sprintf("\noutput (last %d lines):\n", len(lines)))
	} else {
		block.WriteString(fmt.Sprintf("\noutput (%d lines):\n", len(lines)))
	}
	for _, line := range lines {
		block.WriteString("  ")
		block.WriteString(line)
		block.WriteString("\n")
	}

	// Find hint line and insert before it
	hintIdx := strings.Index(output, "\nhint: ")
	if hintIdx >= 0 {
		return output[:hintIdx] + block.String() + output[hintIdx:]
	}

	// Find try line and insert before it
	tryIdx := strings.Index(output, "\ntry: ")
	if tryIdx >= 0 {
		return output[:tryIdx] + block.String() + output[tryIdx:]
	}

	// No hint or try line, append at end
	return output + block.String()
}

// deriveTryLines returns actionable suggestions based on error code.
func deriveTryLines(ae *AgencyError) []string {
	if ae == nil {
		return nil
	}

	var lines []string

	switch ae.Code {
	case ESessionNotFound:
		if ae.Details != nil {
			if runID := ae.Details["run_id"]; runID != "" {
				lines = append(lines, fmt.Sprintf("agency resume %s", runID))
			}
		}
	case EGhNotAuthenticated:
		lines = append(lines, "gh auth login")
	case EScriptFailed:
		// Check if it's a verify failure
		if isVerifyFailure(ae) {
			if ae.Details != nil {
				if runID := ae.Details["run_id"]; runID != "" {
					lines = append(lines, fmt.Sprintf("agency verify %s", runID))
				}
			}
		}
	case ERemoteOutOfDate:
		if ae.Details != nil {
			if runID := ae.Details["run_id"]; runID != "" {
				lines = append(lines, fmt.Sprintf("agency push %s", runID))
			}
		}
	case ENoPR:
		if ae.Details != nil {
			if runID := ae.Details["run_id"]; runID != "" {
				lines = append(lines, fmt.Sprintf("agency push %s", runID))
			}
		}
	}

	return lines
}

// FormatHint formats a hint for output.
// If hint already starts with "hint:", returns as-is.
// Otherwise prepends "hint: ".
func FormatHint(hint string) string {
	if hint == "" {
		return ""
	}
	if strings.HasPrefix(hint, "hint:") {
		return hint
	}
	return "hint: " + hint
}

// GetHint extracts the hint from an error's details, if present.
func GetHint(err error) string {
	ae, ok := AsAgencyError(err)
	if !ok || ae.Details == nil {
		return ""
	}
	return ae.Details["hint"]
}
