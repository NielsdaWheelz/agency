package daemon

import (
	"net/http"
	"slices"
	"strings"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

func apiErrorMessage(err error) string {
	if ae, ok := agencyerrors.AsAgencyError(err); ok {
		if msg := strings.TrimSpace(ae.Msg); msg != "" {
			return msg
		}
	}
	return err.Error()
}

func (s *Server) writeControlPlaneError(w http.ResponseWriter, status int, requestID, code, message, hint, clientRequestID string) {
	s.writeJSON(w, status, ControlPlaneStartResponse{
		OK:              false,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    daemonBuildVersion(),
		ClientRequestID: clientRequestID,
		ErrorCode:       code,
		Message:         message,
		Hint:            hint,
	})
}

func (s *Server) writeControlPlaneSuccess(w http.ResponseWriter, invocationID string, meta *store.InvocationMeta, repoID, clientRequestID, requestID string, alreadyRunning bool) {
	resp := ControlPlaneStartResponse{
		OK:               true,
		InvocationID:     invocationID,
		SandboxPath:      meta.SandboxPath,
		RepoID:           repoID,
		RepoName:         s.repoName(repoID),
		WorktreeID:       meta.IntegrationWorktreeID,
		WorktreeName:     s.worktreeNameForResponse(repoID, meta.IntegrationWorktreeID),
		ExecutionProfile: meta.ExecutionProfile,
		CheckoutRoot:     meta.CheckoutRoot,
		CustomEnvKeys:    slices.Clone(meta.CustomEnvKeys),
		DaemonInstanceID: s.instanceID,
		AlreadyRunning:   alreadyRunning,
		LogPaths:         s.invocationLogPaths(repoID, invocationID),
		RequestID:        requestID,
		APIVersion:       APIVersion,
		BuildVersion:     daemonBuildVersion(),
		ClientRequestID:  clientRequestID,
	}
	if meta.PID != nil {
		resp.PID = *meta.PID
	}
	if meta.PGID != nil {
		resp.PGID = *meta.PGID
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) writeHeadedError(w http.ResponseWriter, status int, code, message, hint, clientRequestID, requestID string) {
	s.writeJSON(w, status, ControlPlaneStartHeadedResponse{
		OK:              false,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    daemonBuildVersion(),
		GitSHA:          version.Commit,
		ClientRequestID: clientRequestID,
		ErrorCode:       code,
		Message:         message,
		Hint:            hint,
	})
}

func (s *Server) writeHeadedSuccess(w http.ResponseWriter, invocationID string, meta *store.InvocationMeta, repoID, clientRequestID, requestID string, alreadyRunning bool) {
	resp := ControlPlaneStartHeadedResponse{
		OK:               true,
		InvocationID:     invocationID,
		SandboxPath:      meta.SandboxPath,
		RepoID:           repoID,
		RepoName:         s.repoName(repoID),
		WorktreeID:       meta.IntegrationWorktreeID,
		WorktreeName:     s.worktreeNameForResponse(repoID, meta.IntegrationWorktreeID),
		ExecutionProfile: meta.ExecutionProfile,
		CheckoutRoot:     meta.CheckoutRoot,
		CustomEnvKeys:    slices.Clone(meta.CustomEnvKeys),
		TmuxSession:      meta.TmuxSession,
		DaemonInstanceID: s.instanceID,
		AlreadyRunning:   alreadyRunning,
		LogPaths:         s.invocationLogPaths(repoID, invocationID),
		RequestID:        requestID,
		APIVersion:       APIVersion,
		BuildVersion:     daemonBuildVersion(),
		GitSHA:           version.Commit,
		ClientRequestID:  clientRequestID,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) writeFollowUpError(w http.ResponseWriter, status int, requestID, code, message, hint, clientRequestID string) {
	s.writeJSON(w, status, ControlPlaneFollowUpResponse{
		OK:              false,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    daemonBuildVersion(),
		ClientRequestID: clientRequestID,
		ErrorCode:       code,
		Message:         message,
		Hint:            hint,
	})
}

func (s *Server) writeInvocationActionSuccess(w http.ResponseWriter, requestID, invocationID string) {
	s.writeJSON(w, http.StatusOK, InvocationActionResponse{
		OK:           true,
		InvocationID: invocationID,
		RequestID:    requestID,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
	})
}
