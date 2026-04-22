package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Server) handleControlPlaneStartHeadless(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := prepareRequestID(w, r)
	writeErr := func(status int, code, message, hint, clientRequestID string) {
		s.writeControlPlaneError(w, status, requestID, code, message, hint, clientRequestID)
	}

	var req ControlPlaneStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "", "")
		return
	}
	if req.ClientRequestID == "" {
		writeErr(http.StatusBadRequest, "E_INVALID_REQUEST", "client_request_id is required", "provide a UUID for idempotency", "")
		return
	}
	if req.RepoRoot == "" {
		writeErr(http.StatusBadRequest, "E_INVALID_REQUEST", "repo_root is required", "", req.ClientRequestID)
		return
	}
	if req.WorktreeRef == "" {
		writeErr(http.StatusBadRequest, "E_INVALID_REQUEST", "worktree_ref is required", "", req.ClientRequestID)
		return
	}
	if req.Runner == "" {
		writeErr(http.StatusBadRequest, "E_INVALID_REQUEST", "runner is required", "", req.ClientRequestID)
		return
	}
	if req.Prompt == "" {
		writeErr(http.StatusBadRequest, string(errors.EPromptRequired), "prompt is required for headless invocation", "", req.ClientRequestID)
		return
	}
	if len(req.Prompt) > MaxPromptSize {
		writeErr(http.StatusBadRequest, string(errors.EPromptTooLarge), fmt.Sprintf("prompt exceeds maximum size of %d bytes (got %d)", MaxPromptSize, len(req.Prompt)), "reduce prompt size or split into smaller chunks", req.ClientRequestID)
		return
	}

	canonicalRunner, err := validateControlPlaneStartRunner(req.Runner, req.RunnerArgs, true)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerArgConflict
		}
		hint := "remove reserved flags from runner_args"
		if code == errors.ERunnerNotFound {
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		}
		writeErr(http.StatusBadRequest, string(code), err.Error(), hint, req.ClientRequestID)
		return
	}
	req.Runner = canonicalRunner
	if err := validateControlPlaneStartInvocationName(req.InvocationName); err != nil {
		writeErr(http.StatusBadRequest, string(errors.EInvalidName), "invalid invocation name: "+err.Error(), "names must be 2-40 chars, lowercase alphanumeric + hyphens", req.ClientRequestID)
		return
	}

	repoRoot, repoIdentity, ok := s.resolveControlPlaneRepoRoot(ctx, req.RepoRoot, func(status int, code, message, hint string) {
		writeErr(status, code, message, hint, req.ClientRequestID)
	})
	if !ok {
		return
	}

	if existingID, isDuplicate := s.checkIdempotency(repoIdentity.RepoID, req.ClientRequestID); isDuplicate {
		meta, err := s.Store.ReadInvocationMeta(repoIdentity.RepoID, existingID)
		if err == nil {
			s.writeControlPlaneSuccess(w, existingID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
			return
		}
	}

	prep, ok := s.prepareControlPlaneStart(ctx, repoRoot, req.WorktreeRef, "control_plane_start_headless", func(status int, code, message, hint string) {
		writeErr(status, code, message, hint, req.ClientRequestID)
	}, repoIdentity)
	if !ok {
		return
	}
	defer func() { _ = prep.unlockRepo() }()

	if req.InvocationName != "" {
		if err := s.checkInvocationNameUniqueness(repoIdentity.RepoID, req.InvocationName); err != nil {
			writeErr(http.StatusConflict, string(errors.EInvocationNameExists), err.Error(), "use a different name or wait for the existing invocation to complete", req.ClientRequestID)
			return
		}
	}

	invSvc := invocation.NewService(s.Store, s.Runner, s.FS, s.Clock)
	createResult, err := invSvc.Create(ctx, invocation.CreateOpts{
		IntegrationWorktreeID:   prep.wtRecord.WorktreeID,
		IntegrationWorktreeMeta: prep.wtRecord.Meta,
		RepoRoot:                repoRoot,
		RepoID:                  repoIdentity.RepoID,
		Runner:                  req.Runner,
		Mode:                    store.RunnerModeHeadless,
		InvocationName:          req.InvocationName,
		NoIncludeUntracked:      req.NoIncludeUntracked,
	})
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		writeErr(http.StatusInternalServerError, string(code), err.Error(), "", req.ClientRequestID)
		return
	}

	logsDir := s.Store.InvocationLogsDir(repoIdentity.RepoID, createResult.InvocationID)
	if err := s.FS.MkdirAll(logsDir, 0o700); err != nil {
		s.cleanupFailedInvocation(ctx, repoIdentity.RepoID, createResult, repoRoot, "start_failed")
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to create logs directory: "+err.Error(), "", req.ClientRequestID)
		return
	}

	promptPath := s.Store.InvocationPromptPath(repoIdentity.RepoID, createResult.InvocationID)
	if err := s.FS.WriteFile(promptPath, []byte(req.Prompt), 0o600); err != nil {
		s.cleanupFailedInvocation(ctx, repoIdentity.RepoID, createResult, repoRoot, "start_failed")
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to write prompt file: "+err.Error(), "", req.ClientRequestID)
		return
	}

	pid, pgid, err := s.startRunner(ctx, repoIdentity.RepoID, createResult, repoRoot, prep.wtRecord.WorktreeID, req)
	if err != nil {
		s.cleanupFailedInvocation(ctx, repoIdentity.RepoID, createResult, repoRoot, "spawn_failed")
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerStartFailed
		}
		writeErr(http.StatusInternalServerError, string(code), err.Error(), "", req.ClientRequestID)
		return
	}

	s.recordIdempotency(repoIdentity.RepoID, req.ClientRequestID, createResult.InvocationID)

	promptHash := sha256.Sum256([]byte(req.Prompt))
	promptSHA := hex.EncodeToString(promptHash[:])
	envKeys := sortedEnvKeys(req.Env)
	runnerArgs := append([]string(nil), req.RunnerArgs...)

	s.claimHeadlessInvocationStart(
		repoIdentity.RepoID,
		createResult.InvocationID,
		req.Runner,
		pid,
		pgid,
		s.Store.InvocationPromptPath(repoIdentity.RepoID, createResult.InvocationID),
		promptSHA,
		runnerArgs,
		envKeys,
	)

	meta, _ := s.Store.ReadInvocationMeta(repoIdentity.RepoID, createResult.InvocationID)
	s.writeControlPlaneSuccess(w, createResult.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, false)
}
