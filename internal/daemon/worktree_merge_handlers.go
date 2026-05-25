package daemon

import (
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// handleWorktreePRMerge handles POST /worktrees/{ref}/pr/merge.
func (s *Server) handleWorktreePRMerge(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := prepareRequestID(w, r)

	var req WorktreePRMergeRequest
	if err := decodeOptionalStrictJSON(r.Body, &req); err != nil {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "")
		return
	}

	normalizedReq, err := normalizeMergeRequest(req)
	if err != nil {
		code := errors.CodeOr(err, errors.EInvalidArgument)
		s.writeErrorWithRequestID(w, httpStatusForCode(code), requestID, string(code), err.Error(), errors.Hint(err))
		return
	}

	record, ok := s.resolveWorktreeFromQuery(w, r, worktreeRef, requestID)
	if !ok {
		return
	}
	if record.Meta.State != store.WorktreeStatePresent {
		mergeMeta, readErr := s.store.ReadIntegrationWorktreeMerge(record.RepoID, record.WorktreeID)
		if readErr != nil {
			code := errors.CodeOr(readErr, errors.EStoreCorrupt)
			s.writeErrorWithRequestID(w, httpStatusForCode(code), requestID, string(code), apiErrorMessage(readErr), "inspect worktree merge state")
			return
		}
		if record.Meta.State != store.WorktreeStateArchived || worktreeRef != record.WorktreeID || mergeMeta == nil || mergeMeta.Status == store.WorktreeMergeStatusSucceeded || mergeMeta.Stage != store.WorktreeMergeStageArchive {
			s.writeErrorWithRequestID(w, http.StatusNotFound, requestID, string(errors.EWorktreeNotFound), "integration worktree is archived", "archived worktree merge cleanup retries must use the exact worktree_id")
			return
		}
	}

	resp, status, err := s.startWorktreePRMerge(record, worktreeRef, requestID, normalizedReq)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		s.writeErrorWithRequestID(w, httpStatusForCode(code), requestID, string(code), apiErrorMessage(err), errors.Hint(err))
		return
	}

	s.writeJSON(w, status, *resp)
}
