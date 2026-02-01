package daemon

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// handleWorktreeCreate handles POST /worktrees/create.
func (s *Server) handleWorktreeCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req WorktreeCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeWorktreeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "")
		return
	}

	// 1. Validate required fields
	if req.RepoRoot == "" {
		s.writeWorktreeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "repo_root is required", "")
		return
	}
	if req.Name == "" {
		s.writeWorktreeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "name is required", "")
		return
	}

	// 2. Validate name format
	if err := core.ValidateName(req.Name); err != nil {
		s.writeWorktreeError(w, http.StatusBadRequest, string(errors.EInvalidName),
			"invalid worktree name: "+err.Error(),
			"names must be 2-40 chars, lowercase alphanumeric + hyphens")
		return
	}

	// 3. Canonicalize repo_root: Abs -> EvalSymlinks -> git rev-parse --show-toplevel
	repoRoot, err := filepath.Abs(req.RepoRoot)
	if err != nil {
		s.writeWorktreeError(w, http.StatusBadRequest, "E_INVALID_REQUEST",
			"failed to resolve repo_root: "+err.Error(), "")
		return
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		s.writeWorktreeError(w, http.StatusBadRequest, "E_INVALID_REQUEST",
			"failed to resolve repo_root symlinks: "+err.Error(), "")
		return
	}

	// Derive git root via git rev-parse --show-toplevel
	gitRoot, err := git.GetRepoRoot(ctx, s.Runner, repoRoot)
	if err != nil {
		s.writeWorktreeError(w, http.StatusBadRequest, string(errors.ENoRepo),
			"repo_root is not inside a git repository: "+err.Error(), "")
		return
	}
	repoRoot = gitRoot.Path

	// 4. Derive repo identity
	originInfo := git.GetOriginInfo(ctx, s.Runner, repoRoot)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot, originInfo.URL)

	// 5. Check idempotency before acquiring lock
	if entry, isDuplicate := s.checkWorktreeIdempotency(repoIdentity.RepoID, req.IdempotencyKey); isDuplicate {
		// Return the existing worktree
		s.writeWorktreeSuccess(w, entry.WorktreeID, entry.TreePath, entry.Branch, repoIdentity.RepoID)
		return
	}

	// 6. Determine parent branch
	parentBranch := req.ParentBranch
	if parentBranch == "" {
		result, err := s.Runner.Run(ctx, "git", []string{"-C", repoRoot, "rev-parse", "--abbrev-ref", "HEAD"}, exec.RunOpts{})
		if err != nil || result.ExitCode != 0 {
			s.writeWorktreeError(w, http.StatusBadRequest, string(errors.EParentBranchNotFound),
				"failed to determine current branch; use parent_branch to specify", "")
			return
		}
		parentBranch = strings.TrimSpace(result.Stdout)
	}

	// 7. Acquire repo lock BEFORE name check and branch generation
	unlock, err := s.repoLock.Lock(repoIdentity.RepoID, "worktree create")
	if err != nil {
		var lockErr *lock.ErrLocked
		if stderrors.As(err, &lockErr) {
			s.writeWorktreeError(w, http.StatusConflict, string(errors.ERepoLocked),
				"repository is locked by another operation", "wait for the other operation to complete")
			return
		}
		s.writeWorktreeError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to acquire repo lock: "+err.Error(), "")
		return
	}
	defer func() { _ = unlock() }()

	// 8. Ensure repo is registered
	if err := s.ensureRepoRegistered(repoIdentity, repoRoot); err != nil {
		s.writeWorktreeError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to register repo: "+err.Error(), "")
		return
	}

	// 9. Write/update repo.json
	if err := s.ensureRepoRecord(repoIdentity, repoRoot, originInfo); err != nil {
		s.writeWorktreeError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to write repo.json: "+err.Error(), "")
		return
	}

	// 10. Check name uniqueness among non-archived worktrees
	records, err := store.ScanIntegrationWorktreesForRepo(s.Store.DataDir, repoIdentity.RepoID)
	if err != nil {
		s.writeWorktreeError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to scan integration worktrees: "+err.Error(), "")
		return
	}

	refs := make([]ids.WorktreeRef, len(records))
	for i, r := range records {
		state := ""
		if r.Meta != nil {
			state = string(r.Meta.State)
		}
		refs[i] = ids.WorktreeRef{
			WorktreeID: r.WorktreeID,
			RepoID:     r.RepoID,
			Name:       r.Name,
			State:      state,
			Broken:     r.Broken,
		}
	}

	if err := ids.CheckWorktreeNameUnique(req.Name, refs); err != nil {
		s.writeWorktreeError(w, http.StatusConflict, string(errors.EWorktreeNameExists),
			err.Error(), "use a different name")
		return
	}

	// 11. Generate worktree_id
	worktreeID, err := core.NewRunID(s.Clock())
	if err != nil {
		s.writeWorktreeError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to generate worktree_id: "+err.Error(), "")
		return
	}

	// 12. Compute branch name and tree path
	branch := core.BranchName(req.Name, worktreeID)
	treePath := s.Store.IntegrationWorktreeTreePath(repoIdentity.RepoID, worktreeID)

	// 13. Create record directory (exclusive)
	_, err = s.Store.EnsureIntegrationWorktreeDir(repoIdentity.RepoID, worktreeID)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeWorktreeError(w, http.StatusInternalServerError, string(code),
			err.Error(), "")
		return
	}

	// Track cleanup state
	recordDirCreated := true
	gitWorktreeCreated := false
	branchCreated := false

	// Cleanup function for rollback on partial failure
	cleanup := func() {
		if gitWorktreeCreated {
			args := []string{"-C", repoRoot, "worktree", "remove", "--force", treePath}
			_, _ = s.Runner.Run(ctx, "git", args, exec.RunOpts{})
		}
		if branchCreated {
			args := []string{"-C", repoRoot, "branch", "-D", branch}
			_, _ = s.Runner.Run(ctx, "git", args, exec.RunOpts{})
		}
		if recordDirCreated {
			_ = s.Store.RemoveIntegrationWorktreeDir(repoIdentity.RepoID, worktreeID)
		}
	}

	// 14. Create git worktree + branch
	args := []string{
		"-C", repoRoot,
		"worktree", "add",
		"-b", branch,
		treePath,
		parentBranch,
	}

	result, err := s.Runner.Run(ctx, "git", args, exec.RunOpts{})
	if err != nil {
		cleanup()
		s.writeWorktreeError(w, http.StatusInternalServerError, string(errors.EWorktreeCreateFailed),
			"failed to execute git worktree add: "+err.Error(), "")
		return
	}
	if result.ExitCode != 0 {
		cleanup()
		s.writeWorktreeError(w, http.StatusInternalServerError, string(errors.EWorktreeCreateFailed),
			"git worktree add failed: "+strings.TrimSpace(result.Stderr), "")
		return
	}

	gitWorktreeCreated = true
	branchCreated = true

	// 15. Create .agency/ directory and write INTEGRATION_MARKER
	agencyDir := filepath.Join(treePath, ".agency")
	if err := s.FS.MkdirAll(agencyDir, 0o755); err != nil {
		cleanup()
		s.writeWorktreeError(w, http.StatusInternalServerError, string(errors.EWorktreeCreateFailed),
			"failed to create .agency directory: "+err.Error(), "")
		return
	}

	markerPath := filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName)
	markerContent := "# This directory is an integration worktree.\n# Runners must not execute here.\n"
	if err := s.FS.WriteFile(markerPath, []byte(markerContent), 0o644); err != nil {
		cleanup()
		s.writeWorktreeError(w, http.StatusInternalServerError, string(errors.EWorktreeCreateFailed),
			"failed to write INTEGRATION_MARKER: "+err.Error(), "")
		return
	}

	// 16. Write meta.json
	meta := store.NewIntegrationWorktreeMeta(
		worktreeID,
		req.Name,
		repoIdentity.RepoID,
		branch,
		parentBranch,
		treePath,
		s.Clock(),
	)

	if err := s.Store.WriteIntegrationWorktreeMeta(repoIdentity.RepoID, worktreeID, meta); err != nil {
		cleanup()
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeWorktreeError(w, http.StatusInternalServerError, string(code),
			err.Error(), "")
		return
	}

	// 17. Record idempotency entry
	s.recordWorktreeIdempotency(repoIdentity.RepoID, req.IdempotencyKey, worktreeID, treePath, branch)

	// 18. Return success
	s.writeWorktreeSuccess(w, worktreeID, treePath, branch, repoIdentity.RepoID)
}

