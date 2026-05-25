package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/invocation"
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
	if _, fail := validateControlPlaneStartCommon(&req, true); fail != nil {
		writeErr(fail.status, string(fail.code), fail.msg, fail.hint, req.ClientRequestID)
		return
	}

	repoRoot, repoIdentity, ok := s.resolveControlPlaneRepoRoot(ctx, req.RepoRoot, func(status int, code, message, hint string) {
		writeErr(status, code, message, hint, req.ClientRequestID)
	})
	if !ok {
		return
	}

	requestEnv := req.Env
	prior, fail := s.findReusablePriorInvocation(repoIdentity.RepoID, repoRoot, "headless", store.RunnerModeHeadless, req, requestEnv)
	if fail != nil {
		writeErr(fail.status, string(fail.code), fail.msg, fail.hint, req.ClientRequestID)
		return
	}
	if prior != nil {
		s.recordIdempotency(idempotencyScopeHeadlessStart, repoIdentity.RepoID, req.ClientRequestID, prior.InvocationID, prior.Meta.RequestFingerprint)
		s.writeControlPlaneSuccess(w, prior.InvocationID, prior.Meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
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
	cachedEntry, cachedMeta, fail := s.findReusableCachedInvocation(repoIdentity.RepoID, idempotencyScopeHeadlessStart, "headless", req.ClientRequestID, fingerprint)
	if fail != nil {
		writeErr(fail.status, string(fail.code), fail.msg, fail.hint, req.ClientRequestID)
		return
	}
	if cachedEntry != nil {
		s.writeControlPlaneSuccess(w, cachedEntry.invocationID, cachedMeta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
		return
	}
	stored, fail := s.findReusableStoredInvocation(repoIdentity.RepoID, "headless", req.ClientRequestID, fingerprint)
	if fail != nil {
		writeErr(fail.status, string(fail.code), fail.msg, fail.hint, req.ClientRequestID)
		return
	}
	if stored != nil {
		s.recordIdempotency(idempotencyScopeHeadlessStart, repoIdentity.RepoID, req.ClientRequestID, stored.InvocationID, fingerprint)
		s.writeControlPlaneSuccess(w, stored.InvocationID, stored.Meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
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

	s.recordIdempotency(idempotencyScopeHeadlessStart, repoIdentity.RepoID, req.ClientRequestID, createResult.InvocationID, fingerprint)
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
