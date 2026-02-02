package daemon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// handleCheckpoints handles requests to /invocations/{id}/checkpoints/...
func (s *Server) handleCheckpoints(w http.ResponseWriter, r *http.Request, invocationID string) {
	// Parse path: /invocations/{id}/checkpoints/{action}
	path := r.URL.Path
	checkpointsPrefix := "/invocations/" + invocationID + "/checkpoints/"

	if !strings.HasPrefix(path, checkpointsPrefix) {
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "endpoint not found", "use /invocations/{id}/checkpoints/apply")
		return
	}

	action := strings.TrimPrefix(path, checkpointsPrefix)

	switch action {
	case "apply":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleCheckpointApply(w, r, invocationID)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown action: "+action, "supported actions: apply")
	}
}

// handleCheckpointApply handles POST /invocations/{id}/checkpoints/apply.
func (s *Server) handleCheckpointApply(w http.ResponseWriter, r *http.Request, invocationID string) {
	// Read repo_id from query params
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeCheckpointError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	// Parse request body
	var req CheckpointApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeCheckpointError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "")
		return
	}

	if req.CheckpointID <= 0 {
		s.writeCheckpointError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "checkpoint_id must be positive", "")
		return
	}

	// Read invocation meta
	meta, err := s.Store.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.writeCheckpointError(w, http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		s.writeCheckpointError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}

	// Only for headless invocations
	if meta.Mode != store.RunnerModeHeadless {
		s.writeCheckpointError(w, http.StatusBadRequest, string(errors.EInvocationInvalidMode),
			"checkpoint apply is only supported for headless invocations",
			"headed invocations do not have automated checkpoints")
		return
	}

	// Precondition: invocation must be finished or failed
	if meta.Status == store.InvocationStatusRunning || meta.Status == store.InvocationStatusStarting {
		s.writeCheckpointError(w, http.StatusConflict, string(errors.EInvocationStillRunning),
			"invocation is still running",
			"stop the invocation first with 'agency agent stop' or 'agency agent kill'")
		return
	}

	// Get sandbox path and checkpoints directory
	sandboxPath := meta.SandboxPath
	checkpointsDir := s.Store.SandboxDir(repoID, invocationID)
	eventsPath := s.Store.InvocationEventsPath(repoID, invocationID)

	// Create applier and apply checkpoint
	applier := checkpoint.NewApplier(
		invocationID,
		sandboxPath,
		checkpointsDir,
		eventsPath,
		s.Runner,
		s.FS,
		s.Clock,
	)

	cp, err := applier.Apply(r.Context(), req.CheckpointID)
	if err != nil {
		switch errors.GetCode(err) {
		case errors.ECheckpointNotFound:
			s.writeCheckpointError(w, http.StatusNotFound, string(errors.ECheckpointNotFound),
				err.Error(), "run 'agency checkpoint ls' to see available checkpoints")
		case errors.ERollbackFailed:
			s.writeCheckpointError(w, http.StatusInternalServerError, string(errors.ERollbackFailed),
				err.Error(), "")
		default:
			s.writeCheckpointError(w, http.StatusInternalServerError, string(errors.ECheckpointFailed),
				err.Error(), "")
		}
		return
	}

	// Return success
	resp := CheckpointApplyResponse{
		OK:             true,
		APIVersion:     APIVersion,
		BuildVersion:   version.FullVersion(),
		CheckpointID:   cp.ID,
		SnapshotCommit: cp.SnapshotCommit,
		RestoredAt:     s.Clock().UTC().Format("2006-01-02T15:04:05Z"),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// writeCheckpointError writes an error response for checkpoint endpoints.
func (s *Server) writeCheckpointError(w http.ResponseWriter, status int, code, message, hint string) {
	resp := CheckpointApplyResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	}
	s.writeJSON(w, status, resp)
}
