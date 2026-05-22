package daemon

import (
	"fmt"
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

// handleGetInvocationCheckpoints handles GET /invocations/{ref}/checkpoints.
func (s *Server) handleGetInvocationCheckpoints(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)

	record, resolveErr := s.resolveInvocationRef(invocationRef, r.URL.Query().Get("repo_id"))
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agent ls' to list invocations", errors.EInvocationIDAmbiguous)
		return
	}

	params, invalid := parseListCheckpointsParams(r)
	if invalid != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidArgument),
			fmt.Sprintf("invalid value for parameter '%s': %q", invalid.Param, invalid.Value), "",
			*invalid)
		return
	}

	checkpointsPath := s.store.InvocationCheckpointsPath(record.RepoID, record.InvocationID)
	cpFile, err := checkpoint.LoadCheckpointsFile(s.fsys, checkpointsPath)
	if err != nil {
		code := errors.CodeOr(err, errors.EStoreCorrupt)
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(code), err.Error(), "", nil)
		return
	}
	if cpFile == nil {
		s.writeAPIResponse(w, requestID, ListCheckpointsData{Checkpoints: []CheckpointDTO{}, NextCursor: ""})
		return
	}

	allCheckpoints := make([]CheckpointDTO, 0, len(cpFile.Checkpoints))
	for i := len(cpFile.Checkpoints) - 1; i >= 0; i-- {
		allCheckpoints = append(allCheckpoints, checkpointToDTO(cpFile.Checkpoints[i]))
	}

	checkpoints, nextCursor := paginateCheckpoints(allCheckpoints, params.Cursor, params.Limit)
	s.writeAPIResponse(w, requestID, ListCheckpointsData{Checkpoints: checkpoints, NextCursor: nextCursor})
}
