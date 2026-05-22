package daemon

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// handleListWorktrees handles GET /worktrees.
func (s *Server) handleListWorktrees(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)

	params, invalid := parseListWorktreesParams(r)
	if invalid != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidArgument),
			fmt.Sprintf("invalid value for parameter '%s': %q", invalid.Param, invalid.Value), "",
			*invalid)
		return
	}

	repoIDs, err := getRepoIDsForQuery(s, params.RepoID)
	if err != nil {
		writeRepoLookupError(w, s, requestID, err, "run 'agency repo ls' to see registered repos, or 'agency repo add <path>' to register")
		return
	}

	var allWorktrees []WorktreeDTO
	for _, repoID := range repoIDs {
		repoName := s.repoName(repoID)
		records, err := s.store.ScanIntegrationWorktreesForRepo(repoID)
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
			mergeMeta, ok := s.readOptionalWorktreeMergeForReadResponse(w, requestID, r.Meta.RepoID, r.Meta.WorktreeID)
			if !ok {
				return
			}
			dto := worktreeMetaToDTO(r.Meta, mergeMeta)
			dto.RepoName = repoName
			allWorktrees = append(allWorktrees, dto)
		}
	}

	slices.SortFunc(allWorktrees, func(a, b WorktreeDTO) int {
		if a.LastUsedAt != b.LastUsedAt {
			return cmp.Compare(b.LastUsedAt, a.LastUsedAt)
		}
		return cmp.Compare(a.WorktreeID, b.WorktreeID)
	})

	worktrees, nextCursor := paginateWorktrees(allWorktrees, params.Cursor, params.Limit)
	s.writeAPIResponse(w, requestID, ListWorktreesData{Worktrees: worktrees, NextCursor: nextCursor})
}

// handleGetWorktree handles GET /worktrees/{ref}.
func (s *Server) handleGetWorktree(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := getOrCreateRequestID(r)

	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidArgument), "repo_id query parameter is required", "pass ?repo_id=<repo_id>", nil)
		return
	}

	record, resolveErr := s.resolveWorktreeRefForRepo(worktreeRef, repoID)
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agency worktree ls' to list available worktrees", errors.EWorktreeIDAmbiguous)
		return
	}
	if record.Broken || record.Meta == nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EWorktreeBroken), "integration worktree exists but meta.json is unreadable", "inspect or recreate the worktree", nil)
		return
	}

	mergeMeta, ok := s.readOptionalWorktreeMergeForReadResponse(w, requestID, record.Meta.RepoID, record.Meta.WorktreeID)
	if !ok {
		return
	}
	dto := worktreeMetaToDTO(record.Meta, mergeMeta)
	dto.RepoName = s.repoName(record.Meta.RepoID)
	s.writeAPIResponse(w, requestID, dto)
}

// handleGetWorktreeMerge handles GET /worktrees/{ref}/pr/merge.
func (s *Server) handleGetWorktreeMerge(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	requestID := getOrCreateRequestID(r)

	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidArgument), "repo_id query parameter is required", "pass ?repo_id=<repo_id>", nil)
		return
	}

	record, resolveErr := s.resolveWorktreeRefForRepo(worktreeRef, repoID)
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agency worktree ls' to list available worktrees", errors.EWorktreeIDAmbiguous)
		return
	}
	if record.Broken || record.Meta == nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EWorktreeBroken), "integration worktree exists but meta.json is unreadable", "inspect or recreate the worktree", nil)
		return
	}

	mergeMeta, ok := s.readOptionalWorktreeMergeForReadResponse(w, requestID, record.Meta.RepoID, record.Meta.WorktreeID)
	if !ok {
		return
	}
	if mergeMeta == nil {
		s.writeAPIError(w, http.StatusNotFound, requestID, string(errors.EWorktreeMergeNotFound), "worktree does not have durable merge state", "start merge with 'agency worktree <worktree-ref> pr merge'", nil)
		return
	}

	s.writeAPIResponse(w, requestID, worktreeMergeMetaToDTO(mergeMeta))
}

func (s *Server) readOptionalWorktreeMergeForReadResponse(w http.ResponseWriter, requestID, repoID, worktreeID string) (*store.IntegrationWorktreeMergeMeta, bool) {
	mergeMeta, err := s.store.ReadIntegrationWorktreeMerge(repoID, worktreeID)
	if err != nil {
		code := errors.CodeOr(err, errors.EStoreCorrupt)
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(code), err.Error(), "", nil)
		return nil, false
	}
	return mergeMeta, true
}

// resolveWorktreeRefForRepo resolves a worktree reference within a specific repo.
func (s *Server) resolveWorktreeRefForRepo(ref string, repoID string) (*store.IntegrationWorktreeRecord, error) {
	records, err := s.store.ScanIntegrationWorktreesForRepo(repoID)
	if err != nil {
		return nil, err
	}

	refs := make([]ids.WorktreeRef, 0, len(records))
	for _, r := range records {
		state := ""
		if r.Meta != nil {
			state = string(r.Meta.State)
		}
		refs = append(refs, ids.WorktreeRef{
			WorktreeID: r.WorktreeID,
			RepoID:     repoID,
			Name:       r.Name,
			State:      state,
			Broken:     r.Broken,
		})
	}

	resolved, err := ids.ResolveWorktreeRef(ref, refs, ids.ResolveWorktreeRefOpts{})
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
