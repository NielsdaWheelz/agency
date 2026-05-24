package daemon

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Server) handleControlPlaneStartHeaded(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := prepareRequestID(w, r)

	var req ControlPlaneStartRequest
	if err := decodeStrictJSON(r.Body, &req); err != nil {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "", "", requestID)
		return
	}
	if req.ClientRequestID == "" {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvalidRequest), "client_request_id is required", "provide a UUID for idempotency", "", requestID)
		return
	}
	if req.RepoRoot == "" {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvalidRequest), "repo_root is required", "", req.ClientRequestID, requestID)
		return
	}
	if req.WorktreeRef == "" {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvalidRequest), "worktree_ref is required", "", req.ClientRequestID, requestID)
		return
	}
	if req.Runner == "" {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvalidRequest), "runner is required", "", req.ClientRequestID, requestID)
		return
	}

	canonicalRunner, err := validateControlPlaneStartRunner(req.Runner, req.RunnerArgs, false)
	if err != nil {
		code := errors.CodeOr(err, errors.ERunnerArgConflict)
		hint := "remove reserved flags from runner_args"
		if code == errors.ERunnerNotFound {
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		}
		s.writeHeadedError(w, http.StatusBadRequest, string(code), err.Error(), hint, req.ClientRequestID, requestID)
		return
	}
	req.Runner = canonicalRunner
	req.ExecutionProfile = strings.TrimSpace(req.ExecutionProfile)
	req.AgencyConfigPath = strings.TrimSpace(req.AgencyConfigPath)
	if req.AgencyConfigPath != "" && !filepath.IsAbs(req.AgencyConfigPath) {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvalidArgument), "agency_config_path must be absolute", "", req.ClientRequestID, requestID)
		return
	}

	headedRunnerArgs, err := buildRunnerArgsForHeaded(req.Runner, req.RunnerArgs)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		hint := ""
		switch code {
		case errors.ERunnerNotFound:
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		case errors.EInvocationInvalidMode:
			hint = "runner does not support headed mode"
		}
		status := http.StatusInternalServerError
		if code == errors.ERunnerNotFound || code == errors.EInvocationInvalidMode {
			status = http.StatusBadRequest
		}
		s.writeHeadedError(w, status, string(code), err.Error(), hint, req.ClientRequestID, requestID)
		return
	}

	if req.InvocationName != "" {
		if err := validateControlPlaneStartInvocationName(req.InvocationName); err != nil {
			s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvalidName), "invalid invocation name: "+err.Error(), "names must be 2-40 chars, lowercase alphanumeric + hyphens", req.ClientRequestID, requestID)
			return
		}
	}

	repoRoot, repoIdentity, ok := s.resolveControlPlaneRepoRoot(ctx, req.RepoRoot, func(status int, code, message, hint string) {
		s.writeHeadedError(w, status, code, message, hint, req.ClientRequestID, requestID)
	})
	if !ok {
		return
	}

	requestEnv := req.Env
	if record, exists, err := s.findInvocationRecordByClientRequestID(repoIdentity.RepoID, req.ClientRequestID); err != nil {
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInternal), "failed to scan invocations for idempotency: "+err.Error(), "", req.ClientRequestID, requestID)
		return
	} else if exists {
		if record.Meta == nil || record.Broken {
			s.writeHeadedError(w, http.StatusConflict, string(errors.EStoreCorrupt), "client_request_id record exists but invocation metadata is unreadable", "inspect invocation state before retrying", req.ClientRequestID, requestID)
			return
		}
		if s.directStartRequestConflictsWithRecord(repoIdentity.RepoID, repoRoot, store.RunnerModeHeaded, req, requestEnv, record.Meta) {
			s.writeHeadedError(w, http.StatusConflict, string(errors.EIdempotencyConflict), "client_request_id was already used for a different headed invocation start request", "retry with the original request or choose a new client_request_id", req.ClientRequestID, requestID)
			return
		}
		switch record.Meta.Status {
		case store.InvocationStatusRunning, store.InvocationStatusStopping, store.InvocationStatusFinished:
		case store.InvocationStatusStarting:
			s.writeHeadedError(w, http.StatusConflict, string(errors.EInvocationStartFailed), "client_request_id was already accepted but invocation start has not reached running state", "inspect invocation state before retrying", req.ClientRequestID, requestID)
			return
		case store.InvocationStatusFailed:
			if directStartFailedBeforeClaim(record.Meta) {
				message := strings.TrimSpace(record.Meta.FailureReason)
				if message == "" {
					message = "invocation start previously failed"
				}
				s.writeHeadedError(w, http.StatusConflict, string(errors.EInvocationStartFailed), message, "inspect invocation state before retrying", req.ClientRequestID, requestID)
				return
			}
		default:
			s.writeHeadedError(w, http.StatusConflict, string(errors.EStoreCorrupt), "client_request_id record has unsupported invocation status", "inspect invocation state before retrying", req.ClientRequestID, requestID)
			return
		}
		s.recordHeadedIdempotency(repoIdentity.RepoID, req.ClientRequestID, record.InvocationID, record.Meta.RequestFingerprint)
		s.writeHeadedSuccess(w, record.InvocationID, record.Meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
		return
	}

	execCtx, err := s.resolveExecutionContext(repoRoot, repoIdentity.RepoID, req.AgencyConfigPath, req.ExecutionProfile)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		s.writeHeadedError(w, http.StatusBadRequest, string(code), apiErrorMessage(err), "", req.ClientRequestID, requestID)
		return
	}
	req.ExecutionProfile = execCtx.Profile
	gitEnv := prSyncNonInteractiveEnv(execCtx.ProfileEnv)
	req.Env = envForLaunch(execCtx.ProfileEnv, requestEnv)

	prep, ok := s.prepareControlPlaneStart(ctx, repoRoot, req.WorktreeRef, "control_plane_start_headed", func(status int, code, message, hint string) {
		s.writeHeadedError(w, status, code, message, hint, req.ClientRequestID, requestID)
	}, repoIdentity)
	if !ok {
		return
	}
	defer func() { _ = prep.unlockRepo() }()

	fingerprint := controlPlaneStartFingerprint(repoRoot, prep.wtRecord.WorktreeID, execCtx.CheckoutRoot, store.RunnerModeHeaded, req, requestEnv)
	if entry, isDuplicate, conflict := s.checkHeadedIdempotency(repoIdentity.RepoID, req.ClientRequestID, fingerprint); isDuplicate {
		if conflict {
			s.writeHeadedError(w, http.StatusConflict, string(errors.EIdempotencyConflict), "client_request_id was already used for a different headed invocation start request", "retry with the original request or choose a new client_request_id", req.ClientRequestID, requestID)
			return
		}
		meta, err := s.store.ReadInvocationMeta(repoIdentity.RepoID, entry.invocationID)
		if err == nil {
			if meta.Status == store.InvocationStatusStarting {
				s.writeHeadedError(w, http.StatusConflict, string(errors.EInvocationStartFailed), "client_request_id was already accepted but invocation start has not reached running state", "inspect invocation state before retrying", req.ClientRequestID, requestID)
				return
			}
			if meta.Status == store.InvocationStatusFailed && directStartFailedBeforeClaim(meta) {
				message := strings.TrimSpace(meta.FailureReason)
				if message == "" {
					message = "invocation start previously failed"
				}
				s.writeHeadedError(w, http.StatusConflict, string(errors.EInvocationStartFailed), message, "inspect invocation state before retrying", req.ClientRequestID, requestID)
				return
			}
			s.writeHeadedSuccess(w, entry.invocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
			return
		}
		s.writeHeadedError(w, http.StatusConflict, string(errors.EStoreCorrupt), "client_request_id was already accepted but invocation metadata is unreadable: "+err.Error(), "inspect invocation state before retrying", req.ClientRequestID, requestID)
		return
	}
	if record, exists, conflict, err := s.findInvocationByClientRequestID(repoIdentity.RepoID, req.ClientRequestID, fingerprint); err != nil {
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInternal), "failed to scan invocations for idempotency: "+err.Error(), "", req.ClientRequestID, requestID)
		return
	} else if exists {
		if conflict {
			s.writeHeadedError(w, http.StatusConflict, string(errors.EIdempotencyConflict), "client_request_id was already used for a different headed invocation start request", "retry with the original request or choose a new client_request_id", req.ClientRequestID, requestID)
			return
		}
		if record.Meta == nil || record.Broken {
			s.writeHeadedError(w, http.StatusConflict, string(errors.EStoreCorrupt), "client_request_id record exists but invocation metadata is unreadable", "inspect invocation state before retrying", req.ClientRequestID, requestID)
			return
		}
		switch record.Meta.Status {
		case store.InvocationStatusRunning, store.InvocationStatusStopping, store.InvocationStatusFinished:
		case store.InvocationStatusStarting:
			s.writeHeadedError(w, http.StatusConflict, string(errors.EInvocationStartFailed), "client_request_id was already accepted but invocation start has not reached running state", "inspect invocation state before retrying", req.ClientRequestID, requestID)
			return
		case store.InvocationStatusFailed:
			if directStartFailedBeforeClaim(record.Meta) {
				message := strings.TrimSpace(record.Meta.FailureReason)
				if message == "" {
					message = "invocation start previously failed"
				}
				s.writeHeadedError(w, http.StatusConflict, string(errors.EInvocationStartFailed), message, "inspect invocation state before retrying", req.ClientRequestID, requestID)
				return
			}
		default:
			s.writeHeadedError(w, http.StatusConflict, string(errors.EStoreCorrupt), "client_request_id record has unsupported invocation status", "inspect invocation state before retrying", req.ClientRequestID, requestID)
			return
		}
		s.recordHeadedIdempotency(repoIdentity.RepoID, req.ClientRequestID, record.InvocationID, fingerprint)
		s.writeHeadedSuccess(w, record.InvocationID, record.Meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
		return
	}

	if req.InvocationName != "" {
		if err := s.checkInvocationNameUniqueness(repoIdentity.RepoID, req.InvocationName); err != nil {
			s.writeHeadedError(w, http.StatusConflict, string(errors.EInvocationNameExists), err.Error(), "use a different name or wait for the existing invocation to complete", req.ClientRequestID, requestID)
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
		Mode:                    store.RunnerModeHeaded,
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
		s.writeHeadedError(w, http.StatusInternalServerError, string(code), err.Error(), "", req.ClientRequestID, requestID)
		return
	}

	meta, fail := s.finishHeadedInvocationStart(ctx, repoRoot, repoIdentity.RepoID, "", prep.wtRecord, createResult, headedInvocationStartParams{
		runner:             req.Runner,
		runnerArgs:         slices.Clone(req.RunnerArgs),
		headedRunnerArgs:   headedRunnerArgs,
		launchEnv:          req.Env,
		envKeys:            sortedEnvKeys(requestEnv),
		gitEnv:             gitEnv,
		noIncludeUntracked: req.NoIncludeUntracked,
	})
	if fail != nil {
		s.writeHeadedError(w, fail.status, string(fail.code), fail.msg, fail.hint, req.ClientRequestID, requestID)
		return
	}

	s.recordHeadedIdempotency(repoIdentity.RepoID, req.ClientRequestID, createResult.InvocationID, fingerprint)
	s.writeHeadedSuccess(w, createResult.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, false)
}

type headedInvocationStartParams struct {
	runner             string
	runnerArgs         []string
	headedRunnerArgs   []string
	launchEnv          map[string]string
	envKeys            []string
	gitEnv             map[string]string
	noIncludeUntracked bool
}

// finishHeadedInvocationStart performs the post-Create steps required to bring
// a headed invocation under supervision: resolve the runner binary, install
// hooks, create and capture the tmux session, claim the invocation, attach the
// checkpoint engine and stream parser, and launch the output/checkpoint
// goroutines. On any failure the invocation is marked start_failed and the
// failure is returned for the caller to render.
func (s *Server) finishHeadedInvocationStart(ctx context.Context, repoRoot, repoID, taskID string, wtRecord *store.IntegrationWorktreeRecord, createResult *invocation.CreateResult, params headedInvocationStartParams) (*store.InvocationMeta, *startFailure) {
	failStart := func(status int, code errors.Code, msg, hint string) *startFailure {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		f := newStartFailure(status, code, msg, hint)
		return &f
	}

	userCfg, err := s.LoadUserConfig()
	if err != nil {
		return nil, failStart(http.StatusInternalServerError, errors.EInvalidUserConfig, "failed to load user config: "+err.Error(), "run `agency config init`")
	}
	runnerCmd, err := config.ResolveRunnerCmd(s.runner, s.fsys, s.configDir, userCfg, params.runner)
	if err != nil {
		return nil, failStart(http.StatusInternalServerError, errors.ERunnerNotFound, "failed to resolve runner command: "+err.Error(), "ensure runner is installed and configured")
	}
	if err := s.installHeadedRunnerHooks(ctx, repoID, createResult.InvocationID, params.runner, params.headedRunnerArgs, createResult.SandboxPath, params.gitEnv); err != nil {
		return nil, failStart(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to install headed runner hooks: "+err.Error(), "ensure sandbox hook files can be written")
	}

	sessionName := createResult.TmuxSession
	if exists, err := s.tmuxClient.HasSession(ctx, sessionName); err != nil {
		s.recordInvocationWarning(repoID, createResult.InvocationID, "start_headed_tmux_has_session_failed", err.Error(), map[string]any{
			"session_name": sessionName,
		})
	} else if exists {
		return nil, failStart(http.StatusConflict, errors.ETmuxSessionExists, "tmux session already exists: "+sessionName, "a tmux session with this name already exists; kill it with 'tmux kill-session -t "+sessionName+"'")
	}

	terminalLogPath, err := s.prepareWritableInvocationLogPath(repoID, createResult.InvocationID, InvocationLogKindTerminal)
	if err != nil {
		return nil, failStart(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to prepare terminal log: "+err.Error(), "")
	}
	terminalFile, err := os.OpenFile(terminalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, failStart(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to create terminal log: "+err.Error(), "")
	}
	_ = terminalFile.Close()

	argv := append([]string{runnerCmd}, params.headedRunnerArgs...)
	if err := s.tmuxClient.NewSession(ctx, sessionName, createResult.SandboxPath, argv, params.launchEnv); err != nil {
		return nil, failStart(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to create tmux session: "+err.Error(), "ensure tmux is installed and working")
	}
	target := sessionName + ":0.0"
	if scrollback, err := s.tmuxClient.CaptureScrollback(ctx, target); err != nil {
		s.recordInvocationWarning(repoID, createResult.InvocationID, "start_headed_tmux_capture_failed", err.Error(), map[string]any{
			"target": target,
		})
	} else if scrollback != "" {
		if err := os.WriteFile(terminalLogPath, []byte(scrollback), 0o600); err != nil {
			_ = s.tmuxClient.KillSession(ctx, sessionName)
			return nil, failStart(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to write initial terminal capture: "+err.Error(), "")
		}
	}
	if err := s.tmuxClient.PipePane(ctx, target, terminalLogPath); err != nil {
		_ = s.tmuxClient.KillSession(ctx, sessionName)
		return nil, failStart(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to pipe tmux pane output: "+err.Error(), "ensure tmux pipe-pane is available")
	}

	if _, err := s.claimHeadedInvocation(repoID, createResult.InvocationID, taskID, params.runner, sessionName, params.runnerArgs, params.envKeys); err != nil {
		_ = s.tmuxClient.KillSession(ctx, sessionName)
		f := newStartFailure(http.StatusInternalServerError, errors.EInternal, "failed to update invocation meta: "+err.Error(), "")
		return nil, &f
	}

	proc := s.buildSupervisedHeadedProcess(ctx, supervisedHeadedSetup{
		invocationID:          createResult.InvocationID,
		repoID:                repoID,
		integrationWorktreeID: wtRecord.WorktreeID,
		repoRoot:              repoRoot,
		sandboxPath:           createResult.SandboxPath,
		sessionName:           sessionName,
		runner:                params.runner,
		runnerArgs:            params.runnerArgs,
		launchEnv:             params.launchEnv,
		includeUntracked:      !params.noIncludeUntracked,
		gitEnv:                params.gitEnv,
	})
	s.launchSupervisedHeadedProcess(proc)

	meta, err := s.store.ReadInvocationMeta(repoID, createResult.InvocationID)
	if err != nil {
		f := startFailureFromError(http.StatusInternalServerError, errors.EInvocationBroken, err, "")
		return nil, &f
	}
	return meta, nil
}
