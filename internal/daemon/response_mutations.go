package daemon

import "net/http"

func (s *Server) writeCheckpointError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	s.writeJSON(w, status, CheckpointApplyResponse{
		responseEnvelope: newErrorEnvelope(requestID, code, message, hint),
	})
}

func (s *Server) writeCheckpointSuccess(w http.ResponseWriter, requestID string, checkpointID int, snapshotCommit, restoredAt string) {
	s.writeJSON(w, http.StatusOK, CheckpointApplyResponse{
		responseEnvelope: newSuccessEnvelope(requestID),
		CheckpointID:     checkpointID,
		SnapshotCommit:   snapshotCommit,
		RestoredAt:       restoredAt,
	})
}

func (s *Server) writeLandError(w http.ResponseWriter, status int, requestID, code, message, hint string, conflictFiles []string) {
	s.writeJSON(w, status, LandResponse{
		responseEnvelope: newErrorEnvelope(requestID, code, message, hint),
		ConflictFiles:    conflictFiles,
	})
}

func (s *Server) writeLandSuccess(w http.ResponseWriter, requestID, invocationID string, mode LandingMode, headBefore, headAfter string, commitsLanded int) {
	s.writeJSON(w, http.StatusOK, LandResponse{
		responseEnvelope:      newSuccessEnvelope(requestID),
		InvocationID:          invocationID,
		AppliedMode:           mode,
		IntegrationHeadBefore: headBefore,
		IntegrationHeadAfter:  headAfter,
		CommitsLanded:         commitsLanded,
	})
}

func (s *Server) writeDiscardError(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	s.writeJSON(w, status, DiscardResponse{
		responseEnvelope: newErrorEnvelope(requestID, code, message, hint),
	})
}

func (s *Server) writeDiscardSuccess(w http.ResponseWriter, requestID, invocationID string) {
	s.writeJSON(w, http.StatusOK, DiscardResponse{
		responseEnvelope: newSuccessEnvelope(requestID),
		InvocationID:     invocationID,
	})
}
