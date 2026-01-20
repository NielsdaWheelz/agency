package errors

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrintSignatureUnchanged is a compile-time contract test.
// It verifies that Print(io.Writer, error) signature exists.
func TestPrintSignatureUnchanged(t *testing.T) {
	// This test compiles if and only if Print has the expected signature.
	// The explicit type assertion ensures the signature matches exactly.
	var fn = (func(io.Writer, error))(Print)
	_ = fn // Use the variable to avoid "unused" error
}

// TestPrintWithOptionsSignature is a compile-time contract test.
// It verifies that PrintWithOptions(io.Writer, error, PrintOptions) signature exists.
func TestPrintWithOptionsSignature(t *testing.T) {
	// This test compiles if and only if PrintWithOptions has the expected signature.
	// The explicit type assertion ensures the signature matches exactly.
	var fn = (func(io.Writer, error, PrintOptions))(PrintWithOptions)
	_ = fn
}

// TestFormatFirstLineAlwaysErrorCode verifies first line is always error_code.
func TestFormatFirstLineAlwaysErrorCode(t *testing.T) {
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
			err := New(tt.code, tt.msg)
			output := Format(err, PrintOptions{})

			lines := strings.Split(output, "\n")
			if len(lines) < 1 {
				t.Fatal("expected at least one line of output")
			}

			expected := "error_code: " + string(tt.code)
			if lines[0] != expected {
				t.Errorf("first line = %q, want %q", lines[0], expected)
			}
		})
	}
}

// TestFormatMessageSecondLine verifies message is always second line.
func TestFormatMessageSecondLine(t *testing.T) {
	err := New(EUsage, "test message")
	output := Format(err, PrintOptions{})

	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		t.Fatal("expected at least two lines of output")
	}

	if lines[1] != "test message" {
		t.Errorf("second line = %q, want %q", lines[1], "test message")
	}
}

// TestFormatContextKeysInOrder verifies context keys appear in specified order.
func TestFormatContextKeysInOrder(t *testing.T) {
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script":    "scripts/agency_verify.sh",
		"exit_code": "1",
		"run_id":    "20260110120000-a3f2",
		"log":       "/path/to/verify.log",
	})

	output := Format(err, PrintOptions{})

	// Check that keys appear in the expected order
	runIDIdx := strings.Index(output, "run_id:")
	scriptIdx := strings.Index(output, "script:")
	exitCodeIdx := strings.Index(output, "exit_code:")
	logIdx := strings.Index(output, "log:")

	// Per spec: run_id < script < exit_code < log
	if runIDIdx >= scriptIdx {
		t.Errorf("run_id should come before script")
	}
	if scriptIdx >= exitCodeIdx {
		t.Errorf("script should come before exit_code")
	}
	if exitCodeIdx >= logIdx {
		t.Errorf("exit_code should come before log")
	}
}

// TestFormatUnknownKeysHiddenByDefault verifies unknown keys are hidden without --verbose.
func TestFormatUnknownKeysHiddenByDefault(t *testing.T) {
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script":      "scripts/agency_verify.sh",
		"unknown_key": "should not appear",
		"another_key": "also hidden",
	})

	output := Format(err, PrintOptions{Verbose: false})

	if strings.Contains(output, "unknown_key") {
		t.Error("unknown_key should not appear in default mode")
	}
	if strings.Contains(output, "another_key") {
		t.Error("another_key should not appear in default mode")
	}
}

// TestFormatVerboseRevealsExtras verifies --verbose reveals extra keys.
func TestFormatVerboseRevealsExtras(t *testing.T) {
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script":      "scripts/agency_verify.sh",
		"unknown_key": "should appear",
		"another_key": "also visible",
	})

	output := Format(err, PrintOptions{Verbose: true})

	if !strings.Contains(output, "extra:") {
		t.Error("verbose mode should include 'extra:' section")
	}
	if !strings.Contains(output, "unknown_key") {
		t.Error("unknown_key should appear in verbose mode")
	}
	if !strings.Contains(output, "another_key") {
		t.Error("another_key should appear in verbose mode")
	}
}

// TestFormatMultilineValueEscaped verifies multi-line values are escaped.
func TestFormatMultilineValueEscaped(t *testing.T) {
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script": "line1\nline2\nline3",
	})

	output := Format(err, PrintOptions{})

	// The value should not contain actual newlines (they should be escaped)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "script:") {
			if strings.Contains(line, "line1") && strings.Contains(line, "line2") {
				// The value is on one line, which is correct
				if !strings.Contains(line, "\\n") {
					t.Error("newlines should be escaped as \\n")
				}
			}
		}
	}
}

