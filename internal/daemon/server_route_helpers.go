package daemon

import (
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func trimRoutePrefix(path, prefix string) (string, bool) {
	return strings.CutPrefix(path, prefix)
}

func routePathEquals(path, route string) bool {
	return path == route || path == route+"/"
}

func splitRouteRefAction(tail string) (string, string) {
	ref, action, _ := strings.Cut(tail, "/")
	return ref, action
}

func routeFirstAction(action string) string {
	topAction, _, _ := strings.Cut(action, "/")
	return topAction
}

func (s *Server) requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	s.writeError(w, http.StatusMethodNotAllowed, string(errors.EMethodNotAllowed), "method not allowed", "")
	return false
}

func (s *Server) requireAPIResponseMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	s.writeAPIError(w, http.StatusMethodNotAllowed, getOrCreateRequestID(r), string(errors.EMethodNotAllowed), "method not allowed", "", nil)
	return false
}
