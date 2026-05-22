package daemon

import (
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func (s *Server) handleInvocations(w http.ResponseWriter, r *http.Request) {
	if routePathEquals(r.URL.Path, "/invocations") {
		if !s.requireAPIResponseMethod(w, r, http.MethodGet) {
			return
		}
		s.handleListInvocations(w, r)
		return
	}

	remaining, ok := trimRoutePrefix(r.URL.Path, "/invocations/")
	if !ok {
		s.writeError(w, http.StatusNotFound, string(errors.ENotFound), "not found", "")
		return
	}
	if remaining == "" {
		if !s.requireAPIResponseMethod(w, r, http.MethodGet) {
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
		s.writeError(w, http.StatusBadRequest, string(errors.EInvalidRequest), "invocation ref required", "")
		return
	}

	switch routeFirstAction(action) {
	case "":
		if !s.requireAPIResponseMethod(w, r, http.MethodGet) {
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
	case "recreate":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleRecreateHeaded(w, r, invocationRef)
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
		if !s.requireAPIResponseMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocationDiff(w, r, invocationRef)
	case "logs":
		if !s.requireAPIResponseMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocationLogs(w, r, invocationRef)
	case "headed_hook":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleHeadedHook(w, r, invocationRef)
	case "timeline":
		if !s.requireAPIResponseMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocationTimeline(w, r, invocationRef)
	case "check":
		if !s.requireAPIResponseMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocationCheck(w, r, invocationRef)
	case "session":
		if !s.requireAPIResponseMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocationSession(w, r, invocationRef)
	case "followup":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleControlPlaneFollowUp(w, r, invocationRef)
	default:
		s.writeError(w, http.StatusNotFound, string(errors.ENotFound), "unknown action: "+action, "")
	}
}

func (s *Server) handleCheckpointsRoute(w http.ResponseWriter, r *http.Request, invocationRef, action string) {
	_, subAction, _ := strings.Cut(action, "/")

	switch subAction {
	case "":
		if !s.requireAPIResponseMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetInvocationCheckpoints(w, r, invocationRef)
	case "apply":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleCheckpointApply(w, r, invocationRef)
	default:
		s.writeError(w, http.StatusNotFound, string(errors.ENotFound), "unknown checkpoints action: "+subAction, "")
	}
}
