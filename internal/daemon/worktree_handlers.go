package daemon

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/landing"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// worktreeRmGitRemoveTimeout bounds the git worktree removal performed by the worktree rm mutation surface.
const worktreeRmGitRemoveTimeout = 30 * time.Second

// handleWorktreeCreate handles POST /worktrees/create.
func (s *Server) handleWorktreeCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req WorktreeCreateRequest
	if err := decodeStrictJSON(r.Body, &req); err != nil {
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
	if req.BaseBranch == "" {
		s.writeWorktreeError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "base_branch is required", "")
		return
	}

	// Canonicalize repo_root: Abs -> EvalSymlinks -> git rev-parse --show-toplevel
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
	insideManagedTree, err := s.isInsideAgencyManagedTree(repoRoot)
	if err != nil {
		s.writeWorktreeError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to inspect managed worktrees: "+err.Error(), "")
		return
	}
	if insideManagedTree {
		s.writeWorktreeError(w, http.StatusBadRequest, string(errors.EUnsafeRepoRoot),
			"repo_root is inside an agency-managed worktree",
			"use the original repository, not a sandbox or integration worktree")
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

	// Derive repo identity.
	originInfo := git.GetOriginInfo(ctx, s.Runner, repoRoot)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot, originInfo.URL)
	execCtx, err := s.resolveExecutionContext(repoRoot, repoIdentity.RepoID, "", "")
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeWorktreeError(w, http.StatusBadRequest, string(code), apiErrorMessage(err), "")
		return
	}

	fingerprint := worktreeCreateFingerprint(repoRoot, req, execCtx)
	if entry, isDuplicate, conflict := s.checkWorktreeIdempotency(repoIdentity.RepoID, req.IdempotencyKey, fingerprint); isDuplicate {
		if conflict {
			s.writeWorktreeError(w, http.StatusConflict, string(errors.EIdempotencyConflict),
				"idempotency_key was already used for a different worktree create request",
				"retry with the original request or choose a new idempotency_key")
			return
		}
		if s.writeIdempotentWorktreeCreate(w, repoIdentity.RepoID, entry) {
			return
		}
		s.writeWorktreeError(w, http.StatusConflict, string(errors.EStoreCorrupt),
			"idempotency_key was already accepted but worktree metadata is unreadable",
			"inspect worktree state before retrying")
		return
	}
	if record, exists, conflict, err := s.findWorktreeByIdempotencyKey(repoIdentity.RepoID, req.IdempotencyKey, fingerprint); err != nil {
		s.writeWorktreeError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to scan worktrees for idempotency: "+err.Error(), "")
		return
	} else if exists {
		if conflict {
			s.writeWorktreeError(w, http.StatusConflict, string(errors.EIdempotencyConflict),
				"idempotency_key was already used for a different worktree create request",
				"retry with the original request or choose a new idempotency_key")
			return
		}
		if record.Meta == nil || record.Broken {
			s.writeWorktreeError(w, http.StatusConflict, string(errors.EStoreCorrupt),
				"idempotency_key record exists but worktree metadata is unreadable",
				"inspect worktree state before retrying")
			return
		}
		s.recordWorktreeIdempotency(repoIdentity.RepoID, req.IdempotencyKey, record.WorktreeID, fingerprint, record.Meta.TreePath, record.Meta.Branch)
		s.writeWorktreeSuccess(w, record.WorktreeID, record.Meta.TreePath, record.Meta.Branch, repoIdentity.RepoID, record.Meta.ExecutionProfile, record.Meta.CheckoutRoot)
		return
	}

	// Check again under the repo lock below before mutating.

	// Acquire repo lock before mutation.
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

	if entry, isDuplicate, conflict := s.checkWorktreeIdempotency(repoIdentity.RepoID, req.IdempotencyKey, fingerprint); isDuplicate {
		if conflict {
			s.writeWorktreeError(w, http.StatusConflict, string(errors.EIdempotencyConflict),
				"idempotency_key was already used for a different worktree create request",
				"retry with the original request or choose a new idempotency_key")
			return
		}
		if s.writeIdempotentWorktreeCreate(w, repoIdentity.RepoID, entry) {
			return
		}
		s.writeWorktreeError(w, http.StatusConflict, string(errors.EStoreCorrupt),
			"idempotency_key was already accepted but worktree metadata is unreadable",
			"inspect worktree state before retrying")
		return
	}
	if record, exists, conflict, err := s.findWorktreeByIdempotencyKey(repoIdentity.RepoID, req.IdempotencyKey, fingerprint); err != nil {
		s.writeWorktreeError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to scan worktrees for idempotency: "+err.Error(), "")
		return
	} else if exists {
		if conflict {
			s.writeWorktreeError(w, http.StatusConflict, string(errors.EIdempotencyConflict),
				"idempotency_key was already used for a different worktree create request",
				"retry with the original request or choose a new idempotency_key")
			return
		}
		if record.Meta == nil || record.Broken {
			s.writeWorktreeError(w, http.StatusConflict, string(errors.EStoreCorrupt),
				"idempotency_key record exists but worktree metadata is unreadable",
				"inspect worktree state before retrying")
			return
		}
		s.recordWorktreeIdempotency(repoIdentity.RepoID, req.IdempotencyKey, record.WorktreeID, fingerprint, record.Meta.TreePath, record.Meta.Branch)
		s.writeWorktreeSuccess(w, record.WorktreeID, record.Meta.TreePath, record.Meta.Branch, repoIdentity.RepoID, record.Meta.ExecutionProfile, record.Meta.CheckoutRoot)
		return
	}

	// Ensure repo is registered.
	if err := s.ensureRepoRegistered(repoIdentity, repoRoot); err != nil {
		s.writeWorktreeError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to register repo: "+err.Error(), "")
		return
	}

	// Write/update repo.json.
	if err := s.ensureRepoRecord(repoIdentity, repoRoot, originInfo); err != nil {
		s.writeWorktreeError(w, http.StatusInternalServerError, "E_INTERNAL",
			"failed to write repo.json: "+err.Error(), "")
		return
	}

	wtSvc := integrationworktree.NewService(s.Store, s.Runner, s.FS, s.Clock)
	createResult, err := wtSvc.Create(ctx, integrationworktree.CreateOpts{
		Name:               req.Name,
		RepoRoot:           repoRoot,
		RepoID:             repoIdentity.RepoID,
		BaseBranch:         req.BaseBranch,
		CheckoutRoot:       execCtx.CheckoutRoot,
		ExecutionProfile:   execCtx.Profile,
		IdempotencyKey:     req.IdempotencyKey,
		RequestFingerprint: fingerprint,
	})
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeWorktreeError(w, http.StatusInternalServerError, string(code), err.Error(), "")
		return
	}

	// Record idempotency entry.
	s.recordWorktreeIdempotency(repoIdentity.RepoID, req.IdempotencyKey, createResult.WorktreeID, fingerprint, createResult.TreePath, createResult.Branch)

	// Return success.
	s.writeWorktreeSuccess(w, createResult.WorktreeID, createResult.TreePath, createResult.Branch, repoIdentity.RepoID, execCtx.Profile, execCtx.CheckoutRoot)
}

