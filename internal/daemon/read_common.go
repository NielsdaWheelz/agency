package daemon

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
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
		BuildVersion: version.FullVersion(),
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
		BuildVersion: version.FullVersion(),
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
		code = errors.EInternal
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

func (s *Server) loadRunnerSummaryBestEffort(record *resolvedInvocation) string {
	statusMeta, err := s.loadRunnerStatusForInvocation(record)
	if err != nil || statusMeta == nil {
		return ""
	}
	if statusMeta.SchemaVersion != runnerstatus.SchemaVersion {
		return ""
	}
	if err := statusMeta.Validate(); err != nil {
		return ""
	}
	return strings.TrimSpace(statusMeta.Summary)
}

func getRepoIDsForQuery(s *Server, filterRepoRef string) ([]string, error) {
	idx, err := s.Store.LoadRepoIndex()
	if err != nil {
		if filterRepoRef == "" {
			return []string{}, nil
		}
		return []string{filterRepoRef}, nil
	}

	if filterRepoRef == "" {
		repoIDs := make([]string, 0, len(idx.Repos))
		for _, entry := range idx.Repos {
			repoIDs = append(repoIDs, entry.RepoID)
		}
		return repoIDs, nil
	}

	refs := s.buildRepoRefs(idx)
	resolved, resolveErr := ids.ResolveRepoRef(filterRepoRef, refs)
	if resolveErr != nil {
		if _, ok := resolveErr.(*ids.ErrRepoNotFound); ok {
			if _, statErr := os.Stat(s.Store.RepoDir(filterRepoRef)); statErr == nil {
				return []string{filterRepoRef}, nil
			}
			return []string{}, nil
		}
		return nil, resolveErr
	}
	return []string{resolved.RepoID}, nil
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

func parseListWorktreesParams(r *http.Request) ListWorktreesParams {
	params := ListWorktreesParams{
		State: "present",
		Limit: 100,
	}

	if repoID := r.URL.Query().Get("repo_id"); repoID != "" {
		params.RepoID = repoID
	}
	if state := r.URL.Query().Get("state"); state != "" {
		params.State = state
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 && l <= 500 {
			params.Limit = l
		}
	}
	params.Cursor = r.URL.Query().Get("cursor")

	return params
}

func parseListInvocationsParams(r *http.Request) ListInvocationsParams {
	params := ListInvocationsParams{
		State: "all",
		Mode:  "all",
		Limit: 100,
	}

	if repoID := r.URL.Query().Get("repo_id"); repoID != "" {
		params.RepoID = repoID
	}
	if worktreeID := r.URL.Query().Get("worktree_id"); worktreeID != "" {
		params.WorktreeID = worktreeID
	}
	if worktreeRef := r.URL.Query().Get("worktree_ref"); worktreeRef != "" {
		params.WorktreeRef = worktreeRef
	}
	if state := r.URL.Query().Get("state"); state != "" {
		params.State = state
	}
	if mode := r.URL.Query().Get("mode"); mode != "" {
		params.Mode = mode
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 && l <= 500 {
			params.Limit = l
		}
	}
	params.Cursor = r.URL.Query().Get("cursor")

	return params
}

func parseGetDiffParams(r *http.Request) GetDiffParams {
	params := GetDiffParams{
		IncludePatch:       true,
		MaxPatchBytes:      2097152,
		IncludeUncommitted: true,
	}

	if includePatch := r.URL.Query().Get("include_patch"); includePatch == "0" || includePatch == "false" {
		params.IncludePatch = false
	}
	if maxPatch := r.URL.Query().Get("max_patch_bytes"); maxPatch != "" {
		if m, err := strconv.Atoi(maxPatch); err == nil && m > 0 && m <= 5242880 {
			params.MaxPatchBytes = m
		}
	}
	if includeUncommitted := r.URL.Query().Get("include_uncommitted"); includeUncommitted == "0" || includeUncommitted == "false" {
		params.IncludeUncommitted = false
	}
	params.TurnID = strings.TrimSpace(r.URL.Query().Get("turn"))
	params.TurnStartID = strings.TrimSpace(r.URL.Query().Get("turn_start"))
	params.TurnEndID = strings.TrimSpace(r.URL.Query().Get("turn_end"))

	return params
}

func parseGetLogsParams(r *http.Request) GetLogsParams {
	params := GetLogsParams{
		Kind:  "raw",
		Limit: 65536,
	}

	if kind := r.URL.Query().Get("kind"); kind != "" {
		params.Kind = kind
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.ParseInt(offsetStr, 10, 64); err == nil {
			params.Offset = o
		} else {
			params.Offset = -1
		}
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			params.Limit = l
		} else {
			params.Limit = -1
		}
	}

	return params
}

