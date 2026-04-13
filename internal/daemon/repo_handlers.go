// Package daemon implements the agency daemon supervisor.
package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// handleRepos handles requests to /repos/...
func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/repos/register" {
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleRepoRegister(w, r)
		return
	}

	if r.URL.Path == "/repos" || r.URL.Path == "/repos/" {
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleListRepos(w, r)
		return
	}

	remaining, ok := trimRoutePrefix(r.URL.Path, "/repos/")
	if !ok {
		s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "not found", "")
		return
	}
	if remaining != "" {
		if !s.requireMethod(w, r, http.MethodGet) {
			return
		}
		s.handleGetRepo(w, r, remaining)
		return
	}

	s.writeError(w, http.StatusNotFound, "E_NOT_FOUND", "not found", "")
}

// handleRepoRegister handles POST /repos/register.
// This is the canonical way for CLI to tell daemon about a repo root and get repo_id back.
func (s *Server) handleRepoRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := getOrCreateRequestID(r)

	var req RepoRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeRepoError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "")
		return
	}

	if req.RepoRoot == "" {
		s.writeRepoError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", "repo_root is required", "")
		return
	}

	// 1. Canonicalize: Abs → EvalSymlinks
	absRoot, err := filepath.Abs(req.RepoRoot)
	if err != nil {
		s.writeRepoError(w, http.StatusBadRequest, requestID, string(errors.ERepoRootInaccessible),
			"failed to resolve repo_root: "+err.Error(), "")
		return
	}

	// 2. Check accessibility
	if _, err := os.Stat(absRoot); err != nil {
		s.writeRepoError(w, http.StatusBadRequest, requestID, string(errors.ERepoRootInaccessible),
			"cannot access repo_root: "+err.Error(),
			"ensure the path exists and is readable")
		return
	}

	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		s.writeRepoError(w, http.StatusBadRequest, requestID, string(errors.ERepoRootInaccessible),
			"failed to resolve symlinks for repo_root: "+err.Error(), "")
		return
	}

	// 3. Normalize to git toplevel via git rev-parse --show-toplevel
	gitRoot, err := git.GetRepoRoot(ctx, s.Runner, absRoot)
	if err != nil {
		s.writeRepoError(w, http.StatusBadRequest, requestID, string(errors.ERepoNotAGitRepo),
			"not a git repository: "+err.Error(),
			"pass a path inside a git repository")
		return
	}
	canonicalRoot := gitRoot.Path

	// EvalSymlinks on the canonical root too (git may return a non-canonical path)
	canonicalRoot, err = filepath.EvalSymlinks(canonicalRoot)
	if err != nil {
		s.writeRepoError(w, http.StatusBadRequest, requestID, string(errors.ERepoRootInaccessible),
			"failed to resolve symlinks for git toplevel: "+err.Error(), "")
		return
	}

	// 4. Derive repo identity
	originInfo := git.GetOriginInfo(ctx, s.Runner, canonicalRoot)
	repoIdentity := identity.DeriveRepoIdentity(canonicalRoot, originInfo.URL)

	// 5. Register in repo_index.json
	if err := s.ensureRepoRegistered(repoIdentity, canonicalRoot); err != nil {
		s.writeRepoError(w, http.StatusInternalServerError, requestID, "E_INTERNAL",
			"failed to register repo: "+err.Error(), "")
		return
	}

	// 6. Upsert repo.json with PreferredRoot
	if err := s.ensureRepoRecordWithPreferredRoot(repoIdentity, canonicalRoot, originInfo); err != nil {
		s.writeRepoError(w, http.StatusInternalServerError, requestID, "E_INTERNAL",
			"failed to update repo record: "+err.Error(), "")
		return
	}

	// 7. Reload state for response
	idx, err := s.Store.LoadRepoIndex()
	if err != nil {
		s.writeRepoError(w, http.StatusInternalServerError, requestID, "E_INTERNAL",
			"failed to load repo index: "+err.Error(), "")
		return
	}

	rec, _, _ := s.Store.LoadRepoRecord(repoIdentity.RepoID)

	// Find the entry for this repo
	var entry store.RepoIndexEntry
	for _, e := range idx.Repos {
		if e.RepoID == repoIdentity.RepoID {
			entry = e
			break
		}
	}

	preferredRoot := rec.PreferredRoot
	accessible := isRootAccessible(preferredRoot)

	resp := RepoRegisterResponse{
		OK:           true,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
		GitSHA:       version.Commit,
		RequestID:    requestID,
		Data: &RepoRegisterData{
			RepoID:                  repoIdentity.RepoID,
			RepoKey:                 repoIdentity.RepoKey,
			Paths:                   entry.Paths,
			PreferredRoot:           preferredRoot,
			PreferredRootAccessible: accessible,
			LastSeenAt:              entry.LastSeenAt,
		},
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// handleListRepos handles GET /repos.
func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)

	idx, err := s.Store.LoadRepoIndex()
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", err.Error(), "", nil)
		return
	}

	var repos []RepoDTO
	for _, entry := range idx.Repos {
		dto := s.buildRepoDTO(entry)
		repos = append(repos, dto)
	}

	// Sort by last_seen_at desc for stable output
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].LastSeenAt > repos[j].LastSeenAt
	})

	if repos == nil {
		repos = []RepoDTO{}
	}

	s.writeAPIResponse(w, requestID, ListReposData{Repos: repos})
}

