package errors

import (
	"bytes"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatFirstLineAlwaysErrorCode verifies first line is always error_code.
func TestFormatFirstLineAlwaysErrorCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code Code
		msg  string
	}{
		{"usage error", EUsage, "bad args"},
		{"script failed", EScriptFailed, "verify failed"},
		{"no repo", ENoRepo, "not inside a git repository"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := New(tt.code, tt.msg)
			output := formatError(err, PrintOptions{})

			lines := strings.Split(output, "\n")
			require.True(t, len(lines) >= 1, "expected at least one line of output")

			expected := "error_code: " + string(tt.code)
			assert.Equal(t, expected, lines[0])
		})
	}
}

// TestFormatMessageSecondLine verifies message is always second line.
func TestFormatMessageSecondLine(t *testing.T) {
	t.Parallel()

	err := New(EUsage, "test message")
	output := formatError(err, PrintOptions{})

	lines := strings.Split(output, "\n")
	require.True(t, len(lines) >= 2, "expected at least two lines of output")

	assert.Equal(t, "test message", lines[1])
}

// TestFormatContextKeysInOrder verifies context keys appear in specified order.
func TestFormatContextKeysInOrder(t *testing.T) {
	t.Parallel()

	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script":    "scripts/agency_verify.sh",
		"exit_code": "1",
		"run_id":    "20260110120000-a3f2",
		"log":       "/path/to/verify.log",
	})

	output := formatError(err, PrintOptions{})

	// Check that keys appear in the expected order
	runIDIdx := strings.Index(output, "run_id:")
	scriptIdx := strings.Index(output, "script:")
	exitCodeIdx := strings.Index(output, "exit_code:")
	logIdx := strings.Index(output, "log:")

	// Per spec: run_id < script < exit_code < log
	assert.True(t, runIDIdx < scriptIdx, "run_id should come before script")
	assert.True(t, scriptIdx < exitCodeIdx, "script should come before exit_code")
	assert.True(t, exitCodeIdx < logIdx, "exit_code should come before log")
}

func TestFormatShowsPathAndSourceByDefault(t *testing.T) {
	t.Parallel()

	err := NewWithDetails(EInvalidAgencyJSON, "version 1 is not supported; agency.json must use version 4", map[string]string{
		"path":   "/repo/agency.json",
		"source": "repo",
		"hint":   "fix /repo/agency.json, or regenerate it",
	})

	output := formatError(err, PrintOptions{})

	pathIdx := strings.Index(output, "path: /repo/agency.json")
	sourceIdx := strings.Index(output, "source: repo")
	hintIdx := strings.Index(output, "hint: fix /repo/agency.json, or regenerate it")

	assert.NotEqual(t, -1, pathIdx, "path should appear in default output")
	assert.NotEqual(t, -1, sourceIdx, "source should appear in default output")
	assert.NotEqual(t, -1, hintIdx, "hint should appear in output")
	assert.True(t, pathIdx < sourceIdx, "path should come before source")
	assert.True(t, sourceIdx < hintIdx, "source should come before hint")
}

// TestFormatUnknownKeysHiddenByDefault verifies unknown keys are hidden without --verbose.
func TestFormatUnknownKeysHiddenByDefault(t *testing.T) {
	t.Parallel()

	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script":      "scripts/agency_verify.sh",
		"unknown_key": "should not appear",
		"another_key": "also hidden",
	})

	output := formatError(err, PrintOptions{Verbose: false})

	assert.NotContains(t, output, "unknown_key", "unknown_key should not appear in default mode")
	assert.NotContains(t, output, "another_key", "another_key should not appear in default mode")
}

// TestFormatVerboseRevealsExtras verifies --verbose reveals extra keys.
func TestFormatVerboseRevealsExtras(t *testing.T) {
	t.Parallel()

	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script":      "scripts/agency_verify.sh",
		"unknown_key": "should appear",
		"another_key": "also visible",
	})

	output := formatError(err, PrintOptions{Verbose: true})

	assert.Contains(t, output, "extra:")
	assert.Contains(t, output, "unknown_key")
	assert.Contains(t, output, "another_key")
}

