package daemon

import (
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if routePathEquals(r.URL.Path, "/tasks") {
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleListTasks(w, r)
		return
	}

	remaining, ok := trimRoutePrefix(r.URL.Path, "/tasks/")
	if !ok {
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "not found", "")
		return
	}
	if remaining == "start" {
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleTaskStart(w, r)
		return
	}

	taskRef, action := splitRouteRefAction(remaining)
	if taskRef == "" {
		s.writeError(w, http.StatusBadRequest, string(errors.EInvalidRequest), "task ref required", "")
		return
	}

	switch routeFirstAction(action) {
	case "":
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetTask(w, r, taskRef)
	case "archive":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleTaskArchive(w, r, taskRef)
	case "retry":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleTaskRetry(w, r, taskRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown action: "+action, "supported actions: archive, retry")
	}
}
