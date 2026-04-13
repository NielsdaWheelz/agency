package daemon

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// handleListInvocations handles GET /invocations.
func (s *Server) handleListInvocations(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	params := parseListInvocationsParams(r)
	if !isValidInvocationState(params.State) {
		s.writeAPIError(w, http.StatusBadRequest, requestID, "E_INVALID_ARGUMENT",
			fmt.Sprintf("invalid value for parameter 'state': %q", params.State), "",
			InvalidQueryArgumentDetails{Param: "state", Value: params.State, AllowedValues: validInvocationStates})
		return
	}
	if !isValidInvocationMode(params.Mode) {
		s.writeAPIError(w, http.StatusBadRequest, requestID, "E_INVALID_ARGUMENT",
			fmt.Sprintf("invalid value for parameter 'mode': %q", params.Mode), "",
			InvalidQueryArgumentDetails{Param: "mode", Value: params.Mode, AllowedValues: validInvocationModes})
		return
	}

	repoIDs, err := getRepoIDsForQuery(s, params.RepoID)
	if err != nil {
		writeRepoLookupError(w, s, requestID, err, "run 'agency repo ls' to see registered repos, or 'agency repo add <path>' to register")
		return
	}

	var worktreeIDFilter string
	if params.WorktreeRef != "" {
		resolved := false
		for _, repoID := range repoIDs {
			record, err := s.resolveWorktreeRefForRepo(params.WorktreeRef, repoID)
			if err == nil && record != nil {
				worktreeIDFilter = record.WorktreeID
				resolved = true
				break
			}
		}
		if !resolved {
			worktreeIDFilter = "__unresolved__"
		}
	} else if params.WorktreeID != "" {
		worktreeIDFilter = params.WorktreeID
	}

	now := s.Clock()
	var allInvocations []InvocationDTO
	for _, repoID := range repoIDs {
		records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
		if err != nil {
			continue
		}
		for _, r := range records {
			if r.Broken || r.Meta == nil {
				continue
			}
			if worktreeIDFilter != "" && r.Meta.IntegrationWorktreeID != worktreeIDFilter {
				continue
			}
			if !matchesInvocationState(r.Meta.Status, r.Meta.LandingStatus, params.State) {
				continue
			}
			if !matchesInvocationMode(r.Meta.Mode, params.Mode) {
				continue
			}

			logsDir := s.preferredInvocationLogsDir(repoID, r.InvocationID)
			dto := InvocationMetaToDTO(r.Meta, repoID, logsDir, now)
			resolved := &resolvedInvocation{
				InvocationID: r.InvocationID,
				RepoID:       repoID,
				Meta:         r.Meta,
			}
			activityProjection := s.buildInvocationActivityProjection(resolved, dto.DisplayStatus, s.loadRunnerSummaryBestEffort(resolved), nil)
			applyInvocationActivityProjection(&dto, activityProjection)
			allInvocations = append(allInvocations, dto)
		}
	}

	sort.Slice(allInvocations, func(i, j int) bool {
		if allInvocations[i].StartedAt != allInvocations[j].StartedAt {
			return allInvocations[i].StartedAt > allInvocations[j].StartedAt
		}
		return allInvocations[i].InvocationID < allInvocations[j].InvocationID
	})

	invocations, nextCursor := paginateInvocations(allInvocations, params.Cursor, params.Limit)
	s.writeAPIResponse(w, requestID, ListInvocationsData{Invocations: invocations, NextCursor: nextCursor})
}

// handleGetInvocation handles GET /invocations/{ref}.
func (s *Server) handleGetInvocation(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationRef, r.URL.Query().Get("repo_id"))
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agent ls' to list invocations", errors.EInvocationIDAmbiguous)
		return
	}

	now := s.Clock()
	logsDir := s.preferredInvocationLogsDir(record.RepoID, record.InvocationID)
	dto := InvocationMetaToDTO(record.Meta, record.RepoID, logsDir, now)
	activityProjection := s.buildInvocationActivityProjection(record, dto.DisplayStatus, s.loadRunnerSummaryBestEffort(record), nil)
	applyInvocationActivityProjection(&dto, activityProjection)
	s.writeAPIResponse(w, requestID, dto)
}

// resolvedInvocation contains the resolved invocation with its repo ID.
type resolvedInvocation struct {
	InvocationID string
	RepoID       string
	Meta         *store.InvocationMeta
}

// resolveInvocationRef resolves an invocation reference across all repos.
func (s *Server) resolveInvocationRef(ref string, repoID string) (*resolvedInvocation, error) {
	repoIDs, err := getRepoIDsForQuery(s, repoID)
	if err != nil {
		return nil, err
	}

	var lastAmbiguousErr error
	for _, rid := range repoIDs {
		record, err := s.resolveInvocationRefForRepo(ref, rid)
		if err == nil && record != nil {
			return record, nil
		}
		if _, ok := err.(*ids.ErrInvocationAmbiguous); ok {
			lastAmbiguousErr = err
		}
	}

	if lastAmbiguousErr != nil {
		return nil, lastAmbiguousErr
	}
	return nil, errors.New(errors.EInvocationNotFound, "invocation not found: "+ref)
}

// resolveInvocationRefForRepo resolves an invocation reference within a specific repo.
func (s *Server) resolveInvocationRefForRepo(ref string, repoID string) (*resolvedInvocation, error) {
	records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return nil, err
	}

	refs := make([]ids.InvocationRef, 0, len(records))
	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}
		refs = append(refs, ids.InvocationRef{
			InvocationID:          r.InvocationID,
			RepoID:                repoID,
			IntegrationWorktreeID: r.Meta.IntegrationWorktreeID,
			InvocationName:        r.Meta.InvocationName,
			Status:                string(r.Meta.Status),
			LandingStatus:         string(r.Meta.LandingStatus),
			Broken:                r.Broken,
		})
	}

	resolved, err := ids.ResolveInvocationRef(ref, refs, ids.ResolveInvocationRefOpts{IncludeFinished: true})
	if err != nil {
		return nil, err
	}

	for _, r := range records {
		if r.InvocationID == resolved.InvocationID {
			return &resolvedInvocation{
				InvocationID: r.InvocationID,
				RepoID:       repoID,
				Meta:         r.Meta,
			}, nil
		}
	}

	return nil, errors.New(errors.EInvocationNotFound, "invocation not found: "+ref)
}
