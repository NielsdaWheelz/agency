package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

func (s *Server) handleControlPlaneStartHeaded(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := prepareRequestID(w, r)

	var req ControlPlaneStartHeadedRequest
	if err := decodeStrictJSON(r.Body, &req); err != nil {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvalidRequest), "invalid request body: "+err.Error(), "", "", requestID)
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
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerArgConflict
		}
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
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
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
		s.recordHeadedIdempotency(repoIdentity.RepoID, req.ClientRequestID, record.InvocationID, record.Meta.RequestFingerprint, record.Meta.TmuxSession, record.Meta.SandboxPath)
		s.writeHeadedSuccess(w, record.InvocationID, record.Meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
		return
	}

	execCtx, err := s.resolveExecutionContext(repoRoot, repoIdentity.RepoID, req.AgencyConfigPath, req.ExecutionProfile)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
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
		meta, err := s.Store.ReadInvocationMeta(repoIdentity.RepoID, entry.InvocationID)
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
			s.writeHeadedSuccess(w, entry.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
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
		s.recordHeadedIdempotency(repoIdentity.RepoID, req.ClientRequestID, record.InvocationID, fingerprint, record.Meta.TmuxSession, record.Meta.SandboxPath)
		s.writeHeadedSuccess(w, record.InvocationID, record.Meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
		return
	}

	if req.InvocationName != "" {
		if err := s.checkInvocationNameUniqueness(repoIdentity.RepoID, req.InvocationName); err != nil {
			s.writeHeadedError(w, http.StatusConflict, string(errors.EInvocationNameExists), err.Error(), "use a different name or wait for the existing invocation to complete", req.ClientRequestID, requestID)
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
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		s.writeHeadedError(w, http.StatusInternalServerError, string(code), err.Error(), "", req.ClientRequestID, requestID)
		return
	}

	userCfg, err := s.LoadUserConfig()
	if err != nil {
		s.failInvocationStart(repoIdentity.RepoID, createResult.InvocationID, "start_failed", true)
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvalidUserConfig), "failed to load user config: "+err.Error(), "run `agency config init`", req.ClientRequestID, requestID)
		return
	}
	runnerCmd, err := config.ResolveRunnerCmd(s.Runner, s.FS, s.ConfigDir, userCfg, req.Runner)
	if err != nil {
		s.failInvocationStart(repoIdentity.RepoID, createResult.InvocationID, "start_failed", true)
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.ERunnerNotFound), "failed to resolve runner command: "+err.Error(), "ensure runner is installed and configured", req.ClientRequestID, requestID)
		return
	}
	if err := s.installHeadedRunnerHooks(ctx, repoIdentity.RepoID, createResult.InvocationID, req.Runner, headedRunnerArgs, createResult.SandboxPath, gitEnv); err != nil {
		s.failInvocationStart(repoIdentity.RepoID, createResult.InvocationID, "start_failed", true)
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to install headed runner hooks: "+err.Error(), "ensure sandbox hook files can be written", req.ClientRequestID, requestID)
		return
	}

	sessionName := tmux.SessionName(createResult.InvocationID)
	exists, err := s.TmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		s.recordInvocationWarning(repoIdentity.RepoID, createResult.InvocationID, "start_headed_tmux_has_session_failed", err.Error(), map[string]any{
			"session_name": sessionName,
		})
	} else if exists {
		s.failInvocationStart(repoIdentity.RepoID, createResult.InvocationID, "start_failed", true)
		s.writeHeadedError(w, http.StatusConflict, string(errors.ETmuxSessionExists), "tmux session already exists: "+sessionName, "a tmux session with this name already exists; kill it with 'tmux kill-session -t "+sessionName+"'", req.ClientRequestID, requestID)
		return
	}

	terminalLogPath, err := s.prepareWritableInvocationLogPath(repoIdentity.RepoID, createResult.InvocationID, "terminal")
	if err != nil {
		s.failInvocationStart(repoIdentity.RepoID, createResult.InvocationID, "start_failed", true)
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to prepare terminal log: "+err.Error(), "", req.ClientRequestID, requestID)
		return
	}
	terminalFile, err := os.OpenFile(terminalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		s.failInvocationStart(repoIdentity.RepoID, createResult.InvocationID, "start_failed", true)
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to create terminal log: "+err.Error(), "", req.ClientRequestID, requestID)
		return
	}
	_ = terminalFile.Close()

	argv := append([]string{runnerCmd}, headedRunnerArgs...)
	if err := s.TmuxClient.NewSession(ctx, sessionName, createResult.SandboxPath, argv, req.Env); err != nil {
		s.failInvocationStart(repoIdentity.RepoID, createResult.InvocationID, "start_failed", true)
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to create tmux session: "+err.Error(), "ensure tmux is installed and working", req.ClientRequestID, requestID)
		return
	}
	target := tmux.SessionTarget(createResult.InvocationID)
	if scrollback, err := s.TmuxClient.CaptureScrollback(ctx, target); err != nil {
		s.recordInvocationWarning(repoIdentity.RepoID, createResult.InvocationID, "start_headed_tmux_capture_failed", err.Error(), map[string]any{
			"target": target,
		})
	} else if scrollback != "" {
		if err := os.WriteFile(terminalLogPath, []byte(scrollback), 0o600); err != nil {
			_ = s.TmuxClient.KillSession(ctx, sessionName)
			s.failInvocationStart(repoIdentity.RepoID, createResult.InvocationID, "start_failed", true)
			s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to write initial terminal capture: "+err.Error(), "", req.ClientRequestID, requestID)
			return
		}
	}
	if err := s.TmuxClient.PipePane(ctx, target, terminalLogPath); err != nil {
		_ = s.TmuxClient.KillSession(ctx, sessionName)
		s.failInvocationStart(repoIdentity.RepoID, createResult.InvocationID, "start_failed", true)
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to pipe tmux pane output: "+err.Error(), "ensure tmux pipe-pane is available", req.ClientRequestID, requestID)
		return
	}

	runnerArgs := append([]string(nil), req.RunnerArgs...)
	if err := s.claimHeadedInvocation(repoIdentity.RepoID, createResult.InvocationID, req.Runner, sessionName, runnerArgs, sortedEnvKeys(requestEnv)); err != nil {
		_ = s.TmuxClient.KillSession(ctx, sessionName)
		s.writeHeadedError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to update invocation meta: "+err.Error(), "", req.ClientRequestID, requestID)
		return
	}

	streamLogPath := s.Store.InvocationStreamLogPath(repoIdentity.RepoID, createResult.InvocationID)
	parser := stream.NewParser(createResult.InvocationID, req.Runner, s.Clock)
	parser.SetInitialSeq(loadMaxStreamSeq(streamLogPath))
	checkpointsDir := s.Store.InvocationDir(repoIdentity.RepoID, createResult.InvocationID)
	eventsPath := s.Store.InvocationEventsPath(repoIdentity.RepoID, createResult.InvocationID)
	cpConfig := checkpoint.DefaultConfig()
	cpConfig.IncludeUntracked = !req.NoIncludeUntracked
	cpConfig.Env = gitEnv
	if s.CheckpointDebounceOverride != nil {
		cpConfig.DebounceInterval = *s.CheckpointDebounceOverride
		cpConfig.DriftInterval = *s.CheckpointDebounceOverride
	}
	cpEngine := checkpoint.NewEngineWithWriter(
		createResult.InvocationID,
		repoIdentity.RepoID,
		createResult.SandboxPath,
		repoRoot,
		checkpointsDir,
		eventsPath,
		cpConfig,
		s.Runner,
		s.FS,
		s.Clock,
		s.InvocationEvents,
	)
	cpEngine.SetGitIgnoredDirs(checkpoint.ReadGitIgnoredDirs(createResult.SandboxPath))
	triggerCh := make(chan checkpoint.TriggerEvent, 32)
	cpEngine.SetTriggerChannel(triggerCh)

	proc := &SupervisedProcess{
		InvocationID:          createResult.InvocationID,
		RepoID:                repoIdentity.RepoID,
		IntegrationWorktreeID: prep.wtRecord.WorktreeID,
		Mode:                  "headed",
		TmuxSession:           sessionName,
		SandboxPath:           createResult.SandboxPath,
		StreamLogFile:         streamLogPath,
		Runner:                req.Runner,
		RepoRoot:              repoRoot,
		RunnerArgs:            runnerArgs,
		Env:                   copyStringMap(req.Env),
		NoIncludeUntracked:    req.NoIncludeUntracked,
		Parser:                parser,
		CheckpointEngine:      cpEngine,
		done:                  make(chan struct{}),
	}
	parser.SetCheckpointNotify(func(n stream.CheckpointNotification) {
		trigger := checkpoint.TriggerEvent{
			Kind:      checkpoint.TriggerToolEnd,
			ToolName:  n.ToolName,
			ToolNames: n.ToolNames,
			Seq:       n.Seq,
		}
		select {
		case triggerCh <- trigger:
			return
		default:
		}

		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
		select {
		case triggerCh <- trigger:
		case <-timer.C:
			s.recordInvocationWarning(repoIdentity.RepoID, createResult.InvocationID, "checkpoint_trigger_dropped", "checkpoint trigger queue full; dropped semantic trigger", map[string]any{
				"seq":       n.Seq,
				"tool_name": n.ToolName,
			})
		}
	})
	parser.SetFinalNotify(func(n stream.FinalNotification) {
		s.handleSuccessfulFinalNotification(proc, n)
	})
	parser.SetSessionStartNotify(func(n stream.SessionStartNotification) {
		proc.SetResumeSessionID(n.SessionID)
	})
	s.replaceInvocationProcess(createResult.InvocationID, proc)
	s.supervisionWg.Add(2)
	go s.runOutputFlushLoop(proc)
	go s.runCheckpointLoop(proc)

	s.recordHeadedIdempotency(repoIdentity.RepoID, req.ClientRequestID, createResult.InvocationID, fingerprint, sessionName, createResult.SandboxPath)

	meta, _ := s.Store.ReadInvocationMeta(repoIdentity.RepoID, createResult.InvocationID)
	s.writeHeadedSuccess(w, createResult.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, false)
}
