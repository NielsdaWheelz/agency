package daemon

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"os"
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

const (
	worktreeCreateEventStarted   = "agency.worktree_create_started"
	worktreeCreateEventSucceeded = "agency.worktree_create_succeeded"
	worktreeCreateEventFailed    = "agency.worktree_create_failed"
	worktreeRmEventStarted       = "agency.worktree_rm_started"
	worktreeRmEventSucceeded     = "agency.worktree_rm_succeeded"
	worktreeRmEventFailed        = "agency.worktree_rm_failed"
)

// handleWorktreeCreate handles POST /worktrees/create.
func (s *Server) handleWorktreeCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := prepareRequestID(w, r)

	// Parse request body
	var req WorktreeCreateRequest
	if err := decodeStrictJSON(r.Body, &req); err != nil {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "")
		return
	}

	// 1. Validate required fields
	if req.RepoRoot == "" {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "repo_root is required", "")
		return
	}
	if req.Name == "" {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "name is required", "")
		return
	}
	if req.BaseBranch == "" {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "base_branch is required", "")
		return
	}

	repoRoot, repoIdentity, ok := s.resolveControlPlaneRepoRoot(ctx, req.RepoRoot, func(status int, code, message, hint string) {
		s.writeErrorWithRequestID(w, status, requestID, code, message, hint)
	})
	if !ok {
		return
	}
	originInfo := git.GetOriginInfo(ctx, s.runner, repoRoot, nil)
	execCtx, err := s.resolveExecutionContext(repoRoot, repoIdentity.RepoID, "", "")
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(code), apiErrorMessage(err), "")
		return
	}

	fingerprint := worktreeCreateFingerprint(repoRoot, req, execCtx)
	if s.handleWorktreeCreateIdempotency(w, requestID, repoIdentity.RepoID, req.IdempotencyKey, fingerprint) {
		return
	}

	// Acquire repo lock before mutation.
	unlock, err := s.repoLock.Lock(repoIdentity.RepoID, "worktree create")
	if err != nil {
		var lockErr *lock.ErrLocked
		if stderrors.As(err, &lockErr) {
			s.writeErrorWithRequestID(w, http.StatusConflict, requestID, string(errors.ERepoLocked),
				"repository is locked by another operation", "wait for the other operation to complete")
			return
		}
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EInternal),
			"failed to acquire repo lock: "+err.Error(), "")
		return
	}
	defer func() { _ = unlock() }()

	// Re-check under the lock: another request could have completed between the
	// pre-lock check and lock acquisition.
	if s.handleWorktreeCreateIdempotency(w, requestID, repoIdentity.RepoID, req.IdempotencyKey, fingerprint) {
		return
	}

	// Ensure repo is registered.
	if err := s.ensureRepoRegistered(repoIdentity, repoRoot); err != nil {
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EInternal),
			"failed to register repo: "+err.Error(), "")
		return
	}

	// Write/update repo.json.
	if err := s.ensureRepoRecord(repoIdentity, repoRoot, originInfo); err != nil {
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EInternal),
			"failed to write repo.json: "+err.Error(), "")
		return
	}

	if err := s.appendRepoEvent(repoIdentity.RepoID, worktreeCreateEventStarted, map[string]any{
		"name":        req.Name,
		"base_branch": req.BaseBranch,
	}); err != nil {
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EPersistFailed),
			"failed to append worktree_create_started event: "+err.Error(), "")
		return
	}

	wtSvc := integrationworktree.NewService(s.store, s.runner, s.fsys, s.clock)
	createResult, err := wtSvc.Create(ctx, integrationworktree.CreateOpts{
		Name:               req.Name,
		RepoRoot:           repoRoot,
		RepoID:             repoIdentity.RepoID,
		BaseBranch:         req.BaseBranch,
		CheckoutRoot:       execCtx.CheckoutRoot,
		ExecutionProfile:   execCtx.Profile,
		Env:                prSyncNonInteractiveEnv(execCtx.ProfileEnv),
		IdempotencyKey:     req.IdempotencyKey,
		RequestFingerprint: fingerprint,
	})
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		if emitErr := s.appendRepoEvent(repoIdentity.RepoID, worktreeCreateEventFailed, map[string]any{
			"name":       req.Name,
			"error_code": string(code),
			"message":    apiErrorMessage(err),
		}); emitErr != nil {
			s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EPersistFailed),
				"failed to append worktree_create_failed event: "+emitErr.Error(), "")
			return
		}
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(code), err.Error(), "")
		return
	}

	// Record idempotency entry.
	s.recordWorktreeIdempotency(repoIdentity.RepoID, req.IdempotencyKey, createResult.WorktreeID, fingerprint)

	if err := s.appendWorktreeEvent(repoIdentity.RepoID, createResult.WorktreeID, worktreeCreateEventSucceeded, map[string]any{
		"name":        req.Name,
		"branch":      createResult.Branch,
		"base_branch": req.BaseBranch,
		"tree_path":   createResult.TreePath,
	}); err != nil {
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EPersistFailed),
			"failed to append worktree_create_succeeded event: "+err.Error(), "")
		return
	}

	// Return success.
	s.writeWorktreeSuccess(w, requestID, createResult.WorktreeID, createResult.TreePath, createResult.Branch, repoIdentity.RepoID, execCtx.Profile, execCtx.CheckoutRoot)
}