func (s *Server) writeIdempotentWorktreeCreate(w http.ResponseWriter, repoID string, entry WorktreeIdempotencyEntry) bool {
	if entry.WorktreeID == "" {
		return false
	}
	if meta, err := s.Store.ReadIntegrationWorktreeMeta(repoID, entry.WorktreeID); err == nil && meta != nil {
		s.writeWorktreeSuccess(w, entry.WorktreeID, meta.TreePath, meta.Branch, repoID, meta.ExecutionProfile, meta.CheckoutRoot)
		return true
	}
	return false
}

// handleWorktreeRm handles POST /worktrees/{id}/rm.
func (s *Server) handleWorktreeRm(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	ctx := r.Context()

	// Parse request body
	var req WorktreeRmRequest
	if err := decodeOptionalStrictJSON(r.Body, &req); err != nil {
		s.writeWorktreeRmError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "")
		return
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

	repoRoot := ""
	for _, root := range []string{repoRecord.PreferredRoot, repoRecord.RepoRootLastSeen} {
		if resolved, ok := canonicalAccessibleDir(root); ok {
			repoRoot = resolved
			break
		}
	}
	if repoRoot == "" {
		s.writeWorktreeRmError(w, http.StatusInternalServerError, "E_INTERNAL",
			"repo root is not accessible from repo.json", "")
		return
	}

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

	if err := s.ensureWorktreeMergeInactive(repoID, record.WorktreeID, "remove the worktree"); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EWorktreeMergeActive
		}
		s.writeWorktreeRmError(w, http.StatusConflict, string(code), err.Error(), mergeHintFromError(err))
		return
	}

	unresolved, err := s.unresolvedInvocationsForWorktree(repoID, record.WorktreeID)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeWorktreeRmError(w, http.StatusInternalServerError, string(code), err.Error(), mergeHintFromError(err))
		return
	}
	if len(unresolved) > 0 && !req.Force {
		s.writeWorktreeRmError(w, http.StatusConflict, string(errors.EWorktreeHasUnresolvedInvocations),
			fmt.Sprintf("%d unresolved invocations exist for this worktree", len(unresolved)),
			"run 'agency agent ls --worktree "+worktreeRef+"' and land or discard each invocation")
		return
	}

	if req.Force && len(unresolved) > 0 {
		discardSvc := landing.NewServiceWithWriter(s.Store, s.Runner, s.FS, s.Clock, s.InvocationEvents)
		for _, invocation := range unresolved {
			if err := discardSvc.Discard(ctx, landing.DiscardOpts{
				RepoID:       repoID,
				InvocationID: invocation.InvocationID,
				RepoRoot:     repoRoot,
				StopCallback: s.stopInvocationForDiscard,
			}); err != nil {
				code := errors.GetCode(err)
				if code == "" {
					code = errors.ELandFailed
				}
				s.writeWorktreeRmError(w, http.StatusConflict, string(code), err.Error(), mergeHintFromError(err))
				return
			}
		}
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

	removeCtx, cancel := context.WithTimeout(ctx, worktreeRmGitRemoveTimeout)
	defer cancel()

	result, runErr := s.Runner.Run(removeCtx, "git", args, exec.RunOpts{})
	if runErr != nil {
		if stderrors.Is(runErr, context.DeadlineExceeded) {
			s.writeWorktreeRmError(w, http.StatusInternalServerError, string(errors.EWorktreeRemoveFailed),
				"git worktree remove timed out", "retry the removal or inspect the worktree for a blocked git process")
			return
		}
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
		PreferredRoot:    repoRoot,
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
