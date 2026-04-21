package daemon

import (
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// handleGetInvocationSession handles GET /invocations/{ref}/session.
func (s *Server) handleGetInvocationSession(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)
	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	repoID := r.URL.Query().Get("repo_id")
	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agent ls' to list invocations", errors.EInvocationIDAmbiguous)
		return
	}
	if record.Meta == nil || record.Meta.Mode != store.RunnerModeHeaded {
		s.writeAPIError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvocationInvalidMode),
			"session reads are only supported for headed invocations",
			"use 'agency agent <invocation-ref> history' to inspect or 'agency agent <invocation-ref> restore' to roll back a headless invocation",
			nil,
		)
		return
	}
	if s.TmuxClient == nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.ETmuxFailed), "tmux client is not configured", "", nil)
		return
	}

	sessionName := strings.TrimSpace(record.Meta.TmuxSession)
	if sessionName == "" {
		sessionName = tmux.SessionName(record.InvocationID)
	}

	data := InvocationSessionData{
		InvocationID:      record.InvocationID,
		RepoID:            record.RepoID,
		SessionStatus:     "missing",
		TmuxSession:       sessionName,
		ClientCount:       0,
		ConnectedClients:  []InvocationSessionClient{},
		RecreateAvailable: s.invocationSessionRecreateAvailable(record),
	}

	exists, err := s.TmuxClient.HasSession(r.Context(), sessionName)
	if err != nil && !tmux.IsNoSessionErr(err) {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.ETmuxFailed), "failed to inspect tmux session: "+err.Error(), "", nil)
		return
	}
	if !exists {
		s.writeAPIResponse(w, requestID, data)
		return
	}

	clients, err := s.TmuxClient.ListAttachedClients(r.Context(), sessionName)
	if err != nil {
		if tmux.IsNoSessionErr(err) {
			s.writeAPIResponse(w, requestID, data)
			return
		}
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.ETmuxFailed), "failed to inspect tmux clients: "+err.Error(), "", nil)
		return
	}

	data.SessionStatus = "live"
	data.RecreateAvailable = false
	data.ClientCount = len(clients)
	data.AttachCommand = "agency agent " + record.InvocationID + " attach --repo " + record.RepoID
	for _, client := range clients {
		data.ConnectedClients = append(data.ConnectedClients, InvocationSessionClient{
			Name:     client.Name,
			TTY:      client.TTY,
			PID:      client.PID,
			ReadOnly: client.ReadOnly,
		})
	}

	s.writeAPIResponse(w, requestID, data)
}

func (s *Server) invocationSessionRecreateAvailable(record *resolvedInvocation) bool {
	if record == nil || record.Meta == nil {
		return false
	}
	if record.Meta.Mode != store.RunnerModeHeaded {
		return false
	}
	if record.Meta.LandingStatus == store.LandingStatusLanded || record.Meta.LandingStatus == store.LandingStatusDiscarded {
		return false
	}
	sandboxPath := strings.TrimSpace(record.Meta.SandboxPath)
	if sandboxPath == "" {
		return false
	}
	info, err := s.FS.Stat(sandboxPath)
	if err != nil {
		return false
	}
	return info.IsDir()
}
