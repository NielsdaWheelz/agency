package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// handleCheckpointApply handles POST /invocations/{ref}/checkpoints/apply.
func (s *Server) handleCheckpointApply(w http.ResponseWriter, r *http.Request, invocationID string) {
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)

	// Read repo_id from query params
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeCheckpointError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	// Parse request body
	var req CheckpointApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeCheckpointError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "")
		return
	}

	if req.CheckpointID <= 0 {
		s.writeCheckpointError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", "checkpoint_id must be positive", "")
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationID, repoID)
	if resolveErr != nil {
		code := errors.GetCode(resolveErr)
		if code == "" {
			code = errors.EInvocationNotFound
		}
		status := http.StatusNotFound
		if code == errors.EInvocationIDAmbiguous {
			status = http.StatusConflict
		}
		s.writeCheckpointError(w, status, requestID, string(code), resolveErr.Error(), "use 'agency agent ls --repo <repo>' to list invocations")
		return
	}

	// Repo-scoped lock serializes rollback mutations with other git-mutating flows.
	unlock, err := s.repoLock.Lock(record.RepoID, "checkpoint_apply")
	if err != nil {
		s.writeCheckpointError(
			w,
			http.StatusConflict,
			requestID,
			string(errors.ERepoLocked),
			"repository is locked by another operation",
			"wait for the other operation to complete",
		)
		return
	}
	defer func() { _ = unlock() }()

	// Read invocation meta
	meta, err := s.Store.ReadInvocationMeta(record.RepoID, record.InvocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.writeCheckpointError(w, http.StatusNotFound, requestID, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		s.writeCheckpointError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}

	// Only for headless invocations
	if meta.Mode != store.RunnerModeHeadless {
		s.writeCheckpointError(w, http.StatusBadRequest, requestID, string(errors.EInvocationInvalidMode),
			"checkpoint apply is only supported for headless invocations",
			"use 'agency agent recreate' to start a new headed tmux session in the same sandbox")
		return
	}

	// Precondition: invocation must be finished or failed
	if meta.Status == store.InvocationStatusRunning || meta.Status == store.InvocationStatusStarting {
		s.writeCheckpointError(w, http.StatusConflict, requestID, string(errors.EInvocationStillRunning),
			"invocation is still running",
			"stop the invocation first with 'agency agent stop' or 'agency agent kill'")
		return
	}

	// Get sandbox path and checkpoints directory
	sandboxPath := meta.SandboxPath
	checkpointsDir := s.Store.InvocationDir(record.RepoID, record.InvocationID)
	eventsPath := s.Store.InvocationEventsPath(record.RepoID, record.InvocationID)

	// Create applier and apply checkpoint
	applier := checkpoint.NewApplierWithWriter(
		record.InvocationID,
		sandboxPath,
		checkpointsDir,
		eventsPath,
		s.Runner,
		s.FS,
		s.Clock,
		s.InvocationEvents,
	)

	cp, err := applier.Apply(r.Context(), req.CheckpointID)
	if err != nil {
		switch errors.GetCode(err) {
		case errors.ECheckpointNotFound:
			s.writeCheckpointError(w, http.StatusNotFound, requestID, string(errors.ECheckpointNotFound),
				err.Error(), "run 'agency agent checkpoint ls' to see available checkpoints")
		case errors.ERollbackFailed:
			s.writeCheckpointError(w, http.StatusInternalServerError, requestID, string(errors.ERollbackFailed),
				err.Error(), "")
		default:
			s.writeCheckpointError(w, http.StatusInternalServerError, requestID, string(errors.ECheckpointFailed),
				err.Error(), "")
		}
		return
	}

	// Return success
	resp := CheckpointApplyResponse{
		OK:             true,
		APIVersion:     APIVersion,
		BuildVersion:   version.FullVersion(),
		RequestID:      requestID,
		CheckpointID:   cp.ID,
		SnapshotCommit: cp.SnapshotCommit,
		RestoredAt:     s.Clock().UTC().Format("2006-01-02T15:04:05Z"),
	}
	s.writeJSON(w, http.StatusOK, resp)
}
