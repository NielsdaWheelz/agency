package daemon

import (
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// handleCheckpointApply handles POST /invocations/{ref}/checkpoints/apply.
func (s *Server) handleCheckpointApply(w http.ResponseWriter, r *http.Request, invocationID string) {
	requestID := prepareRequestID(w, r)

	// Read repo_id from query params
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "repo_id query parameter is required", "")
		return
	}

	// Parse request body
	var req CheckpointApplyRequest
	if err := decodeStrictJSON(r.Body, &req); err != nil {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "")
		return
	}

	if req.CheckpointID <= 0 {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidArgument), "checkpoint_id must be positive", "")
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationID, repoID)
	if resolveErr != nil {
		status, code := invocationResolveStatus(resolveErr)
		s.writeErrorWithRequestID(w, status, requestID, string(code), resolveErr.Error(), "use 'agency agent ls --repo <repo>' to list invocations")
		return
	}

	// Repo-scoped lock serializes rollback mutations with other git-mutating flows.
	unlock, err := s.repoLock.Lock(record.RepoID, "checkpoint_apply")
	if err != nil {
		s.writeErrorWithRequestID(
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
	meta, err := s.store.ReadInvocationMeta(record.RepoID, record.InvocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.writeErrorWithRequestID(w, http.StatusNotFound, requestID, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to read invocation meta: "+err.Error(), "")
		return
	}

	// Only for headless invocations
	if meta.Mode != store.RunnerModeHeadless {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvocationInvalidMode),
			"checkpoint apply is only supported for headless invocations",
			"use 'agency agent <invocation-ref> recreate' to start a new headed tmux session in the same sandbox")
		return
	}

	// Precondition: invocation must already be terminal.
	if meta.Status == store.InvocationStatusStarting ||
		meta.Status == store.InvocationStatusRunning ||
		meta.Status == store.InvocationStatusStopping {
		s.writeErrorWithRequestID(w, http.StatusConflict, requestID, string(errors.EInvocationStillRunning),
			"invocation is still active",
			"stop the invocation first with 'agency agent <invocation-ref> stop' or 'agency agent <invocation-ref> kill'")
		return
	}

	// Get sandbox path and checkpoints directory
	sandboxPath := meta.SandboxPath
	checkpointsDir := s.store.InvocationDir(record.RepoID, record.InvocationID)
	eventsPath := s.store.InvocationEventsPath(record.RepoID, record.InvocationID)
	profileEnv, err := s.executionProfileEnv(meta.ExecutionProfile)
	if err != nil {
		code := errors.CodeOr(err, errors.EExecutionProfileNotFound)
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(code), apiErrorMessage(err), "")
		return
	}

	// Create applier and apply checkpoint
	applier := checkpoint.NewApplierWithWriter(
		record.InvocationID,
		sandboxPath,
		checkpointsDir,
		eventsPath,
		s.runner,
		s.fsys,
		s.clock,
		s.invocationEvents,
	)

	cp, err := applier.ApplyWithOptions(r.Context(), req.CheckpointID, checkpoint.ApplyOptions{Env: prSyncNonInteractiveEnv(profileEnv)})
	if err != nil {
		switch errors.GetCode(err) {
		case errors.ECheckpointNotFound:
			s.writeErrorWithRequestID(w, http.StatusNotFound, requestID, string(errors.ECheckpointNotFound),
				err.Error(), "run 'agency agent <invocation_ref> history' to inspect available checkpoints and turn ids")
		case errors.ERollbackFailed:
			s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.ERollbackFailed),
				err.Error(), "")
		case errors.EStoreCorrupt:
			s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EStoreCorrupt),
				err.Error(), "")
		default:
			s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.ECheckpointFailed),
				err.Error(), "")
		}
		return
	}

	s.writeCheckpointSuccess(w, requestID, cp.ID, cp.SnapshotCommit, s.clock().UTC().Format("2006-01-02T15:04:05Z"))
}