// handleWorktreeRm handles POST /worktrees/{id}/rm.
func (s *Server) handleWorktreeRm(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	ctx := r.Context()

	// Parse request body
	var req WorktreeRmRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeWorktreeRmError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "")
			return
		}
	}

	// Get repo_id from query params (required for resolution)
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeWorktreeRmError(w, http.StatusBadRequest, "E_INVALID_REQUEST",
			"repo_id query parameter is required", "")
		return
	}

	// Load repo record to get repo_root
	repoRecord, exists, err := s.Store.LoadRepoRecord(repoID)
	if err != nil {
		s.writeWorktreeRmError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to load repo record: "+err.Error(), "")
		return
	}
	if !exists {
		s.writeWorktreeRmError(w, http.StatusNotFound, string(errors.ERepoNotFound),
			"repo not found", "")
		return
	}

	// Get repo_root from index (repo record doesn't store path)
	repoRoot, err := s.getRepoRootFromIndex(repoID)
	if err != nil {
		s.writeWorktreeRmError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to get repo root: "+err.Error(), "")
		return
	}
	_ = repoRecord // Used for validation only

	// Acquire repo lock
	unlock, err := s.repoLock.Lock(repoID, "worktree rm")
	if err != nil {
		var lockErr *lock.ErrLocked
		if stderrors.As(err, &lockErr) {
			s.writeWorktreeRmError(w, http.StatusConflict, string(errors.ERepoLocked),
				"repository is locked by another operation", "wait for the other operation to complete")
			return
		}
		s.writeWorktreeRmError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to acquire repo lock: "+err.Error(), "")
		return
	}
	defer func() { _ = unlock() }()

	// Resolve worktree by id/name/prefix
	wtSvc := integrationworktree.NewService(s.Store, s.Runner, s.FS, s.Clock)
	record, err := wtSvc.Resolve(repoID, worktreeRef, false)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeWorktreeRmError(w, http.StatusNotFound, string(code),
			err.Error(), "run 'agency worktree ls' to see available worktrees")
		return
	}

	if record.Broken {
		s.writeWorktreeRmError(w, http.StatusBadRequest, string(errors.EWorktreeBroken),
			"worktree exists but meta.json is unreadable", "remove manually")
		return
	}

	// Check if already archived (idempotent rm)
	if record.Meta.State == store.WorktreeStateArchived {
		// Already archived - return success (idempotent)
		s.writeWorktreeRmSuccess(w)
		return
	}

	// Verify tree contains INTEGRATION_MARKER
	if !integrationworktree.HasIntegrationMarker(record.Meta.TreePath) {
		s.writeWorktreeRmError(w, http.StatusBadRequest, string(errors.ENotAnIntegrationWorktree),
			"tree missing .agency/INTEGRATION_MARKER - not an integration worktree",
			"this safety check prevents accidentally deleting user-managed worktrees")
		return
	}

	// Check for active invocations targeting this worktree
	activeInvocations := s.getActiveInvocationsForWorktree(record.WorktreeID)
	if len(activeInvocations) > 0 && !req.Force {
		s.writeWorktreeRmError(w, http.StatusConflict, string(errors.EWorktreeHasActiveInvocations),
			fmt.Sprintf("%d active invocations exist for this worktree", len(activeInvocations)),
			"use --force to stop invocations and remove, or wait for them to complete")
		return
	}

	// If force and active invocations, stop them first
	if req.Force && len(activeInvocations) > 0 {
		s.forceStopAndDiscardInvocations(ctx, repoID, repoRoot, activeInvocations)
	}

	// Check if tree is dirty (unless force)
	if !req.Force {
		clean, err := git.IsClean(ctx, s.Runner, record.Meta.TreePath)
		if err != nil {
			// Can't determine cleanliness - proceed anyway
		} else if !clean {
			s.writeWorktreeRmError(w, http.StatusConflict, string(errors.EDirtyWorktree),
				"worktree has uncommitted changes",
				"commit/stash your changes or use --force")
			return
		}
	}

	// Remove git worktree
	args := []string{"-C", repoRoot, "worktree", "remove"}
	if req.Force {
		args = append(args, "--force")
	}
	args = append(args, record.Meta.TreePath)

	result, runErr := s.Runner.Run(ctx, "git", args, exec.RunOpts{})
	if runErr != nil {
		s.writeWorktreeRmError(w, http.StatusInternalServerError, string(errors.EWorktreeRemoveFailed),
			"failed to execute git worktree remove: "+runErr.Error(), "")
		return
	}

	if result.ExitCode != 0 {
		stderr := strings.TrimSpace(result.Stderr)
		// Check for dirty worktree error
		if !req.Force && (strings.Contains(stderr, "untracked") || strings.Contains(stderr, "modified")) {
			s.writeWorktreeRmError(w, http.StatusConflict, string(errors.EDirtyWorktree),
				"worktree has uncommitted changes",
				"commit/stash your changes or use --force")
			return
		}
		s.writeWorktreeRmError(w, http.StatusInternalServerError, string(errors.EWorktreeRemoveFailed),
			"git worktree remove failed: "+stderr, "")
		return
	}

	// Update meta.json to archived state
	err = s.Store.UpdateIntegrationWorktreeMeta(repoID, record.WorktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.State = store.WorktreeStateArchived
	})
	// Log but don't fail - worktree is already removed
	// Next rm attempt will detect archived state and succeed (idempotent)
	_ = err

	s.writeWorktreeRmSuccess(w)
}

