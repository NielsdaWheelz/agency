// Package errors defines the stable error code system for agency.
package errors

import (
	"errors"
	"fmt"
	"io"
)

// Code is a stable error code string.
type Code string

// Error codes. Stable public contract per constitution.
const (
	EUsage          Code = "E_USAGE"
	ENotImplemented Code = "E_NOT_IMPLEMENTED"

	// Slice 0 error codes
	ENoRepo Code = "E_NO_REPO"
)

// AgencyError is the standard error type for agency errors.
type AgencyError struct {
	Code  Code
	Msg   string
	Cause error
}

// Error returns the stable error format: "CODE: message".
func (e *AgencyError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Msg)
}

// Unwrap returns the underlying cause for errors.Is/As compatibility.
func (e *AgencyError) Unwrap() error {
	return e.Cause
}

// New creates a new AgencyError with the given code and message.
func New(code Code, msg string) error {
	return &AgencyError{Code: code, Msg: msg}
}

// Wrap creates a new AgencyError wrapping an underlying error.
func Wrap(code Code, msg string, err error) error {
	return &AgencyError{Code: code, Msg: msg, Cause: err}
}

// GetCode extracts the error code from an error, or empty string if not an AgencyError.
func GetCode(err error) Code {
	var ae *AgencyError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

// ExitCode returns the appropriate exit code for an error.
// Returns 0 if err is nil, 2 for E_USAGE, 1 for all other errors.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if GetCode(err) == EUsage {
		return 2
	}
	return 1
}

// Print writes the error to w in the stable stderr format:
//
//	error_code: <CODE>
//	<message>
func Print(w io.Writer, err error) {
	if err == nil {
		return
	}
	var ae *AgencyError
	if errors.As(err, &ae) {
		fmt.Fprintf(w, "error_code: %s\n", ae.Code)
		fmt.Fprintln(w, ae.Msg)
	} else {
		// Fallback for non-AgencyError errors (should not happen in practice)
		fmt.Fprintln(w, err.Error())
	}
}
