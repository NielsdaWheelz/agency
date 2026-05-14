package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"path/filepath"
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
	if err := decodeStrictJSON(r.Body, &req); err != nil {
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
	req.ExecutionProfile = strings.TrimSpace(req.ExecutionProfile)
	req.AgencyConfigPath = strings.TrimSpace(req.AgencyConfigPath)
	if req.AgencyConfigPath != "" && !filepath.IsAbs(req.AgencyConfigPath) {
		writeErr(http.StatusBadRequest, string(errors.EInvalidArgument), "agency_config_path must be absolute", "", req.ClientRequestID)
		return
	}
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

	requestEnv := req.Env
	execCtx, err := s.resolveExecutionContext(repoRoot, repoIdentity.RepoID, req.AgencyConfigPath, req.ExecutionProfile)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		writeErr(http.StatusBadRequest, string(code), apiErrorMessage(err), "", req.ClientRequestID)
		return
	}
	req.ExecutionProfile = execCtx.Profile
	gitEnv := prSyncNonInteractiveEnv(execCtx.ProfileEnv)
	req.Env = envForLaunch(execCtx.ProfileEnv, requestEnv)

	prep, ok := s.prepareControlPlaneStart(ctx, repoRoot, req.WorktreeRef, "control_plane_start_headless", func(status int, code, message, hint string) {
		writeErr(status, code, message, hint, req.ClientRequestID)
	}, repoIdentity)
	if !ok {
		return
	}
	defer func() { _ = prep.unlockRepo() }()

	fingerprint := controlPlaneStartFingerprint(repoRoot, prep.wtRecord.WorktreeID, execCtx.CheckoutRoot, store.RunnerModeHeadless, req, requestEnv)
	if entry, isDuplicate, conflict := s.checkIdempotency(repoIdentity.RepoID, req.ClientRequestID, fingerprint); isDuplicate {
		if conflict {
			writeErr(http.StatusConflict, string(errors.EIdempotencyConflict), "client_request_id was already used for a different headless invocation start request", "retry with the original request or choose a new client_request_id", req.ClientRequestID)
			return
		}
		meta, err := s.Store.ReadInvocationMeta(repoIdentity.RepoID, entry.InvocationID)
		if err == nil {
			s.writeControlPlaneSuccess(w, entry.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
			return
		}
		writeErr(http.StatusConflict, string(errors.EStoreCorrupt), "client_request_id was already accepted but invocation metadata is unreadable: "+err.Error(), "inspect invocation state before retrying", req.ClientRequestID)
		return
	}
	if record, exists, conflict, err := s.findInvocationByClientRequestID(repoIdentity.RepoID, req.ClientRequestID, fingerprint); err != nil {
		writeErr(http.StatusInternalServerError, string(errors.EInternal), "failed to scan invocations for idempotency: "+err.Error(), "", req.ClientRequestID)
		return
	} else if exists {
		if conflict {
			writeErr(http.StatusConflict, string(errors.EIdempotencyConflict), "client_request_id was already used for a different headless invocation start request", "retry with the original request or choose a new client_request_id", req.ClientRequestID)
			return
		}
		if record.Meta == nil || record.Broken {
			writeErr(http.StatusConflict, string(errors.EStoreCorrupt), "client_request_id record exists but invocation metadata is unreadable", "inspect invocation state before retrying", req.ClientRequestID)
			return
		}
		s.recordIdempotency(repoIdentity.RepoID, req.ClientRequestID, record.InvocationID, fingerprint)
		s.writeControlPlaneSuccess(w, record.InvocationID, record.Meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
		return
	}

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
		CheckoutRoot:            execCtx.CheckoutRoot,
		ExecutionProfile:        execCtx.Profile,
		NoIncludeUntracked:      req.NoIncludeUntracked,
		ClientRequestID:         req.ClientRequestID,
		RequestFingerprint:      fingerprint,
		Env:                     gitEnv,
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
		s.cleanupFailedInvocation(ctx, repoIdentity.RepoID, createResult, repoRoot, "start_failed", gitEnv)
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to create logs directory: "+err.Error(), "", req.ClientRequestID)
		return
	}

	promptPath := s.Store.InvocationPromptPath(repoIdentity.RepoID, createResult.InvocationID)
	if err := s.FS.WriteFile(promptPath, []byte(req.Prompt), 0o600); err != nil {
		s.cleanupFailedInvocation(ctx, repoIdentity.RepoID, createResult, repoRoot, "start_failed", gitEnv)
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to write prompt file: "+err.Error(), "", req.ClientRequestID)
		return
	}

	pid, pgid, err := s.startRunner(ctx, repoIdentity.RepoID, createResult, repoRoot, prep.wtRecord.WorktreeID, req, gitEnv)
	if err != nil {
		s.cleanupFailedInvocation(ctx, repoIdentity.RepoID, createResult, repoRoot, "spawn_failed", gitEnv)
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerStartFailed
		}
		writeErr(http.StatusInternalServerError, string(code), err.Error(), "", req.ClientRequestID)
		return
	}

	s.recordIdempotency(repoIdentity.RepoID, req.ClientRequestID, createResult.InvocationID, fingerprint)

	promptHash := sha256.Sum256([]byte(req.Prompt))
	promptSHA := hex.EncodeToString(promptHash[:])
	envKeys := sortedEnvKeys(requestEnv)
	runnerArgs := append([]string(nil), req.RunnerArgs...)

	if err := s.claimHeadlessInvocationStart(
		repoIdentity.RepoID,
		createResult.InvocationID,
		req.Runner,
		pid,
		pgid,
		s.Store.InvocationPromptPath(repoIdentity.RepoID, createResult.InvocationID),
		promptSHA,
		runnerArgs,
		envKeys,
	); err != nil {
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to update invocation meta: "+err.Error(), "", req.ClientRequestID)
		return
	}

	meta, err := s.Store.ReadInvocationMeta(repoIdentity.RepoID, createResult.InvocationID)
	if err != nil {
		writeErr(http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "", req.ClientRequestID)
		return
	}
	s.writeControlPlaneSuccess(w, createResult.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, false)
}
