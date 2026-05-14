package daemon

import (
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/version"
)

func daemonBuildVersion() string {
	return version.FullVersion()
}

func prepareRequestID(w http.ResponseWriter, r *http.Request) string {
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)
	return requestID
}

func (s *Server) invocationLogPaths(repoID, invocationID string) *LogPaths {
	return &LogPaths{
		Raw:      s.readableInvocationLogPath(repoID, invocationID, "raw"),
		Stderr:   s.readableInvocationLogPath(repoID, invocationID, "stderr"),
		Stream:   s.readableInvocationLogPath(repoID, invocationID, "stream"),
		Hooks:    s.readableInvocationLogPath(repoID, invocationID, "hooks"),
		Terminal: s.readableInvocationLogPath(repoID, invocationID, "terminal"),
	}
}

func (s *Server) worktreeNameForResponse(repoID, worktreeID string) string {
	if worktreeID == "" {
		return ""
	}
	worktreeMeta, err := s.Store.ReadIntegrationWorktreeMeta(repoID, worktreeID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(worktreeMeta.Name)
}
