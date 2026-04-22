package daemon

import (
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// getOrCreateRequestID extracts the request ID from context or headers, or generates one.
func getOrCreateRequestID(r *http.Request) string {
	if r == nil {
		return newRequestID()
	}
	if reqID := requestIDFromContext(r.Context()); reqID != "" {
		return reqID
	}
	return resolveOrGenerateRequestID(r.Header.Get("X-Request-ID"))
}

// writeAPIResponse writes a successful API response with the envelope.
func (s *Server) writeAPIResponse(w http.ResponseWriter, requestID string, data interface{}) {
	requestID = resolveOrGenerateRequestID(requestID)
	setRequestIDHeader(w, requestID)
	resp := APIResponse{
		OK:           true,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		GitSHA:       version.Commit,
		RequestID:    requestID,
		Data:         data,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// writeAPIError writes an error API response with the envelope.
func (s *Server) writeAPIError(w http.ResponseWriter, status int, requestID, code, message, hint string, details interface{}) {
	requestID = resolveOrGenerateRequestID(requestID)
	setRequestIDHeader(w, requestID)
	resp := APIResponse{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		GitSHA:       version.Commit,
		RequestID:    requestID,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
		Details:      details,
	}
	s.writeJSON(w, status, resp)
}

// writeReadResolveError handles not-found and ambiguous resolution errors for read endpoints.
func (s *Server) writeReadResolveError(w http.ResponseWriter, requestID string, err error, notFoundHint string, ambiguousCode errors.Code) {
	if candidates, ok := readAmbiguousCandidates(err); ok {
		s.writeAPIError(
			w,
			http.StatusConflict,
			requestID,
			string(ambiguousCode),
			err.Error(),
			notFoundHint,
			AmbiguousDetails{Candidates: candidates},
		)
		return
	}

	code := errors.GetCode(err)
	if code == "" {
		switch err.(type) {
		case *ids.ErrRepoNotFound:
			code = errors.ERepoNotFound
		case *ids.ErrWorktreeNotFound:
			code = errors.EWorktreeNotFound
		case *ids.ErrInvocationNotFound:
			code = errors.EInvocationNotFound
		default:
			code = errors.EInternal
		}
	}
	s.writeAPIError(w, http.StatusNotFound, requestID, string(code), err.Error(), notFoundHint, nil)
}

func readAmbiguousCandidates(err error) ([]string, bool) {
	switch e := err.(type) {
	case *ids.ErrRepoAmbiguous:
		candidates := make([]string, len(e.Candidates))
		for i, c := range e.Candidates {
			candidates[i] = c.RepoID
		}
		return candidates, true
	case *ids.ErrWorktreeAmbiguous:
		candidates := make([]string, len(e.Candidates))
		for i, c := range e.Candidates {
			candidates[i] = c.WorktreeID
		}
		return candidates, true
	case *ids.ErrInvocationAmbiguous:
		candidates := make([]string, len(e.Candidates))
		for i, c := range e.Candidates {
			candidates[i] = c.InvocationID
		}
		return candidates, true
	default:
		return nil, false
	}
}

func (s *Server) loadRunnerStatusForInvocation(record *resolvedInvocation) (*runnerstatus.RunnerStatus, error) {
	if s == nil || s.Store == nil || record == nil || record.Meta == nil {
		return nil, nil
	}
	invocationRoot := s.Store.InvocationDir(record.RepoID, record.InvocationID)
	return runnerstatus.Load(invocationRoot)
}

func getRepoIDsForQuery(s *Server, repoID string) ([]string, error) {
	idx, err := s.Store.LoadRepoIndex()
	if err != nil {
		return nil, err
	}

	if repoID == "" {
		repoIDs := make([]string, 0, len(idx.Repos))
		for _, entry := range idx.Repos {
			repoIDs = append(repoIDs, entry.RepoID)
		}
		return repoIDs, nil
	}

	for _, entry := range idx.Repos {
		if entry.RepoID == repoID {
			return []string{repoID}, nil
		}
	}

	return nil, &ids.ErrRepoNotFound{Input: repoID}
}

func writeRepoLookupError(w http.ResponseWriter, s *Server, requestID string, err error, notFoundHint string) {
	switch e := err.(type) {
	case *ids.ErrRepoAmbiguous:
		candidates := make([]string, len(e.Candidates))
		for i, c := range e.Candidates {
			candidates[i] = c.RepoID
		}
		s.writeAPIError(
			w,
			http.StatusConflict,
			requestID,
			string(errors.ERepoIDAmbiguous),
			e.Error(),
			"use a more specific name, repo key, or full repo id",
			AmbiguousDetails{Candidates: candidates},
		)
	case *ids.ErrRepoNotFound:
		s.writeAPIError(
			w,
			http.StatusNotFound,
			requestID,
			string(errors.ERepoNotFound),
			e.Error(),
			notFoundHint,
			nil,
		)
	default:
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(code), err.Error(), "", nil)
	}
}