// TestFormatMissingContextKeysSkipped verifies missing keys don't create empty lines.
func TestFormatMissingContextKeysSkipped(t *testing.T) {
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script": "scripts/agency_verify.sh",
		// No other keys
	})

	output := Format(err, PrintOptions{})

	// Should not have empty key lines like "exit_code:" with no value
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasSuffix(line, ":") {
			t.Errorf("found empty key line: %q", line)
		}
	}
}

// TestFormatCRLFNormalized verifies CRLF is normalized to LF.
func TestFormatCRLFNormalized(t *testing.T) {
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script": "line1\r\nline2\r\n",
	})

	output := Format(err, PrintOptions{})

	// Should not contain \r
	if strings.Contains(output, "\r") {
		t.Error("output should not contain \\r")
	}
}

// TestFormatLongValuesTruncated verifies long values are truncated.
func TestFormatLongValuesTruncated(t *testing.T) {
	longValue := strings.Repeat("a", 300)
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script": longValue,
	})

	output := Format(err, PrintOptions{})

	// The truncated value should be maxValueLen (256) chars + "…"
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "script:") {
			val := strings.TrimPrefix(line, "script: ")
			if len(val) > maxValueLen+3 { // +3 for "…"
				t.Errorf("value length = %d, should be truncated to ~%d", len(val), maxValueLen)
			}
			if !strings.HasSuffix(val, "…") {
				t.Error("truncated value should end with …")
			}
		}
	}
}

// TestFormatNilDetailsMap verifies nil Details map doesn't cause panic.
func TestFormatNilDetailsMap(t *testing.T) {
	err := New(EUsage, "test")
	output := Format(err, PrintOptions{})

	if !strings.Contains(output, "error_code: E_USAGE") {
		t.Error("should still have error_code line")
	}
	if !strings.Contains(output, "test") {
		t.Error("should still have message line")
	}
}

// TestFormatEmptyStringValues verifies empty string values are skipped.
func TestFormatEmptyStringValues(t *testing.T) {
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"script":    "scripts/agency_verify.sh",
		"exit_code": "",
	})

	output := Format(err, PrintOptions{})

	if strings.Contains(output, "exit_code:") {
		t.Error("empty exit_code should not appear")
	}
}

// TestFormatDetailsOnlyUnknownKeys verifies handling of details with only unknown keys.
func TestFormatDetailsOnlyUnknownKeys(t *testing.T) {
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"custom_key1": "value1",
		"custom_key2": "value2",
	})

	// Default mode
	output := Format(err, PrintOptions{})
	if strings.Contains(output, "custom_key1") {
		t.Error("unknown keys should not appear in default mode")
	}

	// Verbose mode
	output = Format(err, PrintOptions{Verbose: true})
	if !strings.Contains(output, "custom_key1") {
		t.Error("unknown keys should appear in verbose mode")
	}
}

