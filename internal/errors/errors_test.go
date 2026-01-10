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
		name   string
		err    error
		want   string
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