// getActiveInvocationsForWorktree returns invocation IDs supervised by this daemon that target the given worktree.
func (s *Server) getActiveInvocationsForWorktree(worktreeID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var active []string
	for invID, proc := range s.processes {
		if proc.IntegrationWorktreeID == worktreeID {
			active = append(active, invID)
		}
	}
	return active
}

// forceStopAndDiscardInvocations stops and discards invocations: SIGINT → 5s → SIGKILL → discard.
func (s *Server) forceStopAndDiscardInvocations(ctx context.Context, repoID, repoRoot string, invocationIDs []string) {
	for _, invID := range invocationIDs {
		s.mu.RLock()
		proc, exists := s.processes[invID]
		s.mu.RUnlock()

		if !exists {
			continue
		}

		// Send SIGINT
		_ = syscall.Kill(-proc.PGID, syscall.SIGINT)

		// Wait up to 5 seconds
		select {
		case <-proc.done:
			// Exited gracefully
		case <-time.After(5 * time.Second):
			// Force kill
			_ = syscall.Kill(-proc.PGID, syscall.SIGKILL)
			// Wait a bit for process to die
			select {
			case <-proc.done:
			case <-time.After(1 * time.Second):
			}
		}

		// Mark as failed
		now := s.Clock().UTC().Format(time.RFC3339)
		_ = s.Store.UpdateInvocationMeta(repoID, invID, func(meta *store.InvocationMeta) {
			meta.Status = store.InvocationStatusFailed
			meta.ExitReason = "killed"
			meta.FailureReason = "worktree_removed"
			meta.FinishedAt = now
			meta.PID = nil
			meta.LifecycleOwner = ""
			meta.LandingStatus = store.LandingStatusDiscarded
		})

		// Best-effort sandbox cleanup
		sandboxPath := s.Store.SandboxTreePath(repoID, invID)
		args := []string{"-C", repoRoot, "worktree", "remove", "--force", sandboxPath}
		_, _ = s.Runner.Run(ctx, "git", args, exec.RunOpts{})

		// Remove from supervised map
		s.mu.Lock()
		delete(s.processes, invID)
		s.mu.Unlock()
	}
}

