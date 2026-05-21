// Package daemon implements the agency daemon supervisor.
package daemon

import (
	stderrors "errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/ids"
	agencylock "github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/store"
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

	if r.URL.Path == "/repos/rm" && r.Method != http.MethodGet {
		if !s.requireMethod(w, r, http.MethodPost) {
			return
		}
		s.handleRepoRm(w, r)
		return
	}

	if routePathEquals(r.URL.Path, "/repos") {
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
	if err := decodeStrictJSON(r.Body, &req); err != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "", nil)
		return
	}

	if req.RepoRoot == "" {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "repo_root is required", "", nil)
		return
	}

	absRoot, err := filepath.Abs(req.RepoRoot)
	if err != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.ERepoRootInaccessible), "failed to resolve repo_root: "+err.Error(), "", nil)
		return
	}

	if _, err := os.Stat(absRoot); err != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.ERepoRootInaccessible),
			"cannot access repo_root: "+err.Error(),
			"ensure the path exists and is readable",
			nil)
		return
	}

	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.ERepoRootInaccessible), "failed to resolve symlinks for repo_root: "+err.Error(), "", nil)
		return
	}

	gitRoot, err := git.GetRepoRoot(ctx, s.Runner, absRoot, nil)
	if err != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.ERepoNotAGitRepo),
			"not a git repository: "+err.Error(),
			"pass a path inside a git repository",
			nil)
		return
	}
	canonicalRoot := gitRoot.Path

	canonicalRoot, err = filepath.EvalSymlinks(canonicalRoot)
	if err != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.ERepoRootInaccessible), "failed to resolve symlinks for git toplevel: "+err.Error(), "", nil)
		return
	}

	originInfo := git.GetOriginInfo(ctx, s.Runner, canonicalRoot, nil)
	repoIdentity := identity.DeriveRepoIdentity(canonicalRoot, originInfo.URL)

	unlock, err := s.repoLock.Lock(repoIdentity.RepoID, "repo register")
	if err != nil {
		var lockErr *agencylock.ErrLocked
		if stderrors.As(err, &lockErr) {
			s.writeAPIError(w, http.StatusConflict, requestID, string(errors.ERepoLocked), "repository is locked by another operation", "wait for the other operation to complete", nil)
			return
		}
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to acquire repo lock: "+err.Error(), "", nil)
		return
	}
	defer func() { _ = unlock() }()

	if err := s.appendRepoEvent(repoIdentity.RepoID, "agency.repo_register_started", map[string]any{
		"repo_key":       repoIdentity.RepoKey,
		"preferred_root": canonicalRoot,
	}); err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to append repo event: "+err.Error(), "", nil)
		return
	}

	if err := s.ensureRepoRegistered(repoIdentity, canonicalRoot); err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to register repo: "+err.Error(), "", nil)
		return
	}

	if err := s.ensureRepoRecordWithPreferredRoot(repoIdentity, canonicalRoot, originInfo); err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to update repo record: "+err.Error(), "", nil)
		return
	}

	idx, err := s.Store.LoadRepoIndex()
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to load repo index: "+err.Error(), "", nil)
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

	if err := s.appendRepoEvent(repoIdentity.RepoID, "agency.repo_register_succeeded", map[string]any{
		"repo_key":       repoIdentity.RepoKey,
		"preferred_root": preferredRoot,
	}); err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to append repo event: "+err.Error(), "", nil)
		return
	}

	s.writeAPIResponse(w, requestID, RepoRegisterData{
		RepoID:                  repoIdentity.RepoID,
		RepoName:                repoDisplayName(repoIdentity.RepoKey, preferredRoot, repoIdentity.RepoID),
		RepoKey:                 repoIdentity.RepoKey,
		Paths:                   entry.Paths,
		PreferredRoot:           preferredRoot,
		PreferredRootAccessible: accessible,
		LastSeenAt:              entry.LastSeenAt,
	})
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
	dto.RepoName = repoDisplayName(dto.RepoKey, dto.PreferredRoot, dto.RepoID)
	if dto.RepoName == dto.RepoID && len(dto.Paths) > 0 {
		dto.RepoName = repoDisplayName(dto.RepoKey, dto.Paths[0], dto.RepoID)
	}

	if dto.Paths == nil {
		dto.Paths = []string{}
	}

	return dto
}

