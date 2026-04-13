package daemon

import (
	"net/http"
	"strings"
)

func (s *Server) handleInvocations(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if len(path) < len("/invocations/") {
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "not found", "")
		return
	}

	remaining := path[len("/invocations/"):]
	if remaining == "" {
		if r.Method == http.MethodGet {
			s.handleListInvocations(w, r)
			return
		}
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
		return
	}

	if remaining == "start_headless" {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleControlPlaneStartHeadless(w, r)
		return
	}

	if remaining == "start_headed" {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleControlPlaneStartHeaded(w, r)
		return
	}

	var invocationRef, action string
	for i, c := range remaining {
		if c == '/' {
			invocationRef = remaining[:i]
			action = remaining[i+1:]
			break
		}
	}
	if invocationRef == "" {
		invocationRef = remaining
	}

	if invocationRef == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invocation ref required", "")
		return
	}

	topAction, _, _ := strings.Cut(action, "/")
	switch topAction {
	case "":
		if r.Method == http.MethodGet {
			s.handleGetInvocation(w, r, invocationRef)
			return
		}
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
	case "stop":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleStop(w, r, invocationRef)
	case "kill":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleKill(w, r, invocationRef)
	case "checkpoints":
		s.handleCheckpointsRoute(w, r, invocationRef, action)
	case "restart":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleRestartFromCheckpoint(w, r, invocationRef)
	case "land":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleLand(w, r, invocationRef)
	case "discard":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleDiscard(w, r, invocationRef)
	case "diff":
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleGetInvocationDiff(w, r, invocationRef)
	case "logs":
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleGetInvocationLogs(w, r, invocationRef)
	case "timeline":
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleGetInvocationTimeline(w, r, invocationRef)
	case "review":
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleGetInvocationReview(w, r, invocationRef)
	case "chat":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleControlPlaneFollowUpPrompt(w, r, invocationRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown action: "+action, "")
	}
}

func (s *Server) handleCheckpointsRoute(w http.ResponseWriter, r *http.Request, invocationRef, action string) {
	subAction := ""
	if idx := strings.Index(action, "/"); idx != -1 {
		subAction = action[idx+1:]
	}

	switch subAction {
	case "":
		if r.Method == http.MethodGet {
			s.handleGetInvocationCheckpoints(w, r, invocationRef)
			return
		}
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
	case "apply":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleCheckpoints(w, r, invocationRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown checkpoints action: "+subAction, "")
	}
}
