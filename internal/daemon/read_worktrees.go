package daemon

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// handleListWorktrees handles GET /worktrees.
func (s *Server) handleListWorktrees(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	params := parseListWorktreesParams(r)
	if !isValidWorktreeState(params.State) {
		s.writeAPIError(w, http.StatusBadRequest, requestID, "E_INVALID_ARGUMENT",
			fmt.Sprintf("invalid value for parameter 'state': %q", params.State), "",
			InvalidQueryArgumentDetails{
				Param:         "state",
				Value:         params.State,
				AllowedValues: validWorktreeStates,
			})
		return
	}

	repoIDs, err := getRepoIDsForQuery(s, params.RepoID)
	if err != nil {
		writeRepoLookupError(w, s, requestID, err, "run 'agency repo ls' to see registered repos, or 'agency repo add <path>' to register")
		return
	}

	var allWorktrees []WorktreeDTO
	for _, repoID := range repoIDs {
		records, err := store.ScanIntegrationWorktreesForRepo(s.Store.DataDir, repoID)
		if err != nil {
			continue
		}
		for _, r := range records {
			if r.Broken || r.Meta == nil {
				continue
			}
			if !matchesWorktreeState(r.Meta.State, params.State) {
				continue
			}
			allWorktrees = append(allWorktrees, WorktreeMetaToDTO(r.Meta))
		}
	}

	sort.Slice(allWorktrees, func(i, j int) bool {
		if allWorktrees[i].LastUsedAt != allWorktrees[j].LastUsedAt {
			return allWorktrees[i].LastUsedAt > allWorktrees[j].LastUsedAt
		}
		return allWorktrees[i].WorktreeID < allWorktrees[j].WorktreeID
	})

	worktrees, nextCursor := paginateWorktrees(allWorktrees, params.Cursor, params.Limit)
	s.writeAPIResponse(w, requestID, ListWorktreesData{Worktrees: worktrees, NextCursor: nextCursor})
}

// handleGetWorktree handles GET /worktrees/{ref}.
func (s *Server) handleGetWorktree(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	record, resolveErr := s.resolveWorktreeRef(worktreeRef, r.URL.Query().Get("repo_id"))
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agency worktree ls' to list available worktrees", errors.EWorktreeIDAmbiguous)
		return
	}

	s.writeAPIResponse(w, requestID, WorktreeMetaToDTO(record.Meta))
}

// resolveWorktreeRef resolves a worktree reference across all repos.
func (s *Server) resolveWorktreeRef(ref string, repoID string) (*store.IntegrationWorktreeRecord, error) {
	repoIDs, err := getRepoIDsForQuery(s, repoID)
	if err != nil {
		return nil, err
	}

	var lastAmbiguousErr error
	for _, rid := range repoIDs {
		record, err := s.resolveWorktreeRefForRepo(ref, rid)
		if err == nil && record != nil {
			return record, nil
		}
		if _, ok := err.(*ids.ErrWorktreeAmbiguous); ok {
			lastAmbiguousErr = err
		}
	}

	if lastAmbiguousErr != nil {
		return nil, lastAmbiguousErr
	}
	return nil, errors.New(errors.EWorktreeNotFound, "worktree not found: "+ref)
}

// resolveWorktreeRefForRepo resolves a worktree reference within a specific repo.
func (s *Server) resolveWorktreeRefForRepo(ref string, repoID string) (*store.IntegrationWorktreeRecord, error) {
	records, err := store.ScanIntegrationWorktreesForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return nil, err
	}

	refs := make([]ids.WorktreeRef, 0, len(records))
	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}
		refs = append(refs, ids.WorktreeRef{
			WorktreeID: r.WorktreeID,
			RepoID:     repoID,
			Name:       r.Meta.Name,
			State:      string(r.Meta.State),
			Broken:     r.Broken,
		})
	}

	resolved, err := ids.ResolveWorktreeRef(ref, refs, ids.ResolveWorktreeRefOpts{IncludeArchived: true})
	if err != nil {
		return nil, err
	}

	for _, r := range records {
		if r.WorktreeID == resolved.WorktreeID {
			return &r, nil
		}
	}

	return nil, errors.New(errors.EWorktreeNotFound, "worktree not found: "+ref)
}