func (s *Server) writeIdempotentWorktreeCreate(w http.ResponseWriter, requestID, repoID string, entry worktreeIdempotencyEntry) bool {
	if entry.worktreeID == "" {
		return false
	}
	if meta, err := s.store.ReadIntegrationWorktreeMeta(repoID, entry.worktreeID); err == nil && meta != nil {
		s.writeWorktreeSuccess(w, requestID, entry.worktreeID, meta.TreePath, meta.Branch, repoID, meta.ExecutionProfile, meta.CheckoutRoot)
		return true
	}
	return false
}

// handleWorktreeCreateIdempotency resolves a duplicate worktree-create request:
// it consults the in-memory cache first, then scans the store for a matching
// record. It writes the appropriate response and returns true if the request
// was fully handled (either as a successful replay or a conflict). The caller
// must invoke this once before the repo lock (to short-circuit known duplicates)
// and once after the lock is held (to close the race between checks).
func (s *Server) handleWorktreeCreateIdempotency(w http.ResponseWriter, requestID, repoID, idempotencyKey, fingerprint string) bool {
	if entry, isDuplicate, conflict := s.checkWorktreeIdempotency(repoID, idempotencyKey, fingerprint); isDuplicate {
		if conflict {
			s.writeErrorWithRequestID(w, http.StatusConflict, requestID, string(errors.EIdempotencyConflict),
				"idempotency_key was already used for a different worktree create request",
				"retry with the original request or choose a new idempotency_key")
			return true
		}
		if s.writeIdempotentWorktreeCreate(w, requestID, repoID, entry) {
			return true
		}
		s.writeErrorWithRequestID(w, http.StatusConflict, requestID, string(errors.EStoreCorrupt),
			"idempotency_key was already accepted but worktree metadata is unreadable",
			"inspect worktree state before retrying")
		return true
	}
	record, exists, conflict, err := s.findWorktreeByIdempotencyKey(repoID, idempotencyKey, fingerprint)
	if err != nil {
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EInternal),
			"failed to scan worktrees for idempotency: "+err.Error(), "")
		return true
	}
	if !exists {
		return false
	}
	if conflict {
		s.writeErrorWithRequestID(w, http.StatusConflict, requestID, string(errors.EIdempotencyConflict),
			"idempotency_key was already used for a different worktree create request",
			"retry with the original request or choose a new idempotency_key")
		return true
	}
	if record.Meta == nil || record.Broken {
		s.writeErrorWithRequestID(w, http.StatusConflict, requestID, string(errors.EStoreCorrupt),
			"idempotency_key record exists but worktree metadata is unreadable",
			"inspect worktree state before retrying")
		return true
	}
	s.recordWorktreeIdempotency(repoID, idempotencyKey, record.WorktreeID, fingerprint)
	s.writeWorktreeSuccess(w, requestID, record.WorktreeID, record.Meta.TreePath, record.Meta.Branch, repoID, record.Meta.ExecutionProfile, record.Meta.CheckoutRoot)
	return true
}

