package daemon

import (
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Server) writeWorktreeError(w http.ResponseWriter, status int, code, message, hint string) {
	s.writeJSON(w, status, WorktreeCreateResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	})
}

func (s *Server) writeWorktreeSuccess(w http.ResponseWriter, worktreeID, treePath, branch, repoID, executionProfile, checkoutRoot string) {
	s.writeJSON(w, http.StatusOK, WorktreeCreateResponse{
		OK:               true,
		WorktreeID:       worktreeID,
		TreePath:         treePath,
		Branch:           branch,
		RepoID:           repoID,
		ExecutionProfile: executionProfile,
		CheckoutRoot:     checkoutRoot,
		APIVersion:       APIVersion,
		BuildVersion:     daemonBuildVersion(),
	})
}

func (s *Server) writeWorktreeRmError(w http.ResponseWriter, status int, code, message, hint string) {
	s.writeJSON(w, status, WorktreeRmResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	})
}

func (s *Server) writeWorktreeRmSuccess(w http.ResponseWriter) {
	s.writeJSON(w, http.StatusOK, WorktreeRmResponse{
		OK:           true,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
	})
}

func (s *Server) writeWorktreeRebaseError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	s.writeJSON(w, status, WorktreeRebaseResponse{
		OK:           false,
		RequestID:    requestID,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	})
}

func (s *Server) writeWorktreeRebaseSuccess(w http.ResponseWriter, requestID string, record *store.IntegrationWorktreeRecord) {
	s.writeJSON(w, http.StatusOK, WorktreeRebaseResponse{
		OK:                    true,
		RequestID:             requestID,
		APIVersion:            APIVersion,
		BuildVersion:          daemonBuildVersion(),
		RepoID:                record.RepoID,
		IntegrationWorktreeID: record.WorktreeID,
		Branch:                record.Meta.Branch,
		BaseBranch:            record.Meta.BaseBranch,
	})
}

func (s *Server) writeWorktreePRSyncError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	s.writeJSON(w, status, WorktreePRSyncResponse{
		OK:           false,
		RequestID:    requestID,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	})
}

func (s *Server) writeWorktreePRSyncSuccess(w http.ResponseWriter, requestID string, record *store.IntegrationWorktreeRecord, result *prSyncResult) {
	s.writeJSON(w, http.StatusOK, WorktreePRSyncResponse{
		OK:                    true,
		RequestID:             requestID,
		APIVersion:            APIVersion,
		BuildVersion:          daemonBuildVersion(),
		RepoID:                record.RepoID,
		IntegrationWorktreeID: record.WorktreeID,
		Branch:                result.Branch,
		PRNumber:              result.PRNumber,
		PRURL:                 result.PRURL,
		PRAction:              result.PRAction,
	})
}

func (s *Server) writeWorktreeMergeError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	s.writeJSON(w, status, WorktreePRMergeResponse{
		OK:           false,
		RequestID:    requestID,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	})
}

func (s *Server) worktreePRMergeResponse(
	record *store.IntegrationWorktreeRecord,
	requestID string,
	action string,
	mergeMeta *store.IntegrationWorktreeMergeMeta,
) *WorktreePRMergeResponse {
	return &WorktreePRMergeResponse{
		OK:                    true,
		RequestID:             requestID,
		APIVersion:            APIVersion,
		BuildVersion:          daemonBuildVersion(),
		Action:                action,
		RepoID:                record.RepoID,
		IntegrationWorktreeID: record.WorktreeID,
		Merge:                 WorktreeMergeMetaToDTO(mergeMeta),
	}
}
