package daemon

import "net/http"

func (s *Server) writeCheckpointError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	s.writeJSON(w, status, CheckpointApplyResponse{
		OK:           false,
		RequestID:    requestID,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	})
}

func (s *Server) writeCheckpointSuccess(w http.ResponseWriter, requestID string, checkpointID int, snapshotCommit, restoredAt string) {
	s.writeJSON(w, http.StatusOK, CheckpointApplyResponse{
		OK:             true,
		RequestID:      requestID,
		APIVersion:     APIVersion,
		BuildVersion:   daemonBuildVersion(),
		CheckpointID:   checkpointID,
		SnapshotCommit: snapshotCommit,
		RestoredAt:     restoredAt,
	})
}

func (s *Server) writeLandError(w http.ResponseWriter, status int, requestID, code, message, hint string, conflictFiles []string) {
	s.writeJSON(w, status, LandResponse{
		OK:            false,
		RequestID:     requestID,
		APIVersion:    APIVersion,
		BuildVersion:  daemonBuildVersion(),
		ErrorCode:     code,
		Message:       message,
		Hint:          hint,
		ConflictFiles: conflictFiles,
	})
}

func (s *Server) writeLandSuccess(w http.ResponseWriter, requestID, invocationID string, mode LandingMode, headBefore, headAfter string, commitsLanded int) {
	s.writeJSON(w, http.StatusOK, LandResponse{
		OK:                    true,
		RequestID:             requestID,
		APIVersion:            APIVersion,
		BuildVersion:          daemonBuildVersion(),
		InvocationID:          invocationID,
		AppliedMode:           mode,
		IntegrationHeadBefore: headBefore,
		IntegrationHeadAfter:  headAfter,
		CommitsLanded:         commitsLanded,
	})
}

func (s *Server) writeDiscardError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	s.writeJSON(w, status, DiscardResponse{
		OK:           false,
		RequestID:    requestID,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	})
}

func (s *Server) writeDiscardSuccess(w http.ResponseWriter, requestID, invocationID string) {
	s.writeJSON(w, http.StatusOK, DiscardResponse{
		OK:           true,
		RequestID:    requestID,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		InvocationID: invocationID,
	})
}