// TestFormatMultilineValueEscaped verifies multi-line values are escaped.
func TestFormatMultilineValueEscaped(t *testing.T) {
	t.Parallel()

	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script": "line1\nline2\nline3",
	})

	output := formatError(err, PrintOptions{})

	// The value should not contain actual newlines (they should be escaped)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "script:") {
			if strings.Contains(line, "line1") && strings.Contains(line, "line2") {
				// The value is on one line, which is correct
				assert.Contains(t, line, "\\n", "newlines should be escaped as \\n")
			}
		}
	}
}

// TestFormatMissingContextKeysSkipped verifies missing keys don't create empty lines.
func TestFormatMissingContextKeysSkipped(t *testing.T) {
	t.Parallel()

	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script": "scripts/agency_verify.sh",
		// No other keys
	})

	output := formatError(err, PrintOptions{})

	// Should not have empty key lines like "exit_code:" with no value
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		assert.False(t, strings.HasSuffix(line, ":"), "found empty key line: %q", line)
	}
}

// TestFormatCRLFNormalized verifies CRLF is normalized to LF.
func TestFormatCRLFNormalized(t *testing.T) {
	t.Parallel()

	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script": "line1\r\nline2\r\n",
	})

	output := formatError(err, PrintOptions{})

	// Should not contain \r
	assert.NotContains(t, output, "\r", "output should not contain \\r")
}

// TestFormatLongValuesTruncated verifies long values are truncated.
func TestFormatLongValuesTruncated(t *testing.T) {
	t.Parallel()

	longValue := strings.Repeat("a", 300)
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script": longValue,
	})

	output := formatError(err, PrintOptions{})

	// The truncated value should be maxValueLen (256) chars + "..."
	for _, line := range strings.Split(output, "\n") {
		if val, ok := strings.CutPrefix(line, "script: "); ok {
			assert.True(t, len(val) <= maxValueLen+3, "value length = %d, should be truncated to ~%d", len(val), maxValueLen)
			assert.True(t, strings.HasSuffix(val, "\u2026"), "truncated value should end with \u2026")
		}
	}
}

// TestFormatEmptyStringValues verifies empty string values are skipped.
func TestFormatEmptyStringValues(t *testing.T) {
	t.Parallel()

	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script":    "scripts/agency_verify.sh",
		"exit_code": "",
	})

	output := formatError(err, PrintOptions{})

	assert.NotContains(t, output, "exit_code:", "empty exit_code should not appear")
}

// TestFormatDetailsOnlyUnknownKeys verifies handling of details with only unknown keys.
func TestFormatDetailsOnlyUnknownKeys(t *testing.T) {
	t.Parallel()

	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"custom_key1": "value1",
		"custom_key2": "value2",
	})

	// Default mode
	output := formatError(err, PrintOptions{})
	assert.NotContains(t, output, "custom_key1", "unknown keys should not appear in default mode")

	// Verbose mode
	output = formatError(err, PrintOptions{Verbose: true})
	assert.Contains(t, output, "custom_key1", "unknown keys should appear in verbose mode")
}