// handleWorktreeRm handles POST /worktrees/{id}/rm.
func (s *Server) handleWorktreeRm(w http.ResponseWriter, r *http.Request, worktreeRef string) {
	ctx := r.Context()
	requestID := prepareRequestID(w, r)

	// Parse request body
	var req WorktreeRmRequest
	if err := decodeOptionalStrictJSON(r.Body, &req); err != nil {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "")
		return
	}

	// Get repo_id from query params (required for resolution)
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest),
			"repo_id query parameter is required", "")
		return
	}

	wtSvc := integrationworktree.NewService(s.store, s.runner, s.fsys, s.clock)
	record, err := wtSvc.Resolve(repoID, worktreeRef, true)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		s.writeErrorWithRequestID(w, http.StatusNotFound, requestID, string(code),
			err.Error(), "run 'agency worktree ls' to see available worktrees")
		return
	}

	if record.Broken || record.Meta == nil {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.EWorktreeBroken),
			"worktree exists but meta.json is unreadable", "remove manually")
		return
	}

	if record.Meta.State == store.WorktreeStateArchived {
		s.writeWorktreeRmSuccess(w, requestID)
		return
	}

	// Load repo record to get repo_root
	repoRecord, exists, err := s.store.LoadRepoRecord(repoID)
	if err != nil {
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EInternal),
			"failed to load repo record: "+err.Error(), "")
		return
	}
	if !exists {
		s.writeErrorWithRequestID(w, http.StatusNotFound, requestID, string(errors.ERepoNotFound),
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
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EInternal),
			"repo root is not accessible from repo.json", "")
		return
	}

	// Acquire repo lock
	unlock, err := s.repoLock.Lock(repoID, "worktree rm")
	if err != nil {
		var lockErr *lock.ErrLocked
		if stderrors.As(err, &lockErr) {
			s.writeErrorWithRequestID(w, http.StatusConflict, requestID, string(errors.ERepoLocked),
				"repository is locked by another operation", "wait for the other operation to complete")
			return
		}
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EInternal),
			"failed to acquire repo lock: "+err.Error(), "")
		return
	}
	defer func() { _ = unlock() }()

	latestMeta, err := s.store.ReadIntegrationWorktreeMeta(repoID, record.WorktreeID)
	if err != nil {
		code := errors.CodeOr(err, errors.EWorktreeBroken)
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(code), apiErrorMessage(err), "inspect or recreate the worktree")
		return
	}
	record.Meta = latestMeta
	if record.Meta.State == store.WorktreeStateArchived {
		s.writeWorktreeRmSuccess(w, requestID)
		return
	}

	treeMissing := false
	if info, err := s.fsys.Stat(record.Meta.TreePath); err != nil {
		if os.IsNotExist(err) {
			treeMissing = true
		} else {
			s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EWorktreeRemoveFailed),
				"failed to inspect worktree tree: "+err.Error(), "")
			return
		}
	} else if !info.IsDir() {
		s.writeErrorWithRequestID(w, http.StatusBadRequest, requestID, string(errors.ENotAnIntegrationWorktree),
			"worktree path is not a directory",
			"this safety check prevents accidentally deleting user-managed paths")
		return
	}

	if err := s.ensureWorktreeMergeInactive(repoID, record.WorktreeID, "remove the worktree"); err != nil {
		code := errors.CodeOr(err, errors.EWorktreeMergeActive)
		s.writeErrorWithRequestID(w, http.StatusConflict, requestID, string(code), err.Error(), errors.Hint(err))
		return
	}

	unresolved, err := s.unresolvedInvocationsForWorktree(repoID, record.WorktreeID)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(code), err.Error(), errors.Hint(err))
		return
	}
	if len(unresolved) > 0 && !req.Force {
		s.writeErrorWithRequestID(w, http.StatusConflict, requestID, string(errors.EWorktreeHasUnresolvedInvocations),
			fmt.Sprintf("%d unresolved invocations exist for this worktree", len(unresolved)),
			"run 'agency agent ls --worktree "+worktreeRef+"' and land or discard each invocation")
		return
	}

	if err := s.appendWorktreeEvent(repoID, record.WorktreeID, worktreeRmEventStarted, map[string]any{
		"force":        req.Force,
		"unresolved":   len(unresolved),
		"tree_missing": treeMissing,
	}); err != nil {
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EPersistFailed),
			"failed to append worktree_rm_started event: "+err.Error(), "")
		return
	}

	if req.Force && len(unresolved) > 0 {
		discardSvc := landing.NewService(s.store, s.runner, s.fsys, s.clock, s.invocationEvents)
		for _, invocation := range unresolved {
			profileEnv, err := s.executionProfileEnv(invocation.Meta.ExecutionProfile)
			if err != nil {
				code := errors.CodeOr(err, errors.EExecutionProfileNotFound)
				s.failWorktreeRm(w, http.StatusBadRequest, requestID, repoID, record.WorktreeID, string(code), apiErrorMessage(err), "")
				return
			}
			if err := discardSvc.Discard(ctx, landing.DiscardOpts{
				RepoID:       repoID,
				InvocationID: invocation.InvocationID,
				RepoRoot:     repoRoot,
				Env:          prSyncNonInteractiveEnv(profileEnv),
				StopCallback: s.stopInvocationForDiscard,
			}); err != nil {
				code := errors.CodeOr(err, errors.ELandFailed)
				s.failWorktreeRm(w, http.StatusConflict, requestID, repoID, record.WorktreeID, string(code), err.Error(), errors.Hint(err))
				return
			}
		}
	}

	if treeMissing {
		if err := s.store.UpdateIntegrationWorktreeMeta(repoID, record.WorktreeID, func(m *store.IntegrationWorktreeMeta) {
			m.State = store.WorktreeStateArchived
		}); err != nil {
			s.failWorktreeRm(w, http.StatusInternalServerError, requestID, repoID, record.WorktreeID, string(errors.EWorktreeRemoveFailed),
				"worktree tree is missing and metadata archive failed: "+err.Error(),
				"inspect worktree metadata before retrying")
			return
		}
		s.finishWorktreeRm(w, requestID, repoID, record.WorktreeID, true)
		return
	}

	// Verify tree contains INTEGRATION_MARKER
	if !integrationworktree.HasIntegrationMarker(record.Meta.TreePath) {
		s.failWorktreeRm(w, http.StatusBadRequest, requestID, repoID, record.WorktreeID, string(errors.ENotAnIntegrationWorktree),
			"tree missing .agency/INTEGRATION_MARKER - not an integration worktree",
			"this safety check prevents accidentally deleting user-managed worktrees")
		return
	}

	profileEnv, err := s.executionProfileEnv(record.Meta.ExecutionProfile)
	if err != nil {
		code := errors.CodeOr(err, errors.EExecutionProfileNotFound)
		s.failWorktreeRm(w, http.StatusBadRequest, requestID, repoID, record.WorktreeID, string(code), apiErrorMessage(err), "")
		return
	}
	worktreeEnv := prSyncNonInteractiveEnv(profileEnv)

	// Check if tree is dirty (unless force)
	if !req.Force {
		clean, err := git.IsClean(ctx, s.runner, record.Meta.TreePath, worktreeEnv)
		if err != nil {
			s.failWorktreeRm(w, http.StatusInternalServerError, requestID, repoID, record.WorktreeID, string(errors.EWorktreeRemoveFailed),
				"failed to check worktree cleanliness: "+err.Error(), "")
			return
		} else if !clean {
			s.failWorktreeRm(w, http.StatusConflict, requestID, repoID, record.WorktreeID, string(errors.EDirtyWorktree),
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

	result, runErr := s.runner.Run(removeCtx, "git", args, exec.RunOpts{Env: worktreeEnv})
	if runErr != nil {
		if stderrors.Is(runErr, context.DeadlineExceeded) {
			s.failWorktreeRm(w, http.StatusInternalServerError, requestID, repoID, record.WorktreeID, string(errors.EWorktreeRemoveFailed),
				"git worktree remove timed out", "retry the removal or inspect the worktree for a blocked git process")
			return
		}
		s.failWorktreeRm(w, http.StatusInternalServerError, requestID, repoID, record.WorktreeID, string(errors.EWorktreeRemoveFailed),
			"failed to execute git worktree remove: "+runErr.Error(), "")
		return
	}

	if result.ExitCode != 0 {
		stderr := strings.TrimSpace(result.Stderr)
		// Check for dirty worktree error
		if !req.Force && (strings.Contains(stderr, "untracked") || strings.Contains(stderr, "modified")) {
			s.failWorktreeRm(w, http.StatusConflict, requestID, repoID, record.WorktreeID, string(errors.EDirtyWorktree),
				"worktree has uncommitted changes",
				"commit/stash your changes or use --force")
			return
		}
		s.failWorktreeRm(w, http.StatusInternalServerError, requestID, repoID, record.WorktreeID, string(errors.EWorktreeRemoveFailed),
			"git worktree remove failed: "+stderr, "")
		return
	}

	// Update meta.json to archived state
	err = s.store.UpdateIntegrationWorktreeMeta(repoID, record.WorktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.State = store.WorktreeStateArchived
	})
	if err != nil {
		s.failWorktreeRm(w, http.StatusInternalServerError, requestID, repoID, record.WorktreeID, string(errors.EWorktreeRemoveFailed),
			"failed to archive worktree metadata after git remove: "+err.Error(),
			"inspect worktree metadata before retrying")
		return
	}

	s.finishWorktreeRm(w, requestID, repoID, record.WorktreeID, false)
}

// failWorktreeRm emits worktree_rm_failed then writes the http error.
// Use only after worktree_rm_started has been emitted.
func (s *Server) failWorktreeRm(w http.ResponseWriter, status int, requestID, repoID, worktreeID, code, message, hint string) {
	if emitErr := s.appendWorktreeEvent(repoID, worktreeID, worktreeRmEventFailed, map[string]any{
		"error_code": code,
		"message":    message,
	}); emitErr != nil {
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EPersistFailed),
			"failed to append worktree_rm_failed event: "+emitErr.Error(), "")
		return
	}
	s.writeErrorWithRequestID(w, status, requestID, code, message, hint)
}

