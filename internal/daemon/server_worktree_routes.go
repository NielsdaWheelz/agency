package daemon

import (
	"net/http"
	"strings"
)

func (s *Server) handleWorktrees(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if path == "/worktrees" || path == "/worktrees/" {
		if r.Method == http.MethodGet {
			s.handleListWorktrees(w, r)
			return
		}
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
		return
	}

	remaining := strings.TrimPrefix(path, "/worktrees/")
	if remaining == "create" {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleWorktreeCreate(w, r)
		return
	}

	var worktreeRef, action string
	for i, c := range remaining {
		if c == '/' {
			worktreeRef = remaining[:i]
			action = remaining[i+1:]
			break
		}
	}
	if worktreeRef == "" {
		worktreeRef = remaining
	}

	if worktreeRef == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "worktree ref required", "")
		return
	}

	topAction, _, _ := strings.Cut(action, "/")
	switch topAction {
	case "":
		if r.Method == http.MethodGet {
			s.handleGetWorktree(w, r, worktreeRef)
			return
		}
		s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
	case "rm":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleWorktreeRm(w, r, worktreeRef)
	case "pr":
		s.handleWorktreePRRoute(w, r, worktreeRef, action)
	case "merge":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleWorktreePRMerge(w, r, worktreeRef)
	case "update":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleWorktreeUpdate(w, r, worktreeRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown action: "+action, "supported actions: rm, pr, merge, update")
	}
}

func (s *Server) handleWorktreePRRoute(w http.ResponseWriter, r *http.Request, worktreeRef, action string) {
	subAction := ""
	if idx := strings.Index(action, "/"); idx != -1 {
		subAction = action[idx+1:]
	}

	switch subAction {
	case "sync":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "E_METHOD_NOT_ALLOWED", "method not allowed", "")
			return
		}
		s.handleWorktreePRSync(w, r, worktreeRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown pr action: "+subAction, "")
	}
}
