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
	ENoRepo              Code = "E_NO_REPO"
	ENoAgencyJSON        Code = "E_NO_AGENCY_JSON"
	EInvalidAgencyJSON   Code = "E_INVALID_AGENCY_JSON"
	EAgencyJSONExists    Code = "E_AGENCY_JSON_EXISTS"
	ERunnerNotConfigured Code = "E_RUNNER_NOT_CONFIGURED"
	EStoreCorrupt        Code = "E_STORE_CORRUPT"

	// Tool/prerequisite error codes
	EGitNotInstalled     Code = "E_GIT_NOT_INSTALLED"
	ETmuxNotInstalled    Code = "E_TMUX_NOT_INSTALLED"
	EGhNotInstalled      Code = "E_GH_NOT_INSTALLED"
	EGhNotAuthenticated  Code = "E_GH_NOT_AUTHENTICATED"
	EScriptNotFound      Code = "E_SCRIPT_NOT_FOUND"
	EScriptNotExecutable Code = "E_SCRIPT_NOT_EXECUTABLE"
	EPersistFailed       Code = "E_PERSIST_FAILED"
	EInternal            Code = "E_INTERNAL"

	// Slice 1 error codes
	EEmptyRepo            Code = "E_EMPTY_REPO"
	EParentDirty          Code = "E_PARENT_DIRTY"
	EParentBranchNotFound Code = "E_PARENT_BRANCH_NOT_FOUND"
	EWorktreeCreateFailed Code = "E_WORKTREE_CREATE_FAILED"
	ETmuxSessionExists    Code = "E_TMUX_SESSION_EXISTS"
	ETmuxFailed           Code = "E_TMUX_FAILED"
	ETmuxSessionMissing   Code = "E_TMUX_SESSION_MISSING"
	ERunNotFound          Code = "E_RUN_NOT_FOUND"
	ERunRepoMismatch      Code = "E_RUN_REPO_MISMATCH"
	EScriptTimeout        Code = "E_SCRIPT_TIMEOUT"
	EScriptFailed         Code = "E_SCRIPT_FAILED"
)

// AgencyError is the standard error type for agency errors.
type AgencyError struct {
	Code    Code
	Msg     string
	Cause   error
	Details map[string]string // optional structured context
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

// NewWithDetails creates a new AgencyError with code, message, and details.
// Details map is defensively copied (nil if empty).
func NewWithDetails(code Code, msg string, details map[string]string) error {
	return &AgencyError{Code: code, Msg: msg, Details: copyDetails(details)}
}

// Wrap creates a new AgencyError wrapping an underlying error.
func Wrap(code Code, msg string, err error) error {
	return &AgencyError{Code: code, Msg: msg, Cause: err}
}

// WrapWithDetails creates a new AgencyError wrapping an underlying error with details.
// Details map is defensively copied (nil if empty).
func WrapWithDetails(code Code, msg string, err error, details map[string]string) error {
	return &AgencyError{Code: code, Msg: msg, Cause: err, Details: copyDetails(details)}
}

// GetCode extracts the error code from an error, or empty string if not an AgencyError.
func GetCode(err error) Code {
	var ae *AgencyError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

// AsAgencyError returns (*AgencyError, true) if err is or wraps an AgencyError.
func AsAgencyError(err error) (*AgencyError, bool) {
	var ae *AgencyError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

// copyDetails returns a defensive copy of the details map, or nil if empty/nil.
func copyDetails(details map[string]string) map[string]string {
	if len(details) == 0 {
		return nil
	}
	cp := make(map[string]string, len(details))
	for k, v := range details {
		cp[k] = v
	}
	return cp
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
