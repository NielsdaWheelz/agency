package daemon

import "net/http"

func (s *Server) writeCheckpointSuccess(w http.ResponseWriter, requestID string, checkpointID int, snapshotCommit, restoredAt string) {
	s.writeJSON(w, http.StatusOK, CheckpointApplyResponse{
		ResponseEnvelope: NewSuccessEnvelope(requestID),
		CheckpointID:     checkpointID,
		SnapshotCommit:   snapshotCommit,
		RestoredAt:       restoredAt,
	})
}

func (s *Server) writeLandError(w http.ResponseWriter, status int, requestID, code, message, hint string, conflictFiles []string) {
	s.writeJSON(w, status, LandResponse{
		ResponseEnvelope: NewErrorEnvelope(requestID, code, message, hint),
		ConflictFiles:    conflictFiles,
	})
}

func (s *Server) writeLandSuccess(w http.ResponseWriter, requestID, invocationID string, mode LandingMode, headBefore, headAfter string, commitsLanded int) {
	s.writeJSON(w, http.StatusOK, LandResponse{
		ResponseEnvelope:      NewSuccessEnvelope(requestID),
		InvocationID:          invocationID,
		AppliedMode:           mode,
		IntegrationHeadBefore: headBefore,
		IntegrationHeadAfter:  headAfter,
		CommitsLanded:         commitsLanded,
	})
}

func (s *Server) writeDiscardSuccess(w http.ResponseWriter, requestID, invocationID string) {
	s.writeJSON(w, http.StatusOK, DiscardResponse{
		ResponseEnvelope: NewSuccessEnvelope(requestID),
		InvocationID:     invocationID,
	})
}