// finishWorktreeRm emits worktree_rm_succeeded then writes the success response.
// Use only after worktree_rm_started has been emitted.
func (s *Server) finishWorktreeRm(w http.ResponseWriter, requestID, repoID, worktreeID string, treeMissing bool) {
	if err := s.appendWorktreeEvent(repoID, worktreeID, worktreeRmEventSucceeded, map[string]any{
		"tree_missing": treeMissing,
	}); err != nil {
		s.writeErrorWithRequestID(w, http.StatusInternalServerError, requestID, string(errors.EPersistFailed),
			"failed to append worktree_rm_succeeded event: "+err.Error(), "")
		return
	}
	s.writeWorktreeRmSuccess(w, requestID)
}

// ensureRepoRecord writes/updates repo.json for the repo.
func (s *Server) ensureRepoRecord(repoIdentity identity.RepoIdentity, repoRoot string, originInfo git.OriginInfo) error {
	// Check if repo record exists
	existing, exists, err := s.store.LoadRepoRecord(repoIdentity.RepoID)
	if err != nil {
		return err
	}

	// Create repo directory if needed
	repoDir := s.store.RepoDir(repoIdentity.RepoID)
	if err := s.fsys.MkdirAll(repoDir, 0o700); err != nil {
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
		rec = s.store.UpsertRepoRecord(&existing, input)
	} else {
		rec = s.store.UpsertRepoRecord(nil, input)
	}

	return s.store.SaveRepoRecord(rec)
}
