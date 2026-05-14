package daemon

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

func (s *Server) handleRecreateHeaded(w http.ResponseWriter, r *http.Request, invocationRef string) {
	ctx := r.Context()
	requestID := prepareRequestID(w, r)

	var req struct{}
	if err := decodeOptionalStrictJSON(r.Body, &req); err != nil {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvalidRequest), "invalid request body: "+err.Error(), "", "", requestID)
		return
	}

	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvalidRequest), "repo_id query parameter is required", "", "", requestID)
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		code := errors.GetCode(resolveErr)
		if code == "" {
			code = errors.EInvocationNotFound
		}
		status := http.StatusNotFound
		if code == errors.EInvocationIDAmbiguous {
			status = http.StatusConflict
		}
		s.writeHeadedError(w, status, string(code), resolveErr.Error(), "use 'agency agent ls --repo <repo>' to list invocations", "", requestID)
		return
	}

	unlock, err := s.repoLock.Lock(record.RepoID, "recreate_headed")
	if err != nil {
		s.writeHeadedError(w, http.StatusConflict, string(errors.ERepoLocked), "repository is locked by another operation", "wait for the other operation to complete", "", requestID)
		return
	}
	defer func() { _ = unlock() }()

	meta, err := s.Store.ReadInvocationMeta(record.RepoID, record.InvocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.writeHeadedError(w, http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "", "", requestID)
			return
		}
		s.writeHeadedError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "", "", requestID)
		return
	}
	if meta.Mode != store.RunnerModeHeaded {
		s.writeHeadedError(w, http.StatusBadRequest, string(errors.EInvocationInvalidMode), "recreate is only supported for headed invocations", "use 'agency agent <invocation-ref> history' to inspect or 'agency agent <invocation-ref> restore' to roll back a headless invocation", "", requestID)
		return
	}
	if meta.LandingStatus == store.LandingStatusLanded {
		s.writeHeadedError(w, http.StatusConflict, string(errors.ELandAlreadyLanded), "invocation has already been landed", "start a new invocation from an active integration worktree", "", requestID)
		return
	}
	if meta.LandingStatus == store.LandingStatusDiscarded {
		s.writeHeadedError(w, http.StatusConflict, string(errors.ELandAlreadyDiscarded), "invocation has already been discarded", "start a new invocation from an active integration worktree", "", requestID)
		return
	}
	info, err := os.Stat(meta.SandboxPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.writeHeadedError(w, http.StatusNotFound, string(errors.ESandboxMissing), "sandbox no longer exists", "sandbox was removed after landing or discarding", "", requestID)
			return
		}
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.ESandboxMissing), "failed to inspect sandbox: "+err.Error(), "", "", requestID)
		return
	}
	if !info.IsDir() {
		s.writeHeadedError(w, http.StatusNotFound, string(errors.ESandboxMissing), "sandbox path is not a directory", "", "", requestID)
		return
	}

	sessionName := meta.TmuxSession
	if sessionName == "" {
		sessionName = tmux.SessionName(record.InvocationID)
	}
	exists, err := s.TmuxClient.HasSession(ctx, sessionName)
	if err != nil && !tmux.IsNoSessionErr(err) {
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.ETmuxFailed), "failed to check tmux session: "+err.Error(), "", "", requestID)
		return
	}
	if exists {
		s.mu.RLock()
		_, supervised := s.processes[record.InvocationID]
		s.mu.RUnlock()
		if supervised {
			if err := s.claimHeadedInvocation(record.RepoID, record.InvocationID, meta.Runner, sessionName, append([]string(nil), meta.RunnerArgs...), append([]string(nil), meta.CustomEnvKeys...)); err != nil {
				s.writeHeadedError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to update invocation meta: "+err.Error(), "", "", requestID)
				return
			}
			meta, _ = s.Store.ReadInvocationMeta(record.RepoID, record.InvocationID)
			s.writeHeadedSuccess(w, record.InvocationID, meta, record.RepoID, "", requestID, true)
			return
		}
		if err := s.restoreExistingHeadedSupervision(ctx, record.RepoID, record.InvocationID, meta, sessionName, "agency.headed_recreated"); err != nil {
			s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to restore headed supervision: "+err.Error(), "ensure tmux is still available", "", requestID)
			return
		}
		meta, _ = s.Store.ReadInvocationMeta(record.RepoID, record.InvocationID)
		s.writeHeadedSuccess(w, record.InvocationID, meta, record.RepoID, "", requestID, true)
		return
	}

	canonicalRunner, err := validateControlPlaneStartRunner(meta.Runner, meta.RunnerArgs, false)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerArgConflict
		}
		hint := "remove reserved flags from runner_args"
		if code == errors.ERunnerNotFound {
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		}
		s.writeHeadedError(w, http.StatusBadRequest, string(code), err.Error(), hint, "", requestID)
		return
	}
	headedRunnerArgs, err := buildRunnerArgsForHeaded(canonicalRunner, meta.RunnerArgs)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		hint := ""
		if code == errors.ERunnerNotFound {
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		}
		if code == errors.EInvocationInvalidMode {
			hint = "runner does not support headed mode"
		}
		status := http.StatusInternalServerError
		if code == errors.ERunnerNotFound || code == errors.EInvocationInvalidMode {
			status = http.StatusBadRequest
		}
		s.writeHeadedError(w, status, string(code), err.Error(), hint, "", requestID)
		return
	}
	repoRoot, err := s.resolveHeadedSupervisionRepoRoot(record.RepoID)
	if err != nil {
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.ERepoNotFound), "failed to resolve repo root: "+err.Error(), "run 'agency repo add <path>' to refresh the repo registry", "", requestID)
		return
	}
	repoInfo, err := os.Stat(repoRoot)
	if err != nil {
		s.writeHeadedError(w, http.StatusConflict, string(errors.ERepoRootInaccessible), "repo root is not accessible: "+err.Error(), "run 'agency repo add <path>' to refresh the repo registry", "", requestID)
		return
	}
	if !repoInfo.IsDir() {
		s.writeHeadedError(w, http.StatusConflict, string(errors.ERepoRootInaccessible), "repo root is not a directory", "run 'agency repo add <path>' to refresh the repo registry", "", requestID)
		return
	}

	userCfg, err := s.LoadUserConfig()
	if err != nil {
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvalidUserConfig), "failed to load user config: "+err.Error(), "run `agency config init`", "", requestID)
		return
	}
	profileEnv, err := config.ExecutionProfileEnv(userCfg, meta.ExecutionProfile)
	if err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EExecutionProfileNotFound
		}
		s.writeHeadedError(w, http.StatusBadRequest, string(code), apiErrorMessage(err), "", "", requestID)
		return
	}
	launchEnv := copyStringMap(profileEnv)
	runnerCmd, err := config.ResolveRunnerCmd(s.Runner, s.FS, s.ConfigDir, userCfg, canonicalRunner)
	if err != nil {
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.ERunnerNotFound), "failed to resolve runner command: "+err.Error(), "ensure runner is installed and configured", "", requestID)
		return
	}
	if err := s.installHeadedRunnerHooks(ctx, record.RepoID, record.InvocationID, canonicalRunner, headedRunnerArgs, meta.SandboxPath, prSyncNonInteractiveEnv(launchEnv)); err != nil {
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to install headed runner hooks: "+err.Error(), "ensure sandbox hook files can be written", "", requestID)
		return
	}

	terminalLogPath, err := s.prepareWritableInvocationLogPath(record.RepoID, record.InvocationID, "terminal")
	if err != nil {
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to prepare terminal log: "+err.Error(), "", "", requestID)
		return
	}
	terminalFile, err := os.OpenFile(terminalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to create terminal log: "+err.Error(), "", "", requestID)
		return
	}
	_ = terminalFile.Close()

	if err := s.TmuxClient.NewSession(ctx, sessionName, meta.SandboxPath, append([]string{runnerCmd}, headedRunnerArgs...), launchEnv); err != nil {
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to create tmux session: "+err.Error(), "ensure tmux is installed and working", "", requestID)
		return
	}
	target := sessionName + ":0.0"
	if scrollback, err := s.TmuxClient.CaptureScrollback(ctx, target); err != nil {
		s.recordInvocationWarning(record.RepoID, record.InvocationID, "recreate_headed_tmux_capture_failed", err.Error(), map[string]any{
			"target": target,
		})
	} else if scrollback != "" {
		f, err := os.OpenFile(terminalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			_ = s.TmuxClient.KillSession(ctx, sessionName)
			s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to append initial terminal capture: "+err.Error(), "", "", requestID)
			return
		}
		_, writeErr := f.WriteString(scrollback)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			_ = s.TmuxClient.KillSession(ctx, sessionName)
			if writeErr != nil {
				s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to append initial terminal capture: "+writeErr.Error(), "", "", requestID)
				return
			}
			s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to close terminal log: "+closeErr.Error(), "", "", requestID)
			return
		}
	}
	if err := s.TmuxClient.PipePane(ctx, target, terminalLogPath); err != nil {
		_ = s.TmuxClient.KillSession(ctx, sessionName)
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to pipe tmux pane output: "+err.Error(), "ensure tmux pipe-pane is available", "", requestID)
		return
	}

	runnerArgs := append([]string(nil), meta.RunnerArgs...)
	if err := s.claimHeadedInvocation(record.RepoID, record.InvocationID, canonicalRunner, sessionName, runnerArgs, append([]string(nil), meta.CustomEnvKeys...)); err != nil {
		_ = s.TmuxClient.KillSession(ctx, sessionName)
		s.writeHeadedError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to update invocation meta: "+err.Error(), "", "", requestID)
		return
	}

	streamLogPath := s.Store.InvocationStreamLogPath(record.RepoID, record.InvocationID)
	parser := stream.NewParser(record.InvocationID, canonicalRunner, s.Clock)
	parser.SetInitialSeq(loadMaxStreamSeq(streamLogPath))
	checkpointsDir := s.Store.InvocationDir(record.RepoID, record.InvocationID)
	eventsPath := s.Store.InvocationEventsPath(record.RepoID, record.InvocationID)
	cpConfig := checkpoint.DefaultConfig()
	cpConfig.IncludeUntracked = meta.CheckpointIncludeUntracked
	cpConfig.Env = prSyncNonInteractiveEnv(launchEnv)
	if s.CheckpointDebounceOverride != nil {
		cpConfig.DebounceInterval = *s.CheckpointDebounceOverride
		cpConfig.DriftInterval = *s.CheckpointDebounceOverride
	}
	cpEngine := checkpoint.NewEngineWithWriter(
		record.InvocationID,
		record.RepoID,
		meta.SandboxPath,
		repoRoot,
		checkpointsDir,
		eventsPath,
		cpConfig,
		s.Runner,
		s.FS,
		s.Clock,
		s.InvocationEvents,
	)
	cpEngine.SetGitIgnoredDirs(checkpoint.ReadGitIgnoredDirs(meta.SandboxPath))
	triggerCh := make(chan checkpoint.TriggerEvent, 32)
	cpEngine.SetTriggerChannel(triggerCh)

	proc := &SupervisedProcess{
		InvocationID:          record.InvocationID,
		RepoID:                record.RepoID,
		IntegrationWorktreeID: meta.IntegrationWorktreeID,
		Mode:                  "headed",
		TmuxSession:           sessionName,
		SandboxPath:           meta.SandboxPath,
		StreamLogFile:         streamLogPath,
		Runner:                canonicalRunner,
		RepoRoot:              repoRoot,
		RunnerArgs:            runnerArgs,
		Env:                   copyStringMap(launchEnv),
		NoIncludeUntracked:    !meta.CheckpointIncludeUntracked,
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
			s.recordInvocationWarning(record.RepoID, record.InvocationID, "checkpoint_trigger_dropped", "checkpoint trigger queue full; dropped semantic trigger", map[string]any{
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

	if _, err := s.InvocationEvents.Append(eventsPath, record.InvocationID, "agency.headed_recreated", map[string]any{
		"tmux_session": sessionName,
	}, invocationevents.AppendOptions{}); err != nil {
		_ = s.TmuxClient.KillSession(ctx, sessionName)
		s.failInvocationStart(record.RepoID, record.InvocationID, "event_append_failed", true)
		s.writeHeadedError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to append recreate event: "+err.Error(), "", "", requestID)
		return
	}

	s.replaceInvocationProcess(record.InvocationID, proc)
	go s.runOutputFlushLoop(proc)
	go s.runCheckpointLoop(proc)

	meta, _ = s.Store.ReadInvocationMeta(record.RepoID, record.InvocationID)
	s.writeHeadedSuccess(w, record.InvocationID, meta, record.RepoID, "", requestID, false)
}
