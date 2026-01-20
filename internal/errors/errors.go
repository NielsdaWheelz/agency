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
	EInvalidUserConfig   Code = "E_INVALID_USER_CONFIG"
	EAgencyJSONExists    Code = "E_AGENCY_JSON_EXISTS"
	ERunnerNotConfigured Code = "E_RUNNER_NOT_CONFIGURED"
	EEditorNotConfigured Code = "E_EDITOR_NOT_CONFIGURED"
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

	// Run persistence error codes (slice 1 PR-06)
	ERunDirExists       Code = "E_RUN_DIR_EXISTS"
	ERunDirCreateFailed Code = "E_RUN_DIR_CREATE_FAILED"
	EMetaWriteFailed    Code = "E_META_WRITE_FAILED"

	// Tmux attach error codes (slice 1 PR-09)
	ETmuxAttachFailed Code = "E_TMUX_ATTACH_FAILED"

	// Slice 2 observability error codes
	ERunIDAmbiguous Code = "E_RUN_ID_AMBIGUOUS" // id prefix matches >1 run
	ERunBroken      Code = "E_RUN_BROKEN"       // run exists but meta.json is unreadable/invalid
	ERepoLocked     Code = "E_REPO_LOCKED"      // another agency process holds the lock

	// Slice 3 push/PR error codes
	EUnsupportedOriginHost Code = "E_UNSUPPORTED_ORIGIN_HOST" // origin is not github.com
	ENoOrigin              Code = "E_NO_ORIGIN"               // no origin remote configured
	EParentNotFound        Code = "E_PARENT_NOT_FOUND"        // parent branch ref not found locally or on origin
	EGitPushFailed         Code = "E_GIT_PUSH_FAILED"         // git push non-zero exit
	EGHPRCreateFailed      Code = "E_GH_PR_CREATE_FAILED"     // gh pr create non-zero exit
	EGHPREditFailed        Code = "E_GH_PR_EDIT_FAILED"       // gh pr edit non-zero exit
	EGHPRViewFailed        Code = "E_GH_PR_VIEW_FAILED"       // gh pr view failed after create retries
	EPRNotOpen             Code = "E_PR_NOT_OPEN"             // PR exists but is not open (CLOSED or MERGED)
	EReportInvalid         Code = "E_REPORT_INVALID"          // report missing/empty without --force
	EEmptyDiff             Code = "E_EMPTY_DIFF"              // no commits ahead of parent branch
	EWorktreeMissing       Code = "E_WORKTREE_MISSING"        // run worktree path is missing on disk
	EDirtyWorktree         Code = "E_DIRTY_WORKTREE"          // run worktree has uncommitted changes

	// Slice 4 lifecycle control error codes
	ESessionNotFound      Code = "E_SESSION_NOT_FOUND"     // attach when tmux session is missing; suggests resume
	EConfirmationRequired Code = "E_CONFIRMATION_REQUIRED" // restart attempted without confirmation in non-interactive mode

	// Slice 5 verify error codes
	EWorkspaceArchived Code = "E_WORKSPACE_ARCHIVED" // run exists but worktree missing or archived; cannot verify

	// Slice 6 merge + archive error codes
	EArchiveFailed         Code = "E_ARCHIVE_FAILED"          // archive step failed (script failure and/or deletion failure)
	EAborted               Code = "E_ABORTED"                 // user declined confirmation / wrong confirmation token
	ENotInteractive        Code = "E_NOT_INTERACTIVE"         // command requires an interactive TTY
	EGitFetchFailed        Code = "E_GIT_FETCH_FAILED"        // git fetch failed
	ERemoteOutOfDate       Code = "E_REMOTE_OUT_OF_DATE"      // local head sha != origin/<branch> sha
	EPRDraft               Code = "E_PR_DRAFT"                // PR is a draft
	EPRMismatch            Code = "E_PR_MISMATCH"             // resolved PR does not match expected branch
	EGHRepoParseFailed     Code = "E_GH_REPO_PARSE_FAILED"    // failed to parse owner/repo from origin
	EPRMergeabilityUnknown Code = "E_PR_MERGEABILITY_UNKNOWN" // gh reports mergeable as UNKNOWN after retries
	EGHPRMergeFailed       Code = "E_GH_PR_MERGE_FAILED"      // gh merge failed or merge state could not be confirmed
	EPRNotMergeable        Code = "E_PR_NOT_MERGEABLE"        // PR cannot be merged (conflicts or checks failing)
	ENoPR                  Code = "E_NO_PR"                   // no PR exists for the run

	// Name validation error codes
	ENameExists  Code = "E_NAME_EXISTS"  // name already used by an active run
	EInvalidName Code = "E_INVALID_NAME" // name does not match validation rules

	// Report completeness error codes (S7)
	EReportIncomplete Code = "E_REPORT_INCOMPLETE" // report exists but missing required sections
	// Slice 7 global resolution error codes
	EInvalidRepoPath Code = "E_INVALID_REPO_PATH" // --repo path does not exist or is not inside a git repo
	ERunRefAmbiguous Code = "E_RUN_REF_AMBIGUOUS" // name matches multiple active runs (across repos)
	ERepoNotFound    Code = "E_REPO_NOT_FOUND"    // run resolved but no valid repo path exists
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

// ExitCodeError wraps an error with an explicit process exit code.
type ExitCodeError struct {
	Err  error
	Code int
}

func (e *ExitCodeError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

func (e *ExitCodeError) Unwrap() error {
	return e.Err
}

func (e *ExitCodeError) ExitCode() int {
	return e.Code
}

// WithExitCode wraps err with a specific process exit code.
func WithExitCode(err error, code int) error {
	return &ExitCodeError{Err: err, Code: code}
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
	if ec, ok := err.(interface{ ExitCode() int }); ok {
		return ec.ExitCode()
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
		_, _ = fmt.Fprintf(w, "error_code: %s\n", ae.Code)
		_, _ = fmt.Fprintln(w, ae.Msg)
	} else {
		// Fallback for non-AgencyError errors (should not happen in practice)
		_, _ = fmt.Fprintln(w, err.Error())
	}
}
