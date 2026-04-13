package daemon

import (
	"net/http"
	"os"
	"strconv"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

// handleGetInvocationCheckpoints handles GET /invocations/{ref}/checkpoints.
func (s *Server) handleGetInvocationCheckpoints(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationRef, r.URL.Query().Get("repo_id"))
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agent ls' to list invocations", errors.EInvocationIDAmbiguous)
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	cursor := r.URL.Query().Get("cursor")

	checkpointsDir := s.Store.InvocationDir(record.RepoID, record.InvocationID)
	cpFile, err := checkpoint.LoadCheckpointsFile(s.FS, checkpointsDir)
	if err != nil {
		if os.IsNotExist(err) {
			s.writeAPIResponse(w, requestID, ListCheckpointsData{Checkpoints: []CheckpointDTO{}, NextCursor: ""})
			return
		}
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", err.Error(), "", nil)
		return
	}
	if cpFile == nil {
		s.writeAPIResponse(w, requestID, ListCheckpointsData{Checkpoints: []CheckpointDTO{}, NextCursor: ""})
		return
	}

	allCheckpoints := make([]CheckpointDTO, 0, len(cpFile.Checkpoints))
	for i := len(cpFile.Checkpoints) - 1; i >= 0; i-- {
		cp := cpFile.Checkpoints[i]
		allCheckpoints = append(allCheckpoints, CheckpointDTO{
			ID:                   cp.ID,
			CreatedAt:            cp.CreatedAt,
			Diffstat:             cp.Diffstat,
			SnapshotCommit:       cp.SnapshotCommit,
			IncludesUntracked:    cp.IncludesUntracked,
			Degraded:             !cp.IncludesUntracked,
			Trigger:              cp.Trigger,
			ToolName:             cp.ToolName,
			StreamSeq:            cp.StreamSeq,
			Description:          cp.Description,
			ChangedPaths:         cp.ChangedPaths,
			ChangedPathCount:     cp.ChangedPathCount,
			ChangedPathTruncated: cp.ChangedPathTruncated,
		})
	}

	checkpoints, nextCursor := paginateCheckpoints(allCheckpoints, cursor, limit)
	s.writeAPIResponse(w, requestID, ListCheckpointsData{Checkpoints: checkpoints, NextCursor: nextCursor})
}
