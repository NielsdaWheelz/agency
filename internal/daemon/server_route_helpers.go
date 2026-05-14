package daemon

import (
	"net/http"
	"strings"
)

func trimRoutePrefix(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return strings.TrimPrefix(path, prefix), true
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
	s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
	return false
}