var (
	validWorktreeStates   = []string{"present", "archived", "all"}
	validInvocationStates = []string{"active", "finished", "all"}
	validInvocationModes  = []string{"headed", "headless", "all"}
)

func isValidWorktreeState(s string) bool {
	for _, v := range validWorktreeStates {
		if s == v {
			return true
		}
	}
	return false
}

func isValidInvocationState(s string) bool {
	for _, v := range validInvocationStates {
		if s == v {
			return true
		}
	}
	return false
}

func isValidInvocationMode(s string) bool {
	for _, v := range validInvocationModes {
		if s == v {
			return true
		}
	}
	return false
}

func matchesWorktreeState(state store.WorktreeState, filter string) bool {
	switch filter {
	case "all":
		return true
	case "archived":
		return state == store.WorktreeStateArchived
	case "present":
		return state == store.WorktreeStatePresent
	}
	return false
}

func matchesInvocationState(status store.InvocationStatus, landing store.LandingStatus, filter string) bool {
	switch filter {
	case "all":
		return true
	case "active":
		return status == store.InvocationStatusStarting ||
			status == store.InvocationStatusRunning ||
			(status == store.InvocationStatusFinished &&
				landing != store.LandingStatusLanded &&
				landing != store.LandingStatusDiscarded)
	case "finished":
		return status == store.InvocationStatusFinished || status == store.InvocationStatusFailed
	}
	return false
}

func matchesInvocationMode(mode store.RunnerMode, filter string) bool {
	switch filter {
	case "all":
		return true
	case "headed":
		return mode == store.RunnerModeHeaded
	case "headless":
		return mode == store.RunnerModeHeadless
	}
	return false
}

func paginateWorktrees(all []WorktreeDTO, cursor string, limit int) ([]WorktreeDTO, string) {
	if len(all) == 0 {
		return []WorktreeDTO{}, ""
	}

	startIdx := 0
	if cursor != "" {
		var c WorktreeCursor
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil && json.Unmarshal(decoded, &c) == nil {
			for i, w := range all {
				if w.LastUsedAt < c.LastUsedAt || (w.LastUsedAt == c.LastUsedAt && w.WorktreeID > c.WorktreeID) {
					startIdx = i
					break
				}
			}
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(all) {
		endIdx = len(all)
	}

	result := all[startIdx:endIdx]

	var nextCursor string
	if endIdx < len(all) {
		last := result[len(result)-1]
		c := WorktreeCursor{LastUsedAt: last.LastUsedAt, WorktreeID: last.WorktreeID}
		data, _ := json.Marshal(c)
		nextCursor = base64.StdEncoding.EncodeToString(data)
	}

	return result, nextCursor
}

func paginateInvocations(all []InvocationDTO, cursor string, limit int) ([]InvocationDTO, string) {
	if len(all) == 0 {
		return []InvocationDTO{}, ""
	}

	startIdx := 0
	if cursor != "" {
		var c InvocationCursor
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil && json.Unmarshal(decoded, &c) == nil {
			for i, inv := range all {
				if inv.StartedAt < c.StartedAt || (inv.StartedAt == c.StartedAt && inv.InvocationID > c.InvocationID) {
					startIdx = i
					break
				}
			}
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(all) {
		endIdx = len(all)
	}

	result := all[startIdx:endIdx]

	var nextCursor string
	if endIdx < len(all) {
		last := result[len(result)-1]
		c := InvocationCursor{StartedAt: last.StartedAt, InvocationID: last.InvocationID}
		data, _ := json.Marshal(c)
		nextCursor = base64.StdEncoding.EncodeToString(data)
	}

	return result, nextCursor
}

func paginateCheckpoints(all []CheckpointDTO, cursor string, limit int) ([]CheckpointDTO, string) {
	if len(all) == 0 {
		return []CheckpointDTO{}, ""
	}

	startIdx := 0
	if cursor != "" {
		var c CheckpointCursor
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil && json.Unmarshal(decoded, &c) == nil {
			for i, cp := range all {
				if cp.ID < c.ID {
					startIdx = i
					break
				}
			}
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(all) {
		endIdx = len(all)
	}

	result := all[startIdx:endIdx]

	var nextCursor string
	if endIdx < len(all) {
		last := result[len(result)-1]
		c := CheckpointCursor{ID: last.ID}
		data, _ := json.Marshal(c)
		nextCursor = base64.StdEncoding.EncodeToString(data)
	}

	return result, nextCursor
}
