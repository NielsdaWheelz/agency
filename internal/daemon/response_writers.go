package daemon

import (
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

func (s *Server) writeControlPlaneError(w http.ResponseWriter, status int, requestID, code, message, hint, clientRequestID string) {
	s.writeJSON(w, status, ControlPlaneStartResponse{
		OK:              false,
		ErrorCode:       code,
		Message:         message,
		Hint:            hint,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: clientRequestID,
	})
}

func (s *Server) writeControlPlaneSuccess(w http.ResponseWriter, invocationID string, meta *store.InvocationMeta, repoID, clientRequestID, requestID string, alreadyRunning bool) {
	resp := ControlPlaneStartResponse{
		OK:                    true,
		InvocationID:          invocationID,
		SandboxPath:           meta.SandboxPath,
		RepoID:                repoID,
		IntegrationWorktreeID: meta.IntegrationWorktreeID,
		AlreadyRunning:        alreadyRunning,
		RequestID:             requestID,
		APIVersion:            APIVersion,
		BuildVersion:          version.FullVersion(),
		ClientRequestID:       clientRequestID,
		DaemonInstanceID:      s.InstanceID,
	}
	if meta.PID != nil {
		resp.PID = *meta.PID
	}
	if meta.PGID != nil {
		resp.PGID = *meta.PGID
	}
	resp.LogPaths = &LogPaths{
		Raw:      s.readableInvocationLogPath(repoID, invocationID, "raw"),
		Stderr:   s.readableInvocationLogPath(repoID, invocationID, "stderr"),
		Stream:   s.readableInvocationLogPath(repoID, invocationID, "stream"),
		Hooks:    s.readableInvocationLogPath(repoID, invocationID, "hooks"),
		Terminal: s.readableInvocationLogPath(repoID, invocationID, "terminal"),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) writeHeadedError(w http.ResponseWriter, status int, code, message, hint, clientRequestID, requestID string) {
	s.writeJSON(w, status, ControlPlaneStartHeadedResponse{
		OK:              false,
		ErrorCode:       code,
		Message:         message,
		Hint:            hint,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    version.FullVersion(),
		GitSHA:          version.Commit,
		ClientRequestID: clientRequestID,
	})
}

func (s *Server) writeHeadedSuccess(w http.ResponseWriter, invocationID string, meta *store.InvocationMeta, repoID, clientRequestID, requestID string, alreadyRunning bool) {
	resp := ControlPlaneStartHeadedResponse{
		OK:                    true,
		InvocationID:          invocationID,
		SandboxPath:           meta.SandboxPath,
		RepoID:                repoID,
		IntegrationWorktreeID: meta.IntegrationWorktreeID,
		TmuxSession:           meta.TmuxSession,
		AlreadyRunning:        alreadyRunning,
		RequestID:             requestID,
		APIVersion:            APIVersion,
		BuildVersion:          version.FullVersion(),
		GitSHA:                version.Commit,
		ClientRequestID:       clientRequestID,
		DaemonInstanceID:      s.InstanceID,
		LogPaths: &LogPaths{
			Raw:      s.readableInvocationLogPath(repoID, invocationID, "raw"),
			Stderr:   s.readableInvocationLogPath(repoID, invocationID, "stderr"),
			Stream:   s.readableInvocationLogPath(repoID, invocationID, "stream"),
			Hooks:    s.readableInvocationLogPath(repoID, invocationID, "hooks"),
			Terminal: s.readableInvocationLogPath(repoID, invocationID, "terminal"),
		},
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) writeFollowUpError(w http.ResponseWriter, status int, requestID, code, message, hint, clientRequestID string) {
	resp := ControlPlaneFollowUpResponse{
		OK:              false,
		ErrorCode:       code,
		Message:         message,
		Hint:            hint,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: clientRequestID,
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) writeFollowUpSuccessWithDelivery(w http.ResponseWriter, invocationID, timelineEntryID, clientRequestID, requestID string, alreadyApplied bool, deliveryMode string) {
	resp := ControlPlaneFollowUpResponse{
		OK:              true,
		InvocationID:    invocationID,
		TimelineEntry:   timelineEntryID,
		AlreadyApplied:  alreadyApplied,
		DeliveryMode:    deliveryMode,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: clientRequestID,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) writeCheckpointError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	resp := CheckpointApplyResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		RequestID:    requestID,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) writeLandError(w http.ResponseWriter, status int, requestID, code, message, hint string, conflictFiles []string) {
	resp := LandResponse{
		OK:            false,
		APIVersion:    APIVersion,
		BuildVersion:  version.FullVersion(),
		RequestID:     requestID,
		ErrorCode:     code,
		Message:       message,
		Hint:          hint,
		ConflictFiles: conflictFiles,
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) writeDiscardError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	resp := DiscardResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		RequestID:    requestID,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) writeWorktreeError(w http.ResponseWriter, status int, code, message, hint string) {
	resp := WorktreeCreateResponse{
		OK:           false,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) writeWorktreeSuccess(w http.ResponseWriter, worktreeID, treePath, branch, repoID string) {
	resp := WorktreeCreateResponse{
		OK:           true,
		WorktreeID:   worktreeID,
		TreePath:     treePath,
		Branch:       branch,
		RepoID:       repoID,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) writeWorktreeRmError(w http.ResponseWriter, status int, code, message, hint string) {
	resp := WorktreeRmResponse{
		OK:           false,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) writeWorktreeRmSuccess(w http.ResponseWriter) {
	resp := WorktreeRmResponse{
		OK:           true,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) writeWorktreeRebaseError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	s.writeJSON(w, status, WorktreeRebaseResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		RequestID:    requestID,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	})
}

func (s *Server) writeWorktreePRSyncError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	resp := WorktreePRSyncResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		RequestID:    requestID,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) writeWorktreeMergeError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	resp := WorktreePRMergeResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		RequestID:    requestID,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) writeInvocationActionSuccess(w http.ResponseWriter, requestID, invocationID string) {
	s.writeJSON(w, http.StatusOK, InvocationActionResponse{
		OK:           true,
		InvocationID: invocationID,
		RequestID:    requestID,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
	})
}
