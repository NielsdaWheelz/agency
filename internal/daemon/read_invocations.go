package daemon

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// handleListInvocations handles GET /invocations.
func (s *Server) handleListInvocations(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)

	params, invalid := parseListInvocationsParams(r)
	if invalid != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidArgument),
			fmt.Sprintf("invalid value for parameter '%s': %q", invalid.Param, invalid.Value), "",
			*invalid)
		return
	}
	if params.WorktreeRef != "" && params.RepoID == "" {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidArgument), "repo_id query parameter is required when worktree_ref is set", "pass ?repo_id=<repo_id>", nil)
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
	}

	now := s.clock()
	var allInvocations []InvocationDTO
	for _, repoID := range repoIDs {
		repoName := s.repoName(repoID)
		worktreeNames := map[string]string{}
		worktrees, err := s.store.ScanIntegrationWorktreesForRepo(repoID)
		if err == nil {
			for _, worktree := range worktrees {
				if worktree.Meta == nil {
					continue
				}
				worktreeNames[worktree.WorktreeID] = worktree.Meta.Name
			}
		}

		records, err := s.store.ScanInvocationsForRepo(repoID)
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

			resolved := &resolvedInvocation{
				InvocationID: r.InvocationID,
				RepoID:       repoID,
				Meta:         r.Meta,
			}
			projection, err := s.projectInvocationReadSurface(
				resolved,
				repoName,
				worktreeNames[r.Meta.IntegrationWorktreeID],
				now,
				nil,
			)
			if err != nil {
				s.writeInvocationTimelineReadError(w, requestID, err)
				return
			}
			allInvocations = append(allInvocations, projection.DTO)
		}
	}

	slices.SortFunc(allInvocations, func(a, b InvocationDTO) int {
		if a.StartedAt != b.StartedAt {
			return cmp.Compare(b.StartedAt, a.StartedAt)
		}
		return cmp.Compare(a.InvocationID, b.InvocationID)
	})

	invocations, nextCursor := paginateInvocations(allInvocations, params.Cursor, params.Limit)
	s.writeAPIResponse(w, requestID, ListInvocationsData{Invocations: invocations, NextCursor: nextCursor})
}

// handleGetInvocation handles GET /invocations/{ref}.
func (s *Server) handleGetInvocation(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)

	record, resolveErr := s.resolveInvocationRef(invocationRef, r.URL.Query().Get("repo_id"))
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agent ls' to list invocations", errors.EInvocationIDAmbiguous)
		return
	}

	now := s.clock()
	worktreeName := ""
	if worktreeMeta, err := s.store.ReadIntegrationWorktreeMeta(record.RepoID, record.Meta.IntegrationWorktreeID); err == nil && worktreeMeta != nil {
		worktreeName = worktreeMeta.Name
	}
	projection, err := s.projectInvocationReadSurface(record, s.repoName(record.RepoID), worktreeName, now, nil)
	if err != nil {
		s.writeInvocationTimelineReadError(w, requestID, err)
		return
	}
	dto := projection.DTO
	s.writeAPIResponse(w, requestID, dto)
}

type invocationReadProjection struct {
	DTO        InvocationDTO
	RunnerMeta *runnerstatus.RunnerStatus
	RunnerErr  error
}

func (s *Server) projectInvocationReadSurface(
	record *resolvedInvocation,
	repoName string,
	worktreeName string,
	now time.Time,
	entries []timelineSortableEntry,
) (invocationReadProjection, error) {
	logsDir := s.store.InvocationLogsDir(record.RepoID, record.InvocationID)
	runnerMeta, runnerErr := s.loadRunnerStatusForInvocation(record)
	dto := invocationMetaToDTO(record.Meta, record.RepoID, logsDir, runnerMeta, runnerErr, now)
	dto.RepoName = repoName
	dto.WorktreeName = strings.TrimSpace(worktreeName)

	_, _, runnerSummary, _ := projectRunnerStatus(runnerMeta, runnerErr)
	activityProjection, err := s.buildInvocationActivityProjection(record, dto.State, runnerSummary, entries)
	if err != nil {
		return invocationReadProjection{}, err
	}
	dto.StatusSummary = activityProjection.StatusSummary
	dto.LatestActivity = activityProjection.LatestActivity
	dto.Navigation = activityProjection.Navigation

	return invocationReadProjection{
		DTO:        dto,
		RunnerMeta: runnerMeta,
		RunnerErr:  runnerErr,
	}, nil
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
	records, err := s.store.ScanInvocationsForRepo(repoID)
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
