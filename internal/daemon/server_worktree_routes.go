package daemon

import (
	"net/http"
	"strings"
)

func (s *Server) handleWorktrees(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/worktrees" || r.URL.Path == "/worktrees/" {
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleListWorktrees(w, r)
		return
	}

	remaining, ok := trimRoutePrefix(r.URL.Path, "/worktrees/")
	if !ok {
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "not found", "")
		return
	}
	if remaining == "create" {
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleWorktreeCreate(w, r)
		return
	}

	worktreeRef, action := splitRouteRefAction(remaining)
	if worktreeRef == "" {
		s.writeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "worktree ref required", "")
		return
	}

	switch routeFirstAction(action) {
	case "":
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetWorktree(w, r, worktreeRef)
	case "rm":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleWorktreeRm(w, r, worktreeRef)
	case "pr":
		s.handleWorktreePRRoute(w, r, worktreeRef, action)
	case "update":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleWorktreeUpdate(w, r, worktreeRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown action: "+action, "supported actions: rm, pr, update")
	}
}

func (s *Server) handleWorktreePRRoute(w http.ResponseWriter, r *http.Request, worktreeRef, action string) {
	_, subAction, _ := strings.Cut(action, "/")

	switch subAction {
	case "sync":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleWorktreePRSync(w, r, worktreeRef)
	case "merge":
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleWorktreePRMerge(w, r, worktreeRef)
	default:
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "unknown pr action: "+subAction, "")
	}
}