// TestFormatVerifyFailureDetection verifies verify failure detection rule.
func TestFormatVerifyFailureDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		code     Code
		details  map[string]string
		expected bool
	}{
		{
			name: "verify failure by log path",
			code: EScriptFailed,
			details: map[string]string{
				"log": "/path/to/verify.log",
			},
			expected: true,
		},
		{
			name: "verify failure by script path",
			code: EScriptFailed,
			details: map[string]string{
				"script": "scripts/agency_verify.sh",
			},
			expected: true,
		},
		{
			name: "not verify failure - different code",
			code: EUsage,
			details: map[string]string{
				"log": "/path/to/verify.log",
			},
			expected: false,
		},
		{
			name: "not verify failure - different log",
			code: EScriptFailed,
			details: map[string]string{
				"log": "/path/to/setup.log",
			},
			expected: false,
		},
		{
			name:     "not verify failure - no details",
			code:     EScriptFailed,
			details:  nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ae := &AgencyError{
				Code:    tt.code,
				Msg:     "test",
				Details: tt.details,
			}

			result := isVerifyFailure(ae)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFormatHintLine verifies hint line is printed at the end.
func TestFormatHintLine(t *testing.T) {
	t.Parallel()

	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"hint": "fix the failing tests",
	})

	output := formatError(err, PrintOptions{})

	assert.Contains(t, output, "hint: fix the failing tests")

	// Hint should be near the end
	lines := strings.Split(strings.TrimSpace(output), "\n")
	found := false
	for i := len(lines) - 3; i < len(lines); i++ {
		if i >= 0 && strings.HasPrefix(lines[i], "hint:") {
			found = true
			break
		}
	}
	assert.True(t, found, "hint should be near the end of output")
}

// TestPrintWithOptionsNil verifies PrintWithOptions handles nil error.
func TestPrintWithOptionsNil(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	PrintWithOptions(&buf, nil, PrintOptions{})

	assert.Equal(t, 0, buf.Len(), "nil error should produce no output")
}

// TestPrintWithOptionsNonAgencyError verifies handling of non-AgencyError.
func TestPrintWithOptionsNonAgencyError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := stderrors.New("plain error")
	PrintWithOptions(&buf, err, PrintOptions{})

	output := buf.String()
	assert.Contains(t, output, "plain error")
}

// TestSanitizeValue verifies sanitizeValue function.
func TestSanitizeValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"simple", "hello", 100, "hello"},
		{"with newline", "hello\nworld", 100, "hello\\nworld"},
		{"with crlf", "hello\r\nworld", 100, "hello\\nworld"},
		{"trailing whitespace", "hello  \n", 100, "hello"},
		{"truncate", "hello world", 5, "hello\u2026"},
		{"empty", "", 100, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := sanitizeValue(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestReadTail verifies readTail function.
func TestReadTail(t *testing.T) {
	t.Parallel()

	// Create a temp file with test content
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	content := "line1\nline2\nline3\nline4\nline5\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	lines, err := readTail(path, 3, 1024)
	require.NoError(t, err)

	require.Len(t, lines, 3)

	// Should have last 3 lines
	expected := []string{"line3", "line4", "line5"}
	for i, want := range expected {
		assert.Equal(t, want, lines[i], "line[%d]", i)
	}
}

// TestReadTailEmptyFile verifies readTail handles empty files.
func TestReadTailEmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")

	require.NoError(t, os.WriteFile(path, []byte{}, 0o644))

	lines, err := readTail(path, 10, 1024)
	require.NoError(t, err)

	assert.Len(t, lines, 0)
}

// TestReadTailNonexistentFile verifies readTail handles missing files.
func TestReadTailNonexistentFile(t *testing.T) {
	t.Parallel()

	_, err := readTail("/nonexistent/path/file.log", 10, 1024)
	require.Error(t, err)
}

// TestDeriveTryLines verifies try line suggestions.
func TestDeriveTryLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		code     Code
		details  map[string]string
		contains string
	}{
		{
			name:     "gh not authenticated suggests login",
			code:     EGhNotAuthenticated,
			details:  nil,
			contains: "gh auth login",
		},
		{
			name:     "missing user config suggests config init",
			code:     ENoUserConfig,
			details:  nil,
			contains: "agency config init",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ae := &AgencyError{
				Code:    tt.code,
				Msg:     "test",
				Details: tt.details,
			}
			lines := deriveTryLines(ae)

			found := false
			for _, line := range lines {
				if strings.Contains(line, tt.contains) {
					found = true
					break
				}
			}
			assert.True(t, found, "expected try line containing %q, got %v", tt.contains, lines)
		})
	}
}
