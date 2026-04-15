package daemon

import (
	"net/http"
	"strings"
)

func (s *Server) handleInvocations(w http.ResponseWriter, r *http.Request) {
	remaining, ok := trimRoutePrefix(r.URL.Path, "/invocations/")
	if !ok {
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "not found", "")
		return
	}
	if remaining == "" {
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleListInvocations(w, r)
		return
	}

	if remaining == "start_headless" {
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleControlPlaneStartHeadless(w, r)
		return
	}

	if remaining == "start_headed" {
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleControlPlaneStartHeaded(w, r)
		return
	}

	invocationRef, action := splitRouteRefAction(remaining)
	if invocationRef == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invocation ref required", "")
		return
	}

	switch routeFirstAction(action) {
	case "":
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocation(w, r, invocationRef)
	case "stop":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleStop(w, r, invocationRef)
	case "kill":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleKill(w, r, invocationRef)
	case "checkpoints":
		s.handleCheckpointsRoute(w, r, invocationRef, action)
	case "restart":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleRestartFromCheckpoint(w, r, invocationRef)
	case "land":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleLand(w, r, invocationRef)
	case "discard":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleDiscard(w, r, invocationRef)
	case "diff":
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocationDiff(w, r, invocationRef)
	case "logs":
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocationLogs(w, r, invocationRef)
	case "headed_hook":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleHeadedHook(w, r, invocationRef)
	case "timeline":
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocationTimeline(w, r, invocationRef)
	case "review":
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocationReview(w, r, invocationRef)
	case "chat":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleControlPlaneFollowUpPrompt(w, r, invocationRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown action: "+action, "")
	}
}

func (s *Server) handleCheckpointsRoute(w http.ResponseWriter, r *http.Request, invocationRef, action string) {
	_, subAction, _ := strings.Cut(action, "/")

	switch subAction {
	case "":
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocationCheckpoints(w, r, invocationRef)
	case "apply":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleCheckpointApply(w, r, invocationRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown checkpoints action: "+subAction, "")
	}
}