// handleGetRepo handles GET /repos/{repo_ref}.
func (s *Server) handleGetRepo(w http.ResponseWriter, r *http.Request, repoRef string) {
	requestID := getOrCreateRequestID(r)

	idx, err := s.Store.LoadRepoIndex()
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", err.Error(), "", nil)
		return
	}

	// Build refs for resolver
	refs := s.buildRepoRefs(idx)

	resolved, resolveErr := ids.ResolveRepoRef(repoRef, refs)
	if resolveErr != nil {
		switch e := resolveErr.(type) {
		case *ids.ErrRepoAmbiguous:
			candidates := make([]string, len(e.Candidates))
			for i, c := range e.Candidates {
				candidates[i] = c.RepoID
			}
			s.writeAPIError(w, http.StatusConflict, requestID, string(errors.ERepoIDAmbiguous),
				e.Error(),
				"use a more specific name, repo key, or full repo id",
				AmbiguousDetails{Candidates: candidates})
		default:
			s.writeAPIError(w, http.StatusNotFound, requestID, string(errors.ERepoNotFound),
				"repo not found: "+repoRef,
				"run 'agency repo ls' to see registered repos, or 'agency repo add <path>' to register",
				nil)
		}
		return
	}

	// Find the matching index entry
	for _, entry := range idx.Repos {
		if entry.RepoID == resolved.RepoID {
			dto := s.buildRepoDTO(entry)
			s.writeAPIResponse(w, requestID, dto)
			return
		}
	}

	s.writeAPIError(w, http.StatusNotFound, requestID, string(errors.ERepoNotFound),
		"repo not found: "+repoRef,
		"run 'agency repo ls' to see registered repos, or 'agency repo add <path>' to register",
		nil)
}

// buildRepoDTO constructs a RepoDTO from a repo index entry.
func (s *Server) buildRepoDTO(entry store.RepoIndexEntry) RepoDTO {
	dto := RepoDTO{
		RepoID:     entry.RepoID,
		Paths:      entry.Paths,
		LastSeenAt: entry.LastSeenAt,
	}

	// Load repo.json for richer info
	rec, exists, _ := s.Store.LoadRepoRecord(entry.RepoID)
	if exists {
		dto.RepoKey = rec.RepoKey
		dto.PreferredRoot = rec.PreferredRoot
		dto.UpdatedAt = rec.UpdatedAt

		if rec.OriginPresent {
			dto.Origin = &OriginDTO{
				Present: true,
				URL:     rec.OriginURL,
				Host:    rec.OriginHost,
			}
		} else {
			dto.Origin = &OriginDTO{Present: false}
		}
	}

	// Check preferred root accessibility
	if dto.PreferredRoot != "" {
		dto.PreferredRootAccessible = isRootAccessible(dto.PreferredRoot)
	}

	if dto.Paths == nil {
		dto.Paths = []string{}
	}

	return dto
}

// ensureRepoRecordWithPreferredRoot creates or updates repo.json with PreferredRoot set.
func (s *Server) ensureRepoRecordWithPreferredRoot(repoIdentity identity.RepoIdentity, canonicalRoot string, originInfo git.OriginInfo) error {
	existing, found, err := s.Store.LoadRepoRecord(repoIdentity.RepoID)
	if err != nil {
		return err
	}

	var existingPtr *store.RepoRecord
	if found {
		existingPtr = &existing
	}

	rec := s.Store.UpsertRepoRecord(existingPtr, store.BuildRepoRecordInput{
		RepoKey:          repoIdentity.RepoKey,
		RepoID:           repoIdentity.RepoID,
		RepoRootLastSeen: canonicalRoot,
		PreferredRoot:    canonicalRoot,
		OriginPresent:    originInfo.Present,
		OriginURL:        originInfo.URL,
		OriginHost:       originInfo.Host,
		Capabilities: store.Capabilities{
			GitHubOrigin: repoIdentity.GitHubFlowAvailable,
			OriginHost:   originInfo.Host,
		},
	})

	return s.Store.SaveRepoRecord(rec)
}

// isRootAccessible checks if a path is accessible (exists and is a directory).
func isRootAccessible(root string) bool {
	if root == "" {
		return false
	}
	info, err := os.Stat(root)
	return err == nil && info.IsDir()
}

// buildRepoRefs builds a slice of ids.RepoRef from the repo index.
func (s *Server) buildRepoRefs(idx store.RepoIndex) []ids.RepoRef {
	var refs []ids.RepoRef
	for _, entry := range idx.Repos {
		ref := ids.RepoRef{
			RepoID: entry.RepoID,
		}
		// Load repo.json for RepoKey
		rec, exists, _ := s.Store.LoadRepoRecord(entry.RepoID)
		if exists {
			ref.RepoKey = rec.RepoKey
		} else {
			ref.Broken = true
		}
		refs = append(refs, ref)
	}
	return refs
}