func repoDisplayName(repoKey, preferredRoot, repoID string) string {
	if shortName := strings.TrimSpace(ids.RepoShortName(repoKey)); shortName != "" {
		return shortName
	}
	if label := strings.TrimSpace(repoKey); label != "" && !strings.HasPrefix(label, "path:") {
		return label
	}
	if root := strings.TrimSpace(preferredRoot); root != "" {
		if base := strings.TrimSpace(filepath.Base(root)); base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	return strings.TrimSpace(repoID)
}

func (s *Server) repoName(repoID string) string {
	rec, exists, _ := s.Store.LoadRepoRecord(repoID)
	if exists {
		return repoDisplayName(rec.RepoKey, rec.PreferredRoot, repoID)
	}
	return strings.TrimSpace(repoID)
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

func (s *Server) appendRepoEvent(repoID, kind string, data map[string]any) error {
	writer := s.RepoEvents
	if writer == nil {
		writer = eventlog.NewWriter("repo_id", s.Clock)
	}
	_, err := writer.Append(s.Store.RepoEventsPath(repoID), repoID, kind, data, eventlog.AppendOptions{})
	return err
}

// handleRepoRm handles POST /repos/rm.
func (s *Server) handleRepoRm(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)

	var req RepoRmRequest
	if err := decodeStrictJSON(r.Body, &req); err != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "", nil)
		return
	}

	repoRef := strings.TrimSpace(req.RepoRef)
	if repoRef == "" {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "repo_ref is required", "", nil)
		return
	}

	idx, err := s.Store.LoadRepoIndex()
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to load repo index: "+err.Error(), "", nil)
		return
	}

	refs := s.buildRepoRefs(idx)
	resolved, resolveErr := ids.ResolveRepoRef(repoRef, refs)
	if resolveErr != nil {
		switch e := resolveErr.(type) {
		case *ids.ErrRepoAmbiguous:
			candidates := make([]string, len(e.Candidates))
			for i, c := range e.Candidates {
				candidates[i] = c.RepoID
			}
			s.writeAPIError(w, http.StatusConflict, requestID, string(errors.ERepoIDAmbiguous), e.Error(), "use a more specific name, repo key, or full repo id", AmbiguousDetails{Candidates: candidates})
		default:
			s.writeAPIError(w, http.StatusNotFound, requestID, string(errors.ERepoNotFound), resolveErr.Error(), "run 'agency repo ls' to see registered repos, or 'agency repo add <path>' to register", nil)
		}
		return
	}

	unlock, err := s.repoLock.Lock(resolved.RepoID, "repo rm")
	if err != nil {
		var lockErr *agencylock.ErrLocked
		if stderrors.As(err, &lockErr) {
			s.writeAPIError(w, http.StatusConflict, requestID, string(errors.ERepoLocked), "repository is locked by another operation", "wait for the other operation to complete", nil)
			return
		}
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to acquire repo lock: "+err.Error(), "", nil)
		return
	}
	defer func() { _ = unlock() }()

	idx, err = s.Store.LoadRepoIndex()
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to load repo index: "+err.Error(), "", nil)
		return
	}

	repoKey := ""
	for key, entry := range idx.Repos {
		if entry.RepoID == resolved.RepoID {
			repoKey = key
			break
		}
	}
	if repoKey == "" {
		s.writeAPIError(w, http.StatusNotFound, requestID, string(errors.ERepoNotFound), "repo not found: "+repoRef, "run 'agency repo ls' to see registered repos, or 'agency repo add <path>' to register", nil)
		return
	}

	worktrees, err := store.ScanIntegrationWorktreesForRepo(s.Store.DataDir, resolved.RepoID)
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to scan integration worktrees: "+err.Error(), "", nil)
		return
	}
	for _, record := range worktrees {
		if record.Broken || record.Meta == nil || record.Meta.State == store.WorktreeStatePresent {
			s.writeAPIError(w, http.StatusConflict, requestID, string(errors.ERepoHasWorktrees), "integration worktrees exist for repo "+resolved.RepoID, "remove or archive repo worktrees before unregistering the repo", nil)
			return
		}
	}

	invocations, err := store.ScanInvocationsForRepo(s.Store.DataDir, resolved.RepoID)
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to scan invocations: "+err.Error(), "", nil)
		return
	}
	for _, record := range invocations {
		if record.Broken ||
			record.Meta == nil ||
			record.Meta.Status == store.InvocationStatusStarting ||
			record.Meta.Status == store.InvocationStatusRunning ||
			record.Meta.Status == store.InvocationStatusStopping {
			s.writeAPIError(w, http.StatusConflict, requestID, string(errors.ERepoHasInvocations), "active invocations exist for repo "+resolved.RepoID, "stop or finish repo invocations before unregistering the repo", nil)
			return
		}
	}

	if err := s.appendRepoEvent(resolved.RepoID, "agency.repo_rm_started", map[string]any{
		"repo_key": repoKey,
	}); err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to append repo event: "+err.Error(), "", nil)
		return
	}

	delete(idx.Repos, repoKey)
	if err := s.Store.SaveRepoIndex(idx); err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to update repo index: "+err.Error(), "", nil)
		return
	}

	if err := s.appendRepoEvent(resolved.RepoID, "agency.repo_rm_succeeded", map[string]any{
		"repo_key":           repoKey,
		"removed_from_index": true,
	}); err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to append repo event: "+err.Error(), "", nil)
		return
	}

	s.writeAPIResponse(w, requestID, RepoRmData{
		RepoID:           resolved.RepoID,
		RepoName:         repoDisplayName(repoKey, "", resolved.RepoID),
		RepoKey:          repoKey,
		RemovedFromIndex: true,
	})
}
