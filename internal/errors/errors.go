// Package errors defines the stable error code system for agency.
package errors

import (
	"errors"
	"fmt"
	"maps"
	"strings"
)

// Code is a stable error code string.
type Code string

// Error codes. Stable public contract.
const (
	EUsage            Code = "E_USAGE"
	ENotImplemented   Code = "E_NOT_IMPLEMENTED"
	ENotFound         Code = "E_NOT_FOUND"
	EMethodNotAllowed Code = "E_METHOD_NOT_ALLOWED"

	ENoRepo                   Code = "E_NO_REPO"
	ENoAgencyJSON             Code = "E_NO_AGENCY_JSON"
	ENoUserConfig             Code = "E_NO_USER_CONFIG"
	EInvalidAgencyJSON        Code = "E_INVALID_AGENCY_JSON"
	EInvalidUserConfig        Code = "E_INVALID_USER_CONFIG"
	EInvalidExecutionProfile  Code = "E_INVALID_EXECUTION_PROFILE"
	EExecutionProfileNotFound Code = "E_EXECUTION_PROFILE_NOT_FOUND"
	EInvalidCheckoutRoot      Code = "E_INVALID_CHECKOUT_ROOT"
	ECheckoutRootUnsafe       Code = "E_CHECKOUT_ROOT_UNSAFE"
	EAgencyJSONExists         Code = "E_AGENCY_JSON_EXISTS"
	ERunnerNotConfigured      Code = "E_RUNNER_NOT_CONFIGURED"
	EEditorNotConfigured      Code = "E_EDITOR_NOT_CONFIGURED"
	EStoreCorrupt             Code = "E_STORE_CORRUPT"

	// Tool/prerequisite error codes
	EGitNotInstalled     Code = "E_GIT_NOT_INSTALLED"
	ETmuxNotInstalled    Code = "E_TMUX_NOT_INSTALLED"
	EGhNotInstalled      Code = "E_GH_NOT_INSTALLED"
	EGhNotAuthenticated  Code = "E_GH_NOT_AUTHENTICATED"
	EScriptNotFound      Code = "E_SCRIPT_NOT_FOUND"
	EScriptNotExecutable Code = "E_SCRIPT_NOT_EXECUTABLE"
	EPersistFailed       Code = "E_PERSIST_FAILED"
	EInternal            Code = "E_INTERNAL"

	EEmptyRepo            Code = "E_EMPTY_REPO"
	EBaseDirty            Code = "E_BASE_DIRTY"
	EBaseBranchNotFound   Code = "E_BASE_BRANCH_NOT_FOUND"
	EWorktreeCreateFailed Code = "E_WORKTREE_CREATE_FAILED"
	ETmuxSessionExists    Code = "E_TMUX_SESSION_EXISTS"
	ETmuxFailed           Code = "E_TMUX_FAILED"
	ETmuxSessionMissing   Code = "E_TMUX_SESSION_MISSING"
	ERunNotFound          Code = "E_RUN_NOT_FOUND"
	ERunRepoMismatch      Code = "E_RUN_REPO_MISMATCH"
	EScriptTimeout        Code = "E_SCRIPT_TIMEOUT"
	EScriptFailed         Code = "E_SCRIPT_FAILED"

	ERunDirExists       Code = "E_RUN_DIR_EXISTS"
	ERunDirCreateFailed Code = "E_RUN_DIR_CREATE_FAILED"
	EMetaWriteFailed    Code = "E_META_WRITE_FAILED"

	ETmuxAttachFailed Code = "E_TMUX_ATTACH_FAILED"

	ERunIDAmbiguous Code = "E_RUN_ID_AMBIGUOUS" // id prefix matches >1 run
	ERunBroken      Code = "E_RUN_BROKEN"       // run exists but meta.json is unreadable/invalid
	ERepoLocked     Code = "E_REPO_LOCKED"      // another agency process holds the lock

	EUnsupportedOriginHost Code = "E_UNSUPPORTED_ORIGIN_HOST" // origin is not github.com
	ENoOrigin              Code = "E_NO_ORIGIN"               // no origin remote configured
	EBaseNotFound          Code = "E_BASE_NOT_FOUND"          // base branch ref not found locally or on origin
	EGitPushFailed         Code = "E_GIT_PUSH_FAILED"         // git push non-zero exit
	EGHPRCreateFailed      Code = "E_GH_PR_CREATE_FAILED"     // gh pr create non-zero exit
	EGHPREditFailed        Code = "E_GH_PR_EDIT_FAILED"       // gh pr edit non-zero exit
	EGHPRViewFailed        Code = "E_GH_PR_VIEW_FAILED"       // gh pr view failed after create retries
	EPRNotOpen             Code = "E_PR_NOT_OPEN"             // PR exists but is not open (CLOSED or MERGED)
	EEmptyDiff             Code = "E_EMPTY_DIFF"              // no commits ahead of base branch
	EWorktreeMissing       Code = "E_WORKTREE_MISSING"        // run worktree path is missing on disk
	EDirtyWorktree         Code = "E_DIRTY_WORKTREE"          // run worktree has uncommitted changes

	ESessionNotFound      Code = "E_SESSION_NOT_FOUND"     // tmux session is missing
	EConfirmationRequired Code = "E_CONFIRMATION_REQUIRED" // restart attempted without confirmation in non-interactive mode

	EWorkspaceArchived Code = "E_WORKSPACE_ARCHIVED" // run exists but worktree missing or archived; cannot verify

	EArchiveFailed            Code = "E_ARCHIVE_FAILED"             // archive step failed (script failure and/or deletion failure)
	EAborted                  Code = "E_ABORTED"                    // user declined confirmation / wrong confirmation token
	ENotInteractive           Code = "E_NOT_INTERACTIVE"            // command requires an interactive TTY
	EGitFetchFailed           Code = "E_GIT_FETCH_FAILED"           // git fetch failed
	ERemoteOutOfDate          Code = "E_REMOTE_OUT_OF_DATE"         // local head sha != origin/<branch> sha
	EPRDraft                  Code = "E_PR_DRAFT"                   // PR is a draft
	EPRMismatch               Code = "E_PR_MISMATCH"                // resolved PR does not match expected branch
	EGHRepoParseFailed        Code = "E_GH_REPO_PARSE_FAILED"       // failed to parse owner/repo from origin
	EPRMergeabilityUnknown    Code = "E_PR_MERGEABILITY_UNKNOWN"    // gh reports mergeable as UNKNOWN after retries
	EGHPRMergeFailed          Code = "E_GH_PR_MERGE_FAILED"         // gh merge failed or merge state could not be confirmed
	EPRNotMergeable           Code = "E_PR_NOT_MERGEABLE"           // PR cannot be merged (conflicts or checks failing)
	ENoPR                     Code = "E_NO_PR"                      // no PR exists for the branch or worktree
	ERebaseConflict           Code = "E_REBASE_CONFLICT"            // git rebase encountered conflicts during worktree rebase
	EWorktreeMergeNotFound    Code = "E_WORKTREE_MERGE_NOT_FOUND"   // worktree exists but no durable merge state exists yet
	EWorktreeMergeActive      Code = "E_WORKTREE_MERGE_ACTIVE"      // another merge attempt already owns this worktree
	EWorktreeMergeInterrupted Code = "E_WORKTREE_MERGE_INTERRUPTED" // merge attempt was interrupted before reaching a terminal step

	// Name validation error codes
	ENameExists  Code = "E_NAME_EXISTS"  // name already used by an active run
	EInvalidName Code = "E_INVALID_NAME" // name does not match validation rules

	EInvalidRepoPath Code = "E_INVALID_REPO_PATH" // --repo path does not exist or is not inside a git repo
	ERepoNotFound    Code = "E_REPO_NOT_FOUND"    // run resolved but no valid repo path exists

	EWorktreeNotFound     Code = "E_WORKTREE_NOT_FOUND"     // worktree does not exist
	EWorktreeIDAmbiguous  Code = "E_WORKTREE_ID_AMBIGUOUS"  // worktree id/prefix matches multiple
	EWorktreeBroken       Code = "E_WORKTREE_BROKEN"        // worktree exists but meta.json is unreadable
	EWorktreeDirExists    Code = "E_WORKTREE_DIR_EXISTS"    // worktree directory already exists
	EWorktreeRemoveFailed Code = "E_WORKTREE_REMOVE_FAILED" // git worktree remove failed

	EInvocationNotFound       Code = "E_INVOCATION_NOT_FOUND"       // invocation does not exist
	EInvocationIDAmbiguous    Code = "E_INVOCATION_ID_AMBIGUOUS"    // invocation id/prefix matches multiple
	EInvocationBroken         Code = "E_INVOCATION_BROKEN"          // invocation exists but meta.json is unreadable
	EInvocationDirExists      Code = "E_INVOCATION_DIR_EXISTS"      // invocation directory already exists
	EInvocationCreateFailed   Code = "E_INVOCATION_CREATE_FAILED"   // invocation creation failed
	ESandboxCreateFailed      Code = "E_SANDBOX_CREATE_FAILED"      // sandbox worktree creation failed
	EIntegrationMarkerMissing Code = "E_INTEGRATION_MARKER_MISSING" // target is not an integration worktree
	ESandboxPathUnsafe        Code = "E_SANDBOX_PATH_UNSAFE"        // sandbox path resolves to integration tree

	EInvocationInvalidMode  Code = "E_INVOCATION_INVALID_MODE"  // operation not supported for invocation mode (e.g., attach on headless)
	EInvocationNotRunning   Code = "E_INVOCATION_NOT_RUNNING"   // invocation is not in running state
	EInvocationStartFailed  Code = "E_INVOCATION_START_FAILED"  // runner failed to start (tmux session creation failed)
	EInvocationAlreadyEnded Code = "E_INVOCATION_ALREADY_ENDED" // invocation has already finished/failed

	EDaemonNotRunning        Code = "E_DAEMON_NOT_RUNNING"        // daemon is not running (socket missing, /health fails)
	EDaemonAlreadyRunning    Code = "E_DAEMON_ALREADY_RUNNING"    // another daemon instance is already running on this socket
	EDaemonBusy              Code = "E_DAEMON_BUSY"               // active headless invocations exist; use --force to override
	EDaemonStartFailed       Code = "E_DAEMON_START_FAILED"       // daemon failed to start
	EDaemonConnectionFailed  Code = "E_DAEMON_CONNECTION_FAILED"  // failed to connect to daemon
	ESandboxValidationFailed Code = "E_SANDBOX_VALIDATION_FAILED" // sandbox marker missing, integration marker present, or path mismatch
	EInvocationTerminal      Code = "E_INVOCATION_TERMINAL"       // invocation already in terminal state (finished/failed)
	EInvocationOrphaned      Code = "E_INVOCATION_ORPHANED"       // PID dead but was previously running; process exited without daemon observing
	ERunnerNotFound          Code = "E_RUNNER_NOT_FOUND"          // runner binary not found on PATH or in config
	ERunnerStartFailed       Code = "E_RUNNER_START_FAILED"       // runner process failed to start (exec error)
	EInvocationNameExists    Code = "E_INVOCATION_NAME_EXISTS"    // invocation name already used by an active invocation
	ELifecycleOwnerMismatch  Code = "E_LIFECYCLE_OWNER_MISMATCH"  // attempt to modify invocation owned by another entity
	EPromptRequired          Code = "E_PROMPT_REQUIRED"           // headless invocation requires a prompt

	// Task orchestration error codes
	ETaskNotFound            Code = "E_TASK_NOT_FOUND"            // task does not exist
	ETaskIDAmbiguous         Code = "E_TASK_ID_AMBIGUOUS"         // task id/prefix matches multiple
	ETaskBroken              Code = "E_TASK_BROKEN"               // task exists but meta.json is unreadable
	ETaskDirExists           Code = "E_TASK_DIR_EXISTS"           // task directory already exists
	ETaskCreateFailed        Code = "E_TASK_CREATE_FAILED"        // task creation failed
	ETaskNameExists          Code = "E_TASK_NAME_EXISTS"          // task name already used by a non-archived task
	ETaskFingerprintConflict Code = "E_TASK_FINGERPRINT_CONFLICT" // idempotency key reused with different task request

	// Daemon service manager error codes
	EDaemonServiceInstallFailed    Code = "E_DAEMON_SERVICE_INSTALL_FAILED"    // service install operation failed
	EDaemonServiceUninstallFailed  Code = "E_DAEMON_SERVICE_UNINSTALL_FAILED"  // service uninstall operation failed
	EDaemonServiceUnsupported      Code = "E_DAEMON_SERVICE_UNSUPPORTED"       // platform does not support service management
	EDaemonServiceAlreadyInstalled Code = "E_DAEMON_SERVICE_ALREADY_INSTALLED" // service is already installed
	EDaemonServiceNotInstalled     Code = "E_DAEMON_SERVICE_NOT_INSTALLED"     // service is not installed

	EUnsafeRepoRoot      Code = "E_UNSAFE_REPO_ROOT"     // repo_root is inside an agency-managed worktree
	EInvalidRequest      Code = "E_INVALID_REQUEST"      // request body, route, or required field is invalid
	EPromptTooLarge      Code = "E_PROMPT_TOO_LARGE"     // prompt exceeds 256 KB
	EDaemonIncompatible  Code = "E_DAEMON_INCOMPATIBLE"  // CLI api_version does not match daemon api_version
	ERunnerArgConflict   Code = "E_RUNNER_ARG_CONFLICT"  // user-supplied args include reserved flags
	EIdempotencyConflict Code = "E_IDEMPOTENCY_CONFLICT" // client_request_id reused with a different request

	EWorktreeHasUnresolvedInvocations Code = "E_WORKTREE_HAS_UNRESOLVED_INVOCATIONS" // rm/merge blocked by unlanded agent work
	ENotAnIntegrationWorktree         Code = "E_NOT_AN_INTEGRATION_WORKTREE"         // tree missing .agency/INTEGRATION_MARKER on rm
	EWorktreeNameExists               Code = "E_WORKTREE_NAME_EXISTS"                // name collision with existing worktree

	EInvocationStillRunning Code = "E_INVOCATION_STILL_RUNNING" // checkpoint apply refused — invocation must be stopped/finished first
	ECheckpointNotFound     Code = "E_CHECKPOINT_NOT_FOUND"     // requested checkpoint_id does not exist in checkpoints.json
	ERollbackFailed         Code = "E_ROLLBACK_FAILED"          // git reset/clean/checkout failed during checkpoint apply
	ECheckpointFailed       Code = "E_CHECKPOINT_FAILED"        // checkpoint creation failed (git error, index lock, etc.)

	ELandConflict           Code = "E_LAND_CONFLICT"            // cherry-pick or apply resulted in merge conflicts
	ELandNothingToLand      Code = "E_LAND_NOTHING_TO_LAND"     // sandbox has no commits and no uncommitted changes
	ELandApplyRequired      Code = "E_LAND_APPLY_REQUIRED"      // sandbox has no commits but has uncommitted changes; --apply required
	ELandFailed             Code = "E_LAND_FAILED"              // landing operation failed (git error, validation, etc.)
	ELandAlreadyLanded      Code = "E_LAND_ALREADY_LANDED"      // invocation has already been landed
	ELandAlreadyDiscarded   Code = "E_LAND_ALREADY_DISCARDED"   // invocation has already been discarded
	ESandboxMissing         Code = "E_SANDBOX_MISSING"          // sandbox tree no longer exists
	EIntegrationTreeMissing Code = "E_INTEGRATION_TREE_MISSING" // integration worktree tree no longer exists

	ESessionEnded Code = "E_SESSION_ENDED" // tmux session ended; use logs or open to view

	ERepoRootInaccessible  Code = "E_REPO_ROOT_INACCESSIBLE"   // cannot stat / permission denied / path missing
	ERepoNotAGitRepo       Code = "E_REPO_NOT_A_GIT_REPO"      // git rev-parse --show-toplevel fails
	ERepoNoAccessibleRoots Code = "E_REPO_NO_ACCESSIBLE_ROOTS" // all registered roots are inaccessible
	EAmbiguous             Code = "E_AMBIGUOUS"                // name/ref matches multiple entities across repos
	ENoRepoContext         Code = "E_NO_REPO_CONTEXT"          // command requires repo context but none available
	ERepoIDAmbiguous       Code = "E_REPO_ID_AMBIGUOUS"        // repo ref matches multiple repos
	ERepoHasWorktrees      Code = "E_REPO_HAS_WORKTREES"       // repo unregister blocked by present or broken worktrees
	ERepoHasInvocations    Code = "E_REPO_HAS_INVOCATIONS"     // repo unregister blocked by active invocations

	ELogNotFound     Code = "E_LOG_NOT_FOUND"    // log file does not exist or kind unavailable
	EInvalidArgument Code = "E_INVALID_ARGUMENT" // invalid parameter (offset, limit, interval, etc.)

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

// exitCodeError wraps an error with an explicit process exit code.
type exitCodeError struct {
	Err  error
	Code int
}

func (e *exitCodeError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

func (e *exitCodeError) Unwrap() error {
	return e.Err
}

func (e *exitCodeError) ExitCode() int {
	return e.Code
}

// WithExitCode wraps err with a specific process exit code.
func WithExitCode(err error, code int) error {
	return &exitCodeError{Err: err, Code: code}
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

// CodeOr returns the error's code, or defaultCode if err carries no code.
func CodeOr(err error, defaultCode Code) Code {
	if code := GetCode(err); code != "" {
		return code
	}
	return defaultCode
}

// AsAgencyError returns (*AgencyError, true) if err is or wraps an AgencyError.
func AsAgencyError(err error) (*AgencyError, bool) {
	var ae *AgencyError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

// Hint returns the trimmed "hint" detail attached to err, or empty if none.
func Hint(err error) string {
	ae, ok := AsAgencyError(err)
	if !ok || ae.Details == nil {
		return ""
	}
	return strings.TrimSpace(ae.Details["hint"])
}

// copyDetails returns a defensive copy of the details map, or nil if empty/nil.
func copyDetails(details map[string]string) map[string]string {
	if len(details) == 0 {
		return nil
	}
	return maps.Clone(details)
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
