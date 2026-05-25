package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
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
		writeErr(http.StatusBadRequest, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "", "")
		return
	}
	if req.ClientRequestID == "" {
		writeErr(http.StatusBadRequest, string(errors.EInvalidRequest), "client_request_id is required", "provide a UUID for idempotency", "")
		return
	}
	if req.RepoRoot == "" {
		writeErr(http.StatusBadRequest, string(errors.EInvalidRequest), "repo_root is required", "", req.ClientRequestID)
		return
	}
	if req.WorktreeRef == "" {
		writeErr(http.StatusBadRequest, string(errors.EInvalidRequest), "worktree_ref is required", "", req.ClientRequestID)
		return
	}
	if req.Runner == "" {
		writeErr(http.StatusBadRequest, string(errors.EInvalidRequest), "runner is required", "", req.ClientRequestID)
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
		code := errors.CodeOr(err, errors.ERunnerArgConflict)
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
	if record, exists, err := s.findInvocationRecordByClientRequestID(repoIdentity.RepoID, req.ClientRequestID); err != nil {
		writeErr(http.StatusInternalServerError, string(errors.EInternal), "failed to scan invocations for idempotency: "+err.Error(), "", req.ClientRequestID)
		return
	} else if exists {
		if record.Meta == nil || record.Broken {
			writeErr(http.StatusConflict, string(errors.EStoreCorrupt), "client_request_id record exists but invocation metadata is unreadable", "inspect invocation state before retrying", req.ClientRequestID)
			return
		}
		if s.directStartRequestConflictsWithRecord(repoIdentity.RepoID, repoRoot, store.RunnerModeHeadless, req, requestEnv, record.Meta) {
			writeErr(http.StatusConflict, string(errors.EIdempotencyConflict), "client_request_id was already used for a different headless invocation start request", "retry with the original request or choose a new client_request_id", req.ClientRequestID)
			return
		}
		if fail := evaluateIdempotentStartRecord(record.Meta); fail != nil {
			writeErr(fail.status, string(fail.code), fail.msg, fail.hint, req.ClientRequestID)
			return
		}
		s.recordIdempotency(repoIdentity.RepoID, req.ClientRequestID, record.InvocationID, record.Meta.RequestFingerprint)
		s.writeControlPlaneSuccess(w, record.InvocationID, record.Meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
		return
	}

	execCtx, err := s.resolveExecutionContext(repoRoot, repoIdentity.RepoID, req.AgencyConfigPath, req.ExecutionProfile)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		writeErr(http.StatusBadRequest, string(code), apiErrorMessage(err), "", req.ClientRequestID)
		return
	}
	req.ExecutionProfile = execCtx.Profile
	gitEnv := withNonInteractiveEnv(execCtx.ProfileEnv)
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
		meta, err := s.store.ReadInvocationMeta(repoIdentity.RepoID, entry.invocationID)
		if err != nil {
			writeErr(http.StatusConflict, string(errors.EStoreCorrupt), "client_request_id was already accepted but invocation metadata is unreadable: "+err.Error(), "inspect invocation state before retrying", req.ClientRequestID)
			return
		}
		if fail := evaluateIdempotentStartRecord(meta); fail != nil {
			writeErr(fail.status, string(fail.code), fail.msg, fail.hint, req.ClientRequestID)
			return
		}
		s.writeControlPlaneSuccess(w, entry.invocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
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
		if fail := evaluateIdempotentStartRecord(record.Meta); fail != nil {
			writeErr(fail.status, string(fail.code), fail.msg, fail.hint, req.ClientRequestID)
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

	invSvc := invocation.NewService(s.store, s.runner, s.fsys, s.clock)
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
		code := errors.CodeOr(err, errors.EInternal)
		writeErr(http.StatusInternalServerError, string(code), err.Error(), "", req.ClientRequestID)
		return
	}

	meta, fail := s.finishHeadlessInvocationStart(ctx, repoRoot, repoIdentity.RepoID, "", prep.wtRecord, createResult, headlessInvocationStartParams{
		runner:             req.Runner,
		runnerArgs:         req.RunnerArgs,
		prompt:             req.Prompt,
		invocationName:     req.InvocationName,
		env:                req.Env,
		envKeys:            sortedEnvKeys(requestEnv),
		gitEnv:             gitEnv,
		noIncludeUntracked: req.NoIncludeUntracked,
		clientRequestID:    req.ClientRequestID,
	})
	if fail != nil {
		writeErr(fail.status, string(fail.code), fail.msg, fail.hint, req.ClientRequestID)
		return
	}

	s.recordIdempotency(repoIdentity.RepoID, req.ClientRequestID, createResult.InvocationID, fingerprint)
	s.writeControlPlaneSuccess(w, createResult.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, false)
}

type headlessInvocationStartParams struct {
	runner             string
	runnerArgs         []string
	prompt             string
	invocationName     string
	env                map[string]string
	envKeys            []string
	gitEnv             map[string]string
	noIncludeUntracked bool
	clientRequestID    string
}

// finishHeadlessInvocationStart performs the post-Create steps required to
// bring a headless invocation under supervision: prepare logs and prompt,
// launch the runner via startRunner, and claim it on success. On any failure
// the invocation is cleaned up and the failure is returned for the caller to
// render.
func (s *Server) finishHeadlessInvocationStart(ctx context.Context, repoRoot, repoID, taskID string, wtRecord *store.IntegrationWorktreeRecord, createResult *invocation.CreateResult, params headlessInvocationStartParams) (*store.InvocationMeta, *startFailure) {
	cleanup := func(reason string) {
		s.cleanupFailedInvocation(ctx, repoID, createResult, repoRoot, reason, params.gitEnv)
	}

	logsDir := s.store.InvocationLogsDir(repoID, createResult.InvocationID)
	if err := s.fsys.MkdirAll(logsDir, 0o700); err != nil {
		cleanup("start_failed")
		f := newStartFailure(http.StatusInternalServerError, errors.EInternal, "failed to create logs directory: "+err.Error(), "")
		return nil, &f
	}
	promptPath := s.store.InvocationPromptPath(repoID, createResult.InvocationID)
	if err := s.fsys.WriteFile(promptPath, []byte(params.prompt), 0o600); err != nil {
		cleanup("start_failed")
		f := newStartFailure(http.StatusInternalServerError, errors.EInternal, "failed to write prompt file: "+err.Error(), "")
		return nil, &f
	}

	promptHash := sha256.Sum256([]byte(params.prompt))
	promptSHA := hex.EncodeToString(promptHash[:])
	runnerArgs := slices.Clone(params.runnerArgs)
	startReq := ControlPlaneStartRequest{
		RepoRoot:           repoRoot,
		WorktreeRef:        wtRecord.WorktreeID,
		Runner:             params.runner,
		Prompt:             params.prompt,
		InvocationName:     params.invocationName,
		RunnerArgs:         params.runnerArgs,
		Env:                params.env,
		ClientRequestID:    params.clientRequestID,
		NoIncludeUntracked: params.noIncludeUntracked,
	}
	claim := func(pid, pgid int) error {
		return s.claimHeadlessInvocationStart(repoID, createResult.InvocationID, taskID, params.runner, pid, pgid, promptPath, promptSHA, runnerArgs, params.envKeys)
	}
	if _, _, err := s.startRunner(ctx, repoID, createResult, repoRoot, wtRecord.WorktreeID, startReq, params.gitEnv, claim); err != nil {
		cleanup("spawn_failed")
		f := newStartFailure(http.StatusInternalServerError, errors.CodeOr(err, errors.ERunnerStartFailed), err.Error(), "")
		return nil, &f
	}

	meta, err := s.store.ReadInvocationMeta(repoID, createResult.InvocationID)
	if err != nil {
		f := startFailureFromError(http.StatusInternalServerError, errors.EInvocationBroken, err, "")
		return nil, &f
	}
	return meta, nil
}
