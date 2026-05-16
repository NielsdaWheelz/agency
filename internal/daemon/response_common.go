package daemon

import (
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/version"
)

func daemonBuildVersion() string {
	return version.FullVersion()
}

func prepareRequestID(w http.ResponseWriter, r *http.Request) string {
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	return requestID
}

// httpStatusForCode maps a daemon error code to an HTTP status. All worktree
// mutation handlers (merge, pr sync, rebase) share this mapping.
func httpStatusForCode(code errors.Code) int {
	switch code {
	case errors.EWorktreeNotFound, errors.EInvocationNotFound, errors.EWorktreeMergeNotFound, errors.ENoPR:
		return http.StatusNotFound
	case errors.EWorktreeIDAmbiguous, errors.EInvocationIDAmbiguous, errors.ERepoLocked,
		errors.EWorktreeMergeActive, errors.EDirtyWorktree, errors.EGitPushFailed,
		errors.EPRNotOpen, errors.EWorktreeHasUnresolvedInvocations, errors.EInvocationStillRunning,
		errors.EConfirmationRequired, errors.EArchiveFailed, errors.EWorktreeMergeInterrupted,
		errors.EPRDraft, errors.EPRMismatch, errors.EPRNotMergeable, errors.EPRMergeabilityUnknown,
		errors.EGHPRMergeFailed, errors.EGHPRViewFailed, errors.EScriptFailed, errors.ERebaseConflict:
		return http.StatusConflict
	case errors.EGitFetchFailed:
		return http.StatusBadGateway
	case errors.EBaseNotFound, errors.EEmptyDiff, errors.EGHRepoParseFailed,
		errors.EGhNotInstalled, errors.EGhNotAuthenticated, errors.EInvalidArgument:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func (s *Server) invocationLogPaths(repoID, invocationID string) *LogPaths {
	return &LogPaths{
		Raw:      s.readableInvocationLogPath(repoID, invocationID, "raw"),
		Stderr:   s.readableInvocationLogPath(repoID, invocationID, "stderr"),
		Stream:   s.readableInvocationLogPath(repoID, invocationID, "stream"),
		Hooks:    s.readableInvocationLogPath(repoID, invocationID, "hooks"),
		Terminal: s.readableInvocationLogPath(repoID, invocationID, "terminal"),
	}
}

func (s *Server) worktreeNameForResponse(repoID, worktreeID string) string {
	if worktreeID == "" {
		return ""
	}
	worktreeMeta, err := s.Store.ReadIntegrationWorktreeMeta(repoID, worktreeID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(worktreeMeta.Name)
}
