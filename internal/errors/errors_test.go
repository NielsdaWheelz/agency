package errors

import (
	"bytes"
	"errors"
	"testing"
)

func TestNew(t *testing.T) {
	err := New(EUsage, "test message")

	if err.Error() != "E_USAGE: test message" {
		t.Errorf("Error() = %q, want %q", err.Error(), "E_USAGE: test message")
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("underlying")
	err := Wrap(ENotImplemented, "wrapped message", cause)

	if err.Error() != "E_NOT_IMPLEMENTED: wrapped message" {
		t.Errorf("Error() = %q, want %q", err.Error(), "E_NOT_IMPLEMENTED: wrapped message")
	}

	// Test Unwrap
	var ae *AgencyError
	if !errors.As(err, &ae) {
		t.Fatal("errors.As failed")
	}
	if ae.Cause != cause {
		t.Error("Unwrap did not return cause")
	}
}

func TestGetCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Code
	}{
		{"nil error", nil, ""},
		{"agency error", New(EUsage, "x"), EUsage},
		{"wrapped agency error", Wrap(ENotImplemented, "y", errors.New("z")), ENotImplemented},
		{"non-agency error", errors.New("plain"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetCode(tt.err)
			if got != tt.want {
				t.Errorf("GetCode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"E_USAGE", New(EUsage, "x"), 2},
		{"E_NOT_IMPLEMENTED", New(ENotImplemented, "x"), 1},
		{"non-agency error", errors.New("x"), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExitCode(tt.err)
			if got != tt.want {
				t.Errorf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPrint(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"E_USAGE", New(EUsage, "bad args"), "error_code: E_USAGE\nbad args\n"},
		{"E_NOT_IMPLEMENTED", New(ENotImplemented, "not ready"), "error_code: E_NOT_IMPLEMENTED\nnot ready\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			Print(&buf, tt.err)
			got := buf.String()
			if got != tt.want {
				t.Errorf("Print() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorFormatStability(t *testing.T) {
	// This test ensures the error format is stable and matches the spec exactly.
	// The format MUST be: "CODE: message"
	err := New(EUsage, "x")
	expected := "E_USAGE: x"
	if err.Error() != expected {
		t.Errorf("error format changed: got %q, want %q", err.Error(), expected)
	}
}

func TestNewWithDetails(t *testing.T) {
	details := map[string]string{"key": "value"}
	err := NewWithDetails(EUsage, "test message", details)

	var ae *AgencyError
	if !errors.As(err, &ae) {
		t.Fatal("errors.As failed")
	}

	if ae.Code != EUsage {
		t.Errorf("Code = %q, want %q", ae.Code, EUsage)
	}
	if ae.Msg != "test message" {
		t.Errorf("Msg = %q, want %q", ae.Msg, "test message")
	}
	if ae.Details["key"] != "value" {
		t.Errorf("Details[key] = %q, want %q", ae.Details["key"], "value")
	}
}

func TestNewWithDetails_NilDetails(t *testing.T) {
	err := NewWithDetails(EUsage, "test", nil)

	var ae *AgencyError
	if !errors.As(err, &ae) {
		t.Fatal("errors.As failed")
	}
	if ae.Details != nil {
		t.Errorf("Details should be nil, got %v", ae.Details)
	}
}

func TestNewWithDetails_DefensiveCopy(t *testing.T) {
	details := map[string]string{"key": "value"}
	err := NewWithDetails(EUsage, "test", details)

	// Modify the original map
	details["key"] = "modified"

	var ae *AgencyError
	if !errors.As(err, &ae) {
		t.Fatal("errors.As failed")
	}
	// The error's details should not be affected
	if ae.Details["key"] != "value" {
		t.Errorf("Details should be defensively copied")
	}
}

func TestWrapWithDetails(t *testing.T) {
	cause := errors.New("underlying")
	details := map[string]string{"file": "test.go"}
	err := WrapWithDetails(EUsage, "wrapped", cause, details)

	var ae *AgencyError
	if !errors.As(err, &ae) {
		t.Fatal("errors.As failed")
	}

	if ae.Cause != cause {
		t.Error("Cause not set")
	}
	if ae.Details["file"] != "test.go" {
		t.Errorf("Details[file] = %q, want %q", ae.Details["file"], "test.go")
	}
}

func TestAsAgencyError(t *testing.T) {
	t.Run("direct AgencyError", func(t *testing.T) {
		err := New(EUsage, "test")
		ae, ok := AsAgencyError(err)
		if !ok {
			t.Error("should return true for AgencyError")
		}
		if ae.Code != EUsage {
			t.Errorf("Code = %q, want %q", ae.Code, EUsage)
		}
	})

	t.Run("non AgencyError", func(t *testing.T) {
		err := errors.New("regular error")
		ae, ok := AsAgencyError(err)
		if ok {
			t.Error("should return false for non-AgencyError")
		}
		if ae != nil {
			t.Error("should return nil for non-AgencyError")
		}
	})

	t.Run("nil error", func(t *testing.T) {
		ae, ok := AsAgencyError(nil)
		if ok {
			t.Error("should return false for nil")
		}
		if ae != nil {
			t.Error("should return nil for nil")
		}
	})
}

// TestSlice3ErrorCodesExist verifies slice 03 error codes are defined and stable.
func TestSlice3ErrorCodesExist(t *testing.T) {
	// This test ensures slice 03 error codes exist as constants.
	// If any are missing or renamed, this test will fail to compile.
	codes := []Code{
		EUnsupportedOriginHost,
		ENoOrigin,
		EParentNotFound,
		EGitPushFailed,
		EGHPRCreateFailed,
		EGHPREditFailed,
		EGHPRViewFailed,
		EPRNotOpen,
		EReportInvalid,
		EEmptyDiff,
	}

	expectedStrings := map[Code]string{
		EUnsupportedOriginHost: "E_UNSUPPORTED_ORIGIN_HOST",
		ENoOrigin:              "E_NO_ORIGIN",
		EParentNotFound:        "E_PARENT_NOT_FOUND",
		EGitPushFailed:         "E_GIT_PUSH_FAILED",
		EGHPRCreateFailed:      "E_GH_PR_CREATE_FAILED",
		EGHPREditFailed:        "E_GH_PR_EDIT_FAILED",
		EGHPRViewFailed:        "E_GH_PR_VIEW_FAILED",
		EPRNotOpen:             "E_PR_NOT_OPEN",
		EReportInvalid:         "E_REPORT_INVALID",
		EEmptyDiff:             "E_EMPTY_DIFF",
	}

	for _, code := range codes {
		expected := expectedStrings[code]
		if string(code) != expected {
			t.Errorf("code = %q, want %q", code, expected)
		}
	}
}

// TestSlice3ErrorFormat verifies slice 03 error codes format correctly.
func TestSlice3ErrorFormat(t *testing.T) {
	tests := []struct {
		code Code
		msg  string
		want string
	}{
		{EUnsupportedOriginHost, "origin is not github.com", "E_UNSUPPORTED_ORIGIN_HOST: origin is not github.com"},
		{ENoOrigin, "no origin remote", "E_NO_ORIGIN: no origin remote"},
		{EParentNotFound, "branch main not found", "E_PARENT_NOT_FOUND: branch main not found"},
		{EGitPushFailed, "push rejected", "E_GIT_PUSH_FAILED: push rejected"},
		{EGHPRCreateFailed, "gh pr create failed", "E_GH_PR_CREATE_FAILED: gh pr create failed"},
		{EGHPREditFailed, "gh pr edit failed", "E_GH_PR_EDIT_FAILED: gh pr edit failed"},
		{EGHPRViewFailed, "gh pr view failed after retries", "E_GH_PR_VIEW_FAILED: gh pr view failed after retries"},
		{EPRNotOpen, "PR is closed", "E_PR_NOT_OPEN: PR is closed"},
		{EReportInvalid, "report missing or empty", "E_REPORT_INVALID: report missing or empty"},
		{EEmptyDiff, "no commits ahead", "E_EMPTY_DIFF: no commits ahead"},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			err := New(tt.code, tt.msg)
			if err.Error() != tt.want {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}
