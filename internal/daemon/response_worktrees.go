package daemon

import (
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Server) writeWorktreeError(w http.ResponseWriter, status int, code, message, hint string) {
	s.writeJSON(w, status, WorktreeCreateResponse{
		responseEnvelope: newErrorEnvelope("", code, message, hint),
	})
}

func (s *Server) writeWorktreeSuccess(w http.ResponseWriter, worktreeID, treePath, branch, repoID, executionProfile, checkoutRoot string) {
	s.writeJSON(w, http.StatusOK, WorktreeCreateResponse{
		responseEnvelope: newSuccessEnvelope(""),
		WorktreeID:               worktreeID,
		TreePath:                 treePath,
		Branch:                   branch,
		RepoID:                   repoID,
		ExecutionProfile:         executionProfile,
		CheckoutRoot:             checkoutRoot,
	})
}

func (s *Server) writeWorktreeRmError(w http.ResponseWriter, status int, code, message, hint string) {
	s.writeJSON(w, status, WorktreeRmResponse{
		responseEnvelope: newErrorEnvelope("", code, message, hint),
	})
}

func (s *Server) writeWorktreeRmSuccess(w http.ResponseWriter) {
	s.writeJSON(w, http.StatusOK, WorktreeRmResponse{
		responseEnvelope: newSuccessEnvelope(""),
	})
}

func (s *Server) writeWorktreeRebaseError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	s.writeJSON(w, status, WorktreeRebaseResponse{
		responseEnvelope: newErrorEnvelope(requestID, code, message, hint),
	})
}

func (s *Server) writeWorktreeRebaseSuccess(w http.ResponseWriter, requestID string, record *store.IntegrationWorktreeRecord) {
	s.writeJSON(w, http.StatusOK, WorktreeRebaseResponse{
		responseEnvelope: newSuccessEnvelope(requestID),
		RepoID:                   record.RepoID,
		IntegrationWorktreeID:    record.WorktreeID,
		Branch:                   record.Meta.Branch,
		BaseBranch:               record.Meta.BaseBranch,
	})
}

func (s *Server) writeWorktreePRSyncError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	s.writeJSON(w, status, WorktreePRSyncResponse{
		responseEnvelope: newErrorEnvelope(requestID, code, message, hint),
	})
}

func (s *Server) writeWorktreePRSyncSuccess(w http.ResponseWriter, requestID string, record *store.IntegrationWorktreeRecord, result *prSyncResult) {
	s.writeJSON(w, http.StatusOK, WorktreePRSyncResponse{
		responseEnvelope: newSuccessEnvelope(requestID),
		RepoID:                   record.RepoID,
		IntegrationWorktreeID:    record.WorktreeID,
		Branch:                   result.Branch,
		PRNumber:                 result.PRNumber,
		PRURL:                    result.PRURL,
		PRAction:                 result.PRAction,
	})
}

func (s *Server) writeWorktreeMergeError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	s.writeJSON(w, status, WorktreePRMergeResponse{
		responseEnvelope: newErrorEnvelope(requestID, code, message, hint),
	})
}

func (s *Server) worktreePRMergeResponse(
	record *store.IntegrationWorktreeRecord,
	requestID string,
	action string,
	mergeMeta *store.IntegrationWorktreeMergeMeta,
) *WorktreePRMergeResponse {
	return &WorktreePRMergeResponse{
		responseEnvelope: newSuccessEnvelope(requestID),
		Action:                   action,
		RepoID:                   record.RepoID,
		IntegrationWorktreeID:    record.WorktreeID,
		Merge:                    worktreeMergeMetaToDTO(mergeMeta),
	}
}