// TestFormatVerifyFailureDetection verifies verify failure detection rule.
func TestFormatVerifyFailureDetection(t *testing.T) {
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
			ae := &AgencyError{
				Code:    tt.code,
				Msg:     "test",
				Details: tt.details,
			}

			result := isVerifyFailure(ae)
			if result != tt.expected {
				t.Errorf("isVerifyFailure() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestFormatHintLine verifies hint line is printed at the end.
func TestFormatHintLine(t *testing.T) {
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"hint": "fix the failing tests",
	})

	output := Format(err, PrintOptions{})

	if !strings.Contains(output, "hint: fix the failing tests") {
		t.Error("should contain hint line")
	}

	// Hint should be near the end
	lines := strings.Split(strings.TrimSpace(output), "\n")
	found := false
	for i := len(lines) - 3; i < len(lines); i++ {
		if i >= 0 && strings.HasPrefix(lines[i], "hint:") {
			found = true
			break
		}
	}
	if !found {
		t.Error("hint should be near the end of output")
	}
}

// TestPrintWithOptionsNil verifies PrintWithOptions handles nil error.
func TestPrintWithOptionsNil(t *testing.T) {
	var buf bytes.Buffer
	PrintWithOptions(&buf, nil, PrintOptions{})

	if buf.Len() != 0 {
		t.Error("nil error should produce no output")
	}
}

// TestPrintWithOptionsNonAgencyError verifies handling of non-AgencyError.
func TestPrintWithOptionsNonAgencyError(t *testing.T) {
	var buf bytes.Buffer
	err := &testError{msg: "plain error"}
	PrintWithOptions(&buf, err, PrintOptions{})

	output := buf.String()
	if !strings.Contains(output, "plain error") {
		t.Error("should contain error message")
	}
}

// TestSanitizeValue verifies sanitizeValue function.
func TestSanitizeValue(t *testing.T) {
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
		{"truncate", "hello world", 5, "hello…"},
		{"empty", "", 100, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeValue(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("sanitizeValue(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

// TestReadTail verifies readTail function.
func TestReadTail(t *testing.T) {
	// Create a temp file with test content
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := readTail(path, 3, 1024)
	if err != nil {
		t.Fatal(err)
	}

	if len(lines) != 3 {
		t.Errorf("got %d lines, want 3", len(lines))
	}

	// Should have last 3 lines
	expected := []string{"line3", "line4", "line5"}
	for i, want := range expected {
		if lines[i] != want {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], want)
		}
	}
}

// TestReadTailEmptyFile verifies readTail handles empty files.
func TestReadTailEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")

	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := readTail(path, 10, 1024)
	if err != nil {
		t.Fatal(err)
	}

	if len(lines) != 0 {
		t.Errorf("got %d lines, want 0", len(lines))
	}
}

// TestReadTailNonexistentFile verifies readTail handles missing files.
func TestReadTailNonexistentFile(t *testing.T) {
	_, err := readTail("/nonexistent/path/file.log", 10, 1024)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// TestGetHint verifies GetHint function.
func TestGetHint(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "with hint",
			err:      NewWithDetails(EScriptFailed, "test", map[string]string{"hint": "fix it"}),
			expected: "fix it",
		},
		{
			name:     "no hint",
			err:      NewWithDetails(EScriptFailed, "test", map[string]string{"other": "value"}),
			expected: "",
		},
		{
			name:     "nil details",
			err:      New(EScriptFailed, "test"),
			expected: "",
		},
		{
			name:     "non-agency error",
			err:      &testError{msg: "plain"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetHint(tt.err)
			if result != tt.expected {
				t.Errorf("GetHint() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestFormatHint verifies FormatHint function.
func TestFormatHint(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"fix it", "hint: fix it"},
		{"hint: already prefixed", "hint: already prefixed"},
	}

	for _, tt := range tests {
		result := FormatHint(tt.input)
		if result != tt.expected {
			t.Errorf("FormatHint(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// testError is a simple error implementation for testing.
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// TestFormatWithTailer verifies PrintWithOptions uses custom tailer.
func TestFormatWithTailer(t *testing.T) {
	err := NewWithDetails(EScriptFailed, "verify failed", map[string]string{
		"log":    "/path/to/verify.log",
		"script": "scripts/agency_verify.sh",
	})

	tailerCalled := false
	tailer := func(logPath string, maxLines int) ([]string, error) {
		tailerCalled = true
		if logPath != "/path/to/verify.log" {
			t.Errorf("tailer got path %q, want /path/to/verify.log", logPath)
		}
		return []string{"test output line 1", "test output line 2"}, nil
	}

	var buf bytes.Buffer
	PrintWithOptions(&buf, err, PrintOptions{Tailer: tailer})

	if !tailerCalled {
		t.Error("tailer should have been called for verify failure")
	}

	output := buf.String()
	if !strings.Contains(output, "output") {
		t.Error("should contain output block header")
	}
	if !strings.Contains(output, "test output line 1") {
		t.Error("should contain tailer output")
	}
}

// TestDeriveTryLines verifies try line suggestions.
func TestDeriveTryLines(t *testing.T) {
	tests := []struct {
		name     string
		code     Code
		details  map[string]string
		contains string
	}{
		{
			name:     "session not found suggests resume",
			code:     ESessionNotFound,
			details:  map[string]string{"run_id": "test-123"},
			contains: "agency resume test-123",
		},
		{
			name:     "gh not authenticated suggests login",
			code:     EGhNotAuthenticated,
			details:  nil,
			contains: "gh auth login",
		},
		{
			name:     "remote out of date suggests push",
			code:     ERemoteOutOfDate,
			details:  map[string]string{"run_id": "test-456"},
			contains: "agency push test-456",
		},
		{
			name:     "no pr suggests push",
			code:     ENoPR,
			details:  map[string]string{"run_id": "test-789"},
			contains: "agency push test-789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			if !found {
				t.Errorf("expected try line containing %q, got %v", tt.contains, lines)
			}
		})
	}
}
