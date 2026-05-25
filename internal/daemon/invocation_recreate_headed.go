package daemon

import (
	"net/http"
	"os"
	"slices"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

func (s *Server) handleRecreateHeaded(w http.ResponseWriter, r *http.Request, invocationRef string) {
	ctx := r.Context()
	requestID := prepareRequestID(w, r)
	respondErr := func(status int, code, message, hint string) {
		s.writeHeadedError(w, status, code, message, hint, "", requestID)
	}

	var req struct{}
	if err := decodeOptionalStrictJSON(r.Body, &req); err != nil {
		respondErr(http.StatusBadRequest, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "")
		return
	}

	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		respondErr(http.StatusBadRequest, string(errors.EInvalidRequest), "repo_id query parameter is required", "")
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		status, code := invocationResolveStatus(resolveErr)
		respondErr(status, string(code), resolveErr.Error(), "use 'agency agent ls --repo <repo>' to list invocations")
		return
	}

	unlock, fail := s.lockRepoOrFailure(record.RepoID, "recreate_headed")
	if fail != nil {
		respondErr(fail.status, string(fail.code), fail.msg, fail.hint)
		return
	}
	defer func() { _ = unlock() }()

	meta, err := s.store.ReadInvocationMeta(record.RepoID, record.InvocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			respondErr(http.StatusNotFound, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		respondErr(http.StatusInternalServerError, string(errors.EInternal), "failed to read invocation meta: "+err.Error(), "")
		return
	}
	if meta.Mode != store.RunnerModeHeaded {
		respondErr(http.StatusBadRequest, string(errors.EInvocationInvalidMode), "recreate is only supported for headed invocations", "use 'agency agent <invocation-ref> history' to inspect or 'agency agent <invocation-ref> restore' to roll back a headless invocation")
		return
	}
	if meta.LandingStatus == store.LandingStatusLanded {
		respondErr(http.StatusConflict, string(errors.ELandAlreadyLanded), "invocation has already been landed", "start a new invocation from an active integration worktree")
		return
	}
	if meta.LandingStatus == store.LandingStatusDiscarded {
		respondErr(http.StatusConflict, string(errors.ELandAlreadyDiscarded), "invocation has already been discarded", "start a new invocation from an active integration worktree")
		return
	}
	info, err := os.Stat(meta.SandboxPath)
	if err != nil {
		if os.IsNotExist(err) {
			respondErr(http.StatusNotFound, string(errors.ESandboxMissing), "sandbox no longer exists", "sandbox was removed after landing or discarding")
			return
		}
		respondErr(http.StatusInternalServerError, string(errors.ESandboxMissing), "failed to inspect sandbox: "+err.Error(), "")
		return
	}
	if !info.IsDir() {
		respondErr(http.StatusNotFound, string(errors.ESandboxMissing), "sandbox path is not a directory", "")
		return
	}

	sessionName, ok := headedInvocationSessionName(meta)
	if !ok {
		respondErr(http.StatusInternalServerError, string(errors.ETmuxSessionMissing), "headed invocation is missing tmux_session", "inspect invocation metadata before recreating")
		return
	}
	exists, err := s.tmuxClient.HasSession(ctx, sessionName)
	if err != nil && !tmux.IsNoSessionErr(err) {
		respondErr(http.StatusInternalServerError, string(errors.ETmuxFailed), "failed to check tmux session: "+err.Error(), "")
		return
	}
	if exists {
		s.mu.RLock()
		_, supervised := s.processes[record.InvocationID]
		s.mu.RUnlock()
		if supervised {
			updatedMeta, err := s.claimHeadedInvocation(record.RepoID, record.InvocationID, "", meta.Runner, sessionName, slices.Clone(meta.RunnerArgs), slices.Clone(meta.CustomEnvKeys))
			if err != nil {
				respondErr(http.StatusInternalServerError, string(errors.EInternal), "failed to update invocation meta: "+err.Error(), "")
				return
			}
			s.writeHeadedSuccess(w, record.InvocationID, updatedMeta, record.RepoID, "", requestID, true)
			return
		}
		updatedMeta, err := s.restoreExistingHeadedSupervision(ctx, record.RepoID, record.InvocationID, meta, sessionName, "agency.headed_recreated")
		if err != nil {
			respondErr(http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to restore headed supervision: "+err.Error(), "ensure tmux is still available")
			return
		}
		s.writeHeadedSuccess(w, record.InvocationID, updatedMeta, record.RepoID, "", requestID, true)
		return
	}

	canonicalRunner, err := validateControlPlaneStartRunner(meta.Runner, meta.RunnerArgs, false)
	if err != nil {
		fail := runnerValidationFailure(err)
		respondErr(fail.status, string(fail.code), fail.msg, fail.hint)
		return
	}
	headedRunnerArgs, err := buildRunnerArgsForHeaded(canonicalRunner, meta.RunnerArgs)
	if err != nil {
		fail := headedRunnerArgsFailure(err)
		respondErr(fail.status, string(fail.code), fail.msg, fail.hint)
		return
	}
	repoRoot, err := s.resolveRegisteredRepoRoot(record.RepoID)
	if err != nil {
		respondErr(http.StatusInternalServerError, string(errors.ERepoNotFound), "failed to resolve repo root: "+err.Error(), "run 'agency repo add <path>' to refresh the repo registry")
		return
	}
	repoInfo, err := os.Stat(repoRoot)
	if err != nil {
		respondErr(http.StatusConflict, string(errors.ERepoRootInaccessible), "repo root is not accessible: "+err.Error(), "run 'agency repo add <path>' to refresh the repo registry")
		return
	}
	if !repoInfo.IsDir() {
		respondErr(http.StatusConflict, string(errors.ERepoRootInaccessible), "repo root is not a directory", "run 'agency repo add <path>' to refresh the repo registry")
		return
	}

	userCfg, err := s.LoadUserConfig()
	if err != nil {
		respondErr(http.StatusInternalServerError, string(errors.EInvalidUserConfig), "failed to load user config: "+err.Error(), "run `agency config init`")
		return
	}
	profileEnv, err := config.ExecutionProfileEnv(userCfg, meta.ExecutionProfile)
	if err != nil {
		code := errors.CodeOr(err, errors.EExecutionProfileNotFound)
		respondErr(http.StatusBadRequest, string(code), apiErrorMessage(err), "")
		return
	}
	launchEnv := copyStringMap(profileEnv)
	runnerCmd, err := config.ResolveRunnerCmd(s.runner, s.fsys, s.configDir, userCfg, canonicalRunner)
	if err != nil {
		respondErr(http.StatusInternalServerError, string(errors.ERunnerNotFound), "failed to resolve runner command: "+err.Error(), "ensure runner is installed and configured")
		return
	}
	if err := s.installHeadedRunnerHooks(ctx, record.RepoID, record.InvocationID, canonicalRunner, headedRunnerArgs, meta.SandboxPath, withNonInteractiveEnv(launchEnv)); err != nil {
		respondErr(http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to install headed runner hooks: "+err.Error(), "ensure sandbox hook files can be written")
		return
	}

	terminalLogPath, err := s.prepareWritableInvocationLogPath(record.RepoID, record.InvocationID, InvocationLogKindTerminal)
	if err != nil {
		respondErr(http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to prepare terminal log: "+err.Error(), "")
		return
	}
	terminalFile, err := os.OpenFile(terminalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		respondErr(http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to create terminal log: "+err.Error(), "")
		return
	}
	_ = terminalFile.Close()

	if err := s.tmuxClient.NewSession(ctx, sessionName, meta.SandboxPath, append([]string{runnerCmd}, headedRunnerArgs...), launchEnv); err != nil {
		respondErr(http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to create tmux session: "+err.Error(), "ensure tmux is installed and working")
		return
	}
	target := sessionName + ":0.0"
	if scrollback, err := s.tmuxClient.CaptureScrollback(ctx, target); err != nil {
		s.recordInvocationWarning(record.RepoID, record.InvocationID, "recreate_headed_tmux_capture_failed", err.Error(), map[string]any{
			"target": target,
		})
	} else if scrollback != "" {
		f, err := os.OpenFile(terminalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			_ = s.tmuxClient.KillSession(ctx, sessionName)
			respondErr(http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to append initial terminal capture: "+err.Error(), "")
			return
		}
		_, writeErr := f.WriteString(scrollback)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			_ = s.tmuxClient.KillSession(ctx, sessionName)
			if writeErr != nil {
				respondErr(http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to append initial terminal capture: "+writeErr.Error(), "")
				return
			}
			respondErr(http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to close terminal log: "+closeErr.Error(), "")
			return
		}
	}
	if err := s.tmuxClient.PipePane(ctx, target, terminalLogPath); err != nil {
		_ = s.tmuxClient.KillSession(ctx, sessionName)
		respondErr(http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to pipe tmux pane output: "+err.Error(), "ensure tmux pipe-pane is available")
		return
	}

	runnerArgs := slices.Clone(meta.RunnerArgs)
	updatedMeta, err := s.claimHeadedInvocation(record.RepoID, record.InvocationID, "", canonicalRunner, sessionName, runnerArgs, slices.Clone(meta.CustomEnvKeys))
	if err != nil {
		_ = s.tmuxClient.KillSession(ctx, sessionName)
		respondErr(http.StatusInternalServerError, string(errors.EInternal), "failed to update invocation meta: "+err.Error(), "")
		return
	}

	proc := s.buildSupervisedHeadedProcess(ctx, supervisedHeadedSetup{
		invocationID:          record.InvocationID,
		repoID:                record.RepoID,
		integrationWorktreeID: meta.IntegrationWorktreeID,
		repoRoot:              repoRoot,
		sandboxPath:           meta.SandboxPath,
		sessionName:           sessionName,
		runner:                canonicalRunner,
		runnerArgs:            runnerArgs,
		launchEnv:             launchEnv,
		includeUntracked:      meta.CheckpointIncludeUntracked,
		gitEnv:                withNonInteractiveEnv(launchEnv),
	})
	if _, err := s.invocationEvents.Append(s.store.InvocationEventsPath(record.RepoID, record.InvocationID), record.InvocationID, "agency.headed_recreated", map[string]any{
		"tmux_session": sessionName,
	}, eventlog.AppendOptions{}); err != nil {
		_ = s.tmuxClient.KillSession(ctx, sessionName)
		s.failInvocationStart(record.RepoID, record.InvocationID, "event_append_failed", true)
		respondErr(http.StatusInternalServerError, string(errors.EInternal), "failed to append recreate event: "+err.Error(), "")
		return
	}
	s.launchSupervisedHeadedProcess(proc)

	s.writeHeadedSuccess(w, record.InvocationID, updatedMeta, record.RepoID, "", requestID, false)
}
