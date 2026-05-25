package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"slices"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// resolveAccessibleRegisteredRepoRoot resolves a registered repo's root and
// verifies it points at an accessible directory.
func (s *Server) resolveAccessibleRegisteredRepoRoot(repoID string) (string, *startFailure) {
	repoRoot, err := s.resolveRegisteredRepoRoot(repoID)
	if err != nil {
		f := newStartFailure(http.StatusInternalServerError, errors.ERepoNotFound, "failed to resolve repo root: "+err.Error(), "run 'agency repo add <path>' to refresh the repo registry")
		return "", &f
	}
	info, err := os.Stat(repoRoot)
	if err != nil {
		f := newStartFailure(http.StatusConflict, errors.ERepoRootInaccessible, "repo root is not accessible: "+err.Error(), "run 'agency repo add <path>' to refresh the repo registry")
		return "", &f
	}
	if !info.IsDir() {
		f := newStartFailure(http.StatusConflict, errors.ERepoRootInaccessible, "repo root is not a directory", "run 'agency repo add <path>' to refresh the repo registry")
		return "", &f
	}
	return repoRoot, nil
}

// loadRecreatableHeadedMeta reads the invocation meta and confirms it can be
// recreated: headed mode, not landed/discarded, sandbox directory exists.
func (s *Server) loadRecreatableHeadedMeta(repoID, invocationID string) (*store.InvocationMeta, *startFailure) {
	meta, err := s.store.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			f := newStartFailure(http.StatusNotFound, errors.EInvocationNotFound, "invocation not found", "")
			return nil, &f
		}
		f := newStartFailure(http.StatusInternalServerError, errors.EInternal, "failed to read invocation meta: "+err.Error(), "")
		return nil, &f
	}
	if meta.Mode != store.RunnerModeHeaded {
		f := newStartFailure(http.StatusBadRequest, errors.EInvocationInvalidMode, "recreate is only supported for headed invocations", "use 'agency agent <invocation-ref> history' to inspect or 'agency agent <invocation-ref> restore' to roll back a headless invocation")
		return nil, &f
	}
	if meta.LandingStatus == store.LandingStatusLanded {
		f := newStartFailure(http.StatusConflict, errors.ELandAlreadyLanded, "invocation has already been landed", "start a new invocation from an active integration worktree")
		return nil, &f
	}
	if meta.LandingStatus == store.LandingStatusDiscarded {
		f := newStartFailure(http.StatusConflict, errors.ELandAlreadyDiscarded, "invocation has already been discarded", "start a new invocation from an active integration worktree")
		return nil, &f
	}
	info, err := os.Stat(meta.SandboxPath)
	if err != nil {
		if os.IsNotExist(err) {
			f := newStartFailure(http.StatusNotFound, errors.ESandboxMissing, "sandbox no longer exists", "sandbox was removed after landing or discarding")
			return nil, &f
		}
		f := newStartFailure(http.StatusInternalServerError, errors.ESandboxMissing, "failed to inspect sandbox: "+err.Error(), "")
		return nil, &f
	}
	if !info.IsDir() {
		f := newStartFailure(http.StatusNotFound, errors.ESandboxMissing, "sandbox path is not a directory", "")
		return nil, &f
	}
	return meta, nil
}

// reattachExistingHeadedSession returns the post-attach meta when a tmux
// session for this invocation is already live. If the invocation is also
// already supervised, only meta is refreshed; otherwise supervision is
// restored against the existing session.
func (s *Server) reattachExistingHeadedSession(ctx context.Context, record *resolvedInvocation, meta *store.InvocationMeta, sessionName string) (*store.InvocationMeta, *startFailure) {
	s.mu.RLock()
	_, supervised := s.processes[record.InvocationID]
	s.mu.RUnlock()
	if supervised {
		updatedMeta, err := s.claimHeadedInvocation(record.RepoID, record.InvocationID, "", meta.Runner, sessionName, slices.Clone(meta.RunnerArgs), slices.Clone(meta.CustomEnvKeys))
		if err != nil {
			f := newStartFailure(http.StatusInternalServerError, errors.EInternal, "failed to update invocation meta: "+err.Error(), "")
			return nil, &f
		}
		return updatedMeta, nil
	}
	updatedMeta, err := s.restoreExistingHeadedSupervision(ctx, record.RepoID, record.InvocationID, meta, sessionName, "agency.headed_recreated")
	if err != nil {
		f := newStartFailure(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to restore headed supervision: "+err.Error(), "ensure tmux is still available")
		return nil, &f
	}
	return updatedMeta, nil
}

// startHeadedTmuxSession creates the tmux session, replays any captured
// scrollback into the terminal log, and starts piping the pane. On any
// failure the partially-created session is killed.
func (s *Server) startHeadedTmuxSession(ctx context.Context, repoID, invocationID, sessionName, sandboxPath, runnerCmd string, runnerArgs []string, launchEnv map[string]string, terminalLogPath string) error {
	if err := s.tmuxClient.NewSession(ctx, sessionName, sandboxPath, append([]string{runnerCmd}, runnerArgs...), launchEnv); err != nil {
		return fmt.Errorf("failed to create tmux session: %w", err)
	}
	target := sessionName + ":0.0"
	if scrollback, err := s.tmuxClient.CaptureScrollback(ctx, target); err != nil {
		s.recordInvocationWarning(repoID, invocationID, "recreate_headed_tmux_capture_failed", err.Error(), map[string]any{
			"target": target,
		})
	} else if scrollback != "" {
		if err := appendTerminalLog(terminalLogPath, scrollback); err != nil {
			_ = s.tmuxClient.KillSession(ctx, sessionName)
			return err
		}
	}
	if err := s.tmuxClient.PipePane(ctx, target, terminalLogPath); err != nil {
		_ = s.tmuxClient.KillSession(ctx, sessionName)
		return fmt.Errorf("failed to pipe tmux pane output: %w", err)
	}
	return nil
}

func appendTerminalLog(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to append initial terminal capture: %w", err)
	}
	_, writeErr := f.WriteString(content)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("failed to append initial terminal capture: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close terminal log: %w", closeErr)
	}
	return nil
}

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

	meta, fail := s.loadRecreatableHeadedMeta(record.RepoID, record.InvocationID)
	if fail != nil {
		respondErr(fail.status, string(fail.code), fail.msg, fail.hint)
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
		updatedMeta, fail := s.reattachExistingHeadedSession(ctx, record, meta, sessionName)
		if fail != nil {
			respondErr(fail.status, string(fail.code), fail.msg, fail.hint)
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
	repoRoot, fail := s.resolveAccessibleRegisteredRepoRoot(record.RepoID)
	if fail != nil {
		respondErr(fail.status, string(fail.code), fail.msg, fail.hint)
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

	if err := s.startHeadedTmuxSession(ctx, record.RepoID, record.InvocationID, sessionName, meta.SandboxPath, runnerCmd, headedRunnerArgs, launchEnv, terminalLogPath); err != nil {
		respondErr(http.StatusInternalServerError, string(errors.EInvocationStartFailed), err.Error(), "")
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