// ensureRepoRecord writes/updates repo.json for the repo.
func (s *Server) ensureRepoRecord(repoIdentity identity.RepoIdentity, repoRoot string, originInfo git.OriginInfo) error {
	// Check if repo record exists
	existing, exists, err := s.Store.LoadRepoRecord(repoIdentity.RepoID)
	if err != nil {
		return err
	}

	// Create repo directory if needed
	repoDir := s.Store.RepoDir(repoIdentity.RepoID)
	if err := s.FS.MkdirAll(repoDir, 0o700); err != nil {
		return fmt.Errorf("failed to create repo directory: %w", err)
	}

	// Build repo record input
	input := store.BuildRepoRecordInput{
		RepoKey:          repoIdentity.RepoKey,
		RepoID:           repoIdentity.RepoID,
		RepoRootLastSeen: repoRoot,
		OriginPresent:    originInfo.Present,
		OriginURL:        originInfo.URL,
		OriginHost:       originInfo.Host,
	}

	var rec store.RepoRecord
	if exists {
		rec = s.Store.UpsertRepoRecord(&existing, input)
	} else {
		rec = s.Store.UpsertRepoRecord(nil, input)
	}

	return s.Store.SaveRepoRecord(rec)
}

// getRepoRootFromIndex gets the repo root path from the repo index.
func (s *Server) getRepoRootFromIndex(repoID string) (string, error) {
	idx, err := s.Store.LoadRepoIndex()
	if err != nil {
		return "", err
	}

	for _, entry := range idx.Repos {
		if entry.RepoID == repoID {
			// Return the first path (there may be multiple)
			if len(entry.Paths) > 0 {
				return entry.Paths[0], nil
			}
		}
	}

	return "", fmt.Errorf("repo %s not found in index", repoID)
}

// writeWorktreeError writes an error response for worktree endpoints.
func (s *Server) writeWorktreeError(w http.ResponseWriter, status int, code, message, hint string) {
	resp := WorktreeCreateResponse{
		OK:           false,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
	}
	s.writeJSON(w, status, resp)
}

// writeWorktreeSuccess writes a success response for worktree create.
func (s *Server) writeWorktreeSuccess(w http.ResponseWriter, worktreeID, treePath, branch, repoID string) {
	resp := WorktreeCreateResponse{
		OK:           true,
		WorktreeID:   worktreeID,
		TreePath:     treePath,
		Branch:       branch,
		RepoID:       repoID,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// writeWorktreeRmError writes an error response for worktree rm.
func (s *Server) writeWorktreeRmError(w http.ResponseWriter, status int, code, message, hint string) {
	resp := WorktreeRmResponse{
		OK:           false,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
	}
	s.writeJSON(w, status, resp)
}

// writeWorktreeRmSuccess writes a success response for worktree rm.
func (s *Server) writeWorktreeRmSuccess(w http.ResponseWriter) {
	resp := WorktreeRmResponse{
		OK:           true,
		APIVersion:   APIVersion,
		BuildVersion: version.FullVersion(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}
