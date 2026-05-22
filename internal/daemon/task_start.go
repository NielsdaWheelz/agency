package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	agencylock "github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

type taskStartFailure struct {
	status int
	code   errors.Code
	msg    string
	hint   string
}

func (e taskStartFailure) Error() string {
	if e.code == "" {
		return e.msg
	}
	return string(e.code) + ": " + e.msg
}

func newTaskStartFailure(status int, code errors.Code, msg, hint string) taskStartFailure {
	if code == "" {
		code = errors.EInternal
	}
	return taskStartFailure{status: status, code: code, msg: msg, hint: hint}
}

func taskStartFailureFromError(status int, defaultCode errors.Code, err error, hint string) taskStartFailure {
	code := errors.CodeOr(err, defaultCode)
	return newTaskStartFailure(status, code, apiErrorMessage(err), hint)
}

func (s *Server) handleTaskStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := prepareRequestID(w, r)
	writeErr := func(status int, code errors.Code, message, hint, clientRequestID string) {
		s.writeTaskStartError(w, status, requestID, code, message, hint, clientRequestID, nil)
	}

	var req TaskStartRequest
	if err := decodeStrictJSON(r.Body, &req); err != nil {
		writeErr(http.StatusBadRequest, errors.EInvalidRequest, strictJSONDecodeErrorMessage(err), "", "")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.BaseBranch = strings.TrimSpace(req.BaseBranch)
	req.Mode = strings.TrimSpace(req.Mode)
	if req.Mode == "" {
		req.Mode = string(store.RunnerModeHeadless)
	}
	req.Runner = strings.TrimSpace(req.Runner)
	req.InvocationName = strings.TrimSpace(req.InvocationName)
	req.ExecutionProfile = strings.TrimSpace(req.ExecutionProfile)
	req.AgencyConfigPath = strings.TrimSpace(req.AgencyConfigPath)
	if req.AgencyConfigPath != "" && !filepath.IsAbs(req.AgencyConfigPath) {
		writeErr(http.StatusBadRequest, errors.EInvalidArgument, "agency_config_path must be absolute", "", req.ClientRequestID)
		return
	}

	if req.ClientRequestID == "" {
		writeErr(http.StatusBadRequest, errors.EInvalidRequest, "client_request_id is required", "provide a UUID for idempotency", "")
		return
	}
	if req.RepoRoot == "" {
		writeErr(http.StatusBadRequest, errors.EInvalidRequest, "repo_root is required", "", req.ClientRequestID)
		return
	}
	if req.Name == "" {
		writeErr(http.StatusBadRequest, errors.EInvalidRequest, "name is required", "", req.ClientRequestID)
		return
	}
	if err := core.ValidateName(req.Name); err != nil {
		writeErr(http.StatusBadRequest, errors.EInvalidName, "invalid task name: "+err.Error(), "names must be 2-40 chars, lowercase alphanumeric + hyphens", req.ClientRequestID)
		return
	}
	if req.BaseBranch == "" {
		writeErr(http.StatusBadRequest, errors.EInvalidRequest, "base_branch is required", "", req.ClientRequestID)
		return
	}
	if req.Runner == "" {
		writeErr(http.StatusBadRequest, errors.EInvalidRequest, "runner is required", "", req.ClientRequestID)
		return
	}

	headless := req.Mode == string(store.RunnerModeHeadless)
	switch req.Mode {
	case string(store.RunnerModeHeadless):
		if req.Prompt == "" {
			writeErr(http.StatusBadRequest, errors.EPromptRequired, "prompt is required for headless task", "", req.ClientRequestID)
			return
		}
		if len(req.Prompt) > MaxPromptSize {
			writeErr(http.StatusBadRequest, errors.EPromptTooLarge, fmt.Sprintf("prompt exceeds maximum size of %d bytes (got %d)", MaxPromptSize, len(req.Prompt)), "reduce prompt size or split into smaller chunks", req.ClientRequestID)
			return
		}
	case string(store.RunnerModeHeaded):
		if req.Prompt != "" {
			writeErr(http.StatusBadRequest, errors.EUsage, "headed task start does not accept a prompt", "omit --prompt/--prompt-file or use --mode headless", req.ClientRequestID)
			return
		}
	default:
		writeErr(http.StatusBadRequest, errors.EInvalidArgument, "mode must be headless or headed", "", req.ClientRequestID)
		return
	}

	canonicalRunner, err := validateControlPlaneStartRunner(req.Runner, req.RunnerArgs, headless)
	if err != nil {
		code := errors.CodeOr(err, errors.ERunnerArgConflict)
		hint := "remove reserved flags from runner_args"
		if code == errors.ERunnerNotFound {
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		}
		writeErr(http.StatusBadRequest, code, err.Error(), hint, req.ClientRequestID)
		return
	}
	req.Runner = canonicalRunner
	if !headless {
		if _, err := buildRunnerArgsForHeaded(req.Runner, req.RunnerArgs); err != nil {
			code := errors.CodeOr(err, errors.EInternal)
			hint := ""
			status := http.StatusInternalServerError
			if code == errors.ERunnerNotFound || code == errors.EInvocationInvalidMode {
				status = http.StatusBadRequest
			}
			writeErr(status, code, err.Error(), hint, req.ClientRequestID)
			return
		}
	}
	if err := validateControlPlaneStartInvocationName(req.InvocationName); err != nil {
		writeErr(http.StatusBadRequest, errors.EInvalidName, "invalid invocation name: "+err.Error(), "names must be 2-40 chars, lowercase alphanumeric + hyphens", req.ClientRequestID)
		return
	}

	repoRoot, repoIdentity, ok := s.resolveControlPlaneRepoRoot(ctx, req.RepoRoot, func(status int, code, message, hint string) {
		writeErr(status, errors.Code(code), message, hint, req.ClientRequestID)
	})
	if !ok {
		return
	}

	requestEnv := copyStringMap(req.Env)
	execCtx, err := s.resolveExecutionContext(repoRoot, repoIdentity.RepoID, req.AgencyConfigPath, req.ExecutionProfile)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		writeErr(http.StatusBadRequest, code, apiErrorMessage(err), "", req.ClientRequestID)
		return
	}
	req.ExecutionProfile = execCtx.Profile
	req.CheckoutRoot = execCtx.CheckoutRoot
	gitEnv := prSyncNonInteractiveEnv(execCtx.ProfileEnv)
	req.Env = envForLaunch(execCtx.ProfileEnv, requestEnv)

	fingerprint := taskStartFingerprint(repoRoot, execCtx.CheckoutRoot, req, requestEnv)
	if s.writeTaskStartIdempotencyResult(w, requestID, req.ClientRequestID, repoIdentity.RepoID, fingerprint, false) {
		return
	}

	unlock, err := s.acquireControlPlaneRepoLock(repoIdentity.RepoID, "task start")
	if err != nil {
		var lockedErr *agencylock.ErrLocked
		if !stderrors.As(err, &lockedErr) {
			writeErr(http.StatusInternalServerError, errors.EInternal, "failed to acquire repository lock: "+err.Error(), "", req.ClientRequestID)
			return
		}
		writeErr(http.StatusConflict, errors.ERepoLocked, "repository is locked by another operation", "wait for the other operation to complete", req.ClientRequestID)
		return
	}
	defer func() { _ = unlock() }()

	if s.writeTaskStartIdempotencyResult(w, requestID, req.ClientRequestID, repoIdentity.RepoID, fingerprint, true) {
		return
	}

	originInfo := git.GetOriginInfo(ctx, s.runner, repoRoot, gitEnv)
	if err := s.ensureRepoRegistered(repoIdentity, repoRoot); err != nil {
		writeErr(http.StatusInternalServerError, errors.EInternal, "failed to register repo: "+err.Error(), "", req.ClientRequestID)
		return
	}
	if err := s.ensureRepoRecord(repoIdentity, repoRoot, originInfo); err != nil {
		writeErr(http.StatusInternalServerError, errors.EInternal, "failed to write repo.json: "+err.Error(), "", req.ClientRequestID)
		return
	}
	if err := s.checkTaskNameUniqueness(repoIdentity.RepoID, req.Name); err != nil {
		writeErr(http.StatusConflict, errors.ETaskNameExists, err.Error(), "use a different task name or inspect the existing task", req.ClientRequestID)
		return
	}
	if req.InvocationName != "" {
		if err := s.checkInvocationNameUniqueness(repoIdentity.RepoID, req.InvocationName); err != nil {
			writeErr(http.StatusConflict, errors.EInvocationNameExists, err.Error(), "use a different invocation name or wait for the existing invocation to complete", req.ClientRequestID)
			return
		}
	}

	taskID, err := core.NewRunID(s.clock())
	if err != nil {
		writeErr(http.StatusInternalServerError, errors.EInternal, "failed to generate task_id: "+err.Error(), "", req.ClientRequestID)
		return
	}
	if _, err := s.store.EnsureTaskDir(repoIdentity.RepoID, taskID); err != nil {
		writeErr(http.StatusInternalServerError, errors.GetCode(err), err.Error(), "", req.ClientRequestID)
		return
	}
	now := s.clock().UTC().Format(time.RFC3339)
	taskMeta := &store.TaskMeta{
		SchemaVersion:      store.SchemaVersion,
		TaskID:             taskID,
		Name:               req.Name,
		State:              store.TaskStateStarting,
		RepoID:             repoIdentity.RepoID,
		RepoRoot:           repoRoot,
		BaseBranch:         req.BaseBranch,
		CheckoutRoot:       execCtx.CheckoutRoot,
		ExecutionProfile:   execCtx.Profile,
		Mode:               store.RunnerMode(req.Mode),
		Runner:             req.Runner,
		ClientRequestID:    req.ClientRequestID,
		RequestFingerprint: fingerprint,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.store.WriteTaskMeta(repoIdentity.RepoID, taskID, taskMeta); err != nil {
		_ = s.store.RemoveTaskDir(repoIdentity.RepoID, taskID)
		writeErr(http.StatusInternalServerError, errors.GetCode(err), err.Error(), "", req.ClientRequestID)
		return
	}
	if err := s.appendTaskEvent(repoIdentity.RepoID, taskID, "agency.task_started", map[string]any{
		"client_request_id": req.ClientRequestID,
		"name":              req.Name,
		"base_branch":       req.BaseBranch,
		"checkout_root":     execCtx.CheckoutRoot,
		"execution_profile": execCtx.Profile,
		"mode":              req.Mode,
		"runner":            req.Runner,
	}); err != nil {
		_ = s.store.RemoveTaskDir(repoIdentity.RepoID, taskID)
		writeErr(http.StatusInternalServerError, errors.EPersistFailed, "failed to append task event: "+err.Error(), "", req.ClientRequestID)
		return
	}

	wtSvc := integrationworktree.NewService(s.store, s.runner, s.fsys, s.clock)
	wtCreate, err := wtSvc.Create(ctx, integrationworktree.CreateOpts{
		Name:             req.Name,
		RepoRoot:         repoRoot,
		RepoID:           repoIdentity.RepoID,
		BaseBranch:       req.BaseBranch,
		CheckoutRoot:     execCtx.CheckoutRoot,
		ExecutionProfile: execCtx.Profile,
		Env:              gitEnv,
	})
	if err != nil {
		fail := taskStartFailureFromError(http.StatusInternalServerError, errors.EWorktreeCreateFailed, err, "")
		s.markTaskFailed(repoIdentity.RepoID, taskID, "worktree_create", fail)
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, taskMeta)
		return
	}
	wtMeta, err := s.store.ReadIntegrationWorktreeMeta(repoIdentity.RepoID, wtCreate.WorktreeID)
	if err != nil {
		fail := taskStartFailureFromError(http.StatusInternalServerError, errors.EWorktreeBroken, err, "")
		s.markTaskFailed(repoIdentity.RepoID, taskID, "worktree_read", fail)
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, taskMeta)
		return
	}
	if err := s.store.UpdateIntegrationWorktreeMeta(repoIdentity.RepoID, wtCreate.WorktreeID, func(meta *store.IntegrationWorktreeMeta) {
		meta.TaskID = taskID
	}); err != nil {
		fail := taskStartFailureFromError(http.StatusInternalServerError, errors.EMetaWriteFailed, err, "")
		s.markTaskFailed(repoIdentity.RepoID, taskID, "worktree_task_link", fail)
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, taskMeta)
		return
	}
	if err := s.updateTaskWorktree(repoIdentity.RepoID, taskID, wtMeta, wtCreate); err != nil {
		fail := taskStartFailureFromError(http.StatusInternalServerError, errors.EMetaWriteFailed, err, "")
		s.markTaskFailed(repoIdentity.RepoID, taskID, "task_worktree_update", fail)
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, taskMeta)
		return
	}
	if err := s.appendTaskEvent(repoIdentity.RepoID, taskID, "agency.task_worktree_created", map[string]any{
		"worktree_id":   wtCreate.WorktreeID,
		"worktree_name": req.Name,
		"branch":        wtCreate.Branch,
		"tree_path":     wtCreate.TreePath,
	}); err != nil {
		fail := newTaskStartFailure(http.StatusInternalServerError, errors.EPersistFailed, "failed to append task event: "+err.Error(), "")
		s.markTaskFailed(repoIdentity.RepoID, taskID, "task_event_worktree_created", fail)
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, taskMeta)
		return
	}

	wtRecord := &store.IntegrationWorktreeRecord{
		WorktreeID:  wtCreate.WorktreeID,
		RepoID:      repoIdentity.RepoID,
		Name:        req.Name,
		Meta:        wtMeta,
		WorktreeDir: s.store.IntegrationWorktreeDir(repoIdentity.RepoID, wtCreate.WorktreeID),
	}

	var invMeta *store.InvocationMeta
	envKeys := sortedEnvKeys(requestEnv)
	if headless {
		invMeta, err = s.startTaskHeadlessInvocation(ctx, repoRoot, repoIdentity.RepoID, taskID, fingerprint, wtRecord, req, envKeys, gitEnv)
	} else {
		invMeta, err = s.startTaskHeadedInvocation(ctx, repoRoot, repoIdentity.RepoID, taskID, fingerprint, wtRecord, req, envKeys, gitEnv)
	}
	if err != nil {
		fail := normalizeTaskStartFailure(err)
		s.markTaskFailed(repoIdentity.RepoID, taskID, "invocation_start", fail)
		if latest, readErr := s.store.ReadTaskMeta(repoIdentity.RepoID, taskID); readErr == nil {
			taskMeta = latest
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, taskMeta)
		return
	}

	if err := s.appendTaskEvent(repoIdentity.RepoID, taskID, "agency.task_running", map[string]any{
		"invocation_id": invMeta.InvocationID,
		"worktree_id":   wtCreate.WorktreeID,
	}); err != nil {
		fail := newTaskStartFailure(http.StatusInternalServerError, errors.EPersistFailed, "failed to append task event: "+err.Error(), "")
		s.abortStartedTaskInvocation(repoIdentity.RepoID, invMeta, "task_event_running_failed")
		s.markTaskFailed(repoIdentity.RepoID, taskID, "task_event_running", fail)
		if latest, readErr := s.store.ReadTaskMeta(repoIdentity.RepoID, taskID); readErr == nil {
			taskMeta = latest
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, taskMeta)
		return
	}
	if err := s.markTaskRunning(repoIdentity.RepoID, taskID, invMeta); err != nil {
		fail := taskStartFailureFromError(http.StatusInternalServerError, errors.EMetaWriteFailed, err, "")
		s.abortStartedTaskInvocation(repoIdentity.RepoID, invMeta, "task_running_update_failed")
		s.markTaskFailed(repoIdentity.RepoID, taskID, "task_running_update", fail)
		if latest, readErr := s.store.ReadTaskMeta(repoIdentity.RepoID, taskID); readErr == nil {
			taskMeta = latest
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, taskMeta)
		return
	}

	taskMeta, _ = s.store.ReadTaskMeta(repoIdentity.RepoID, taskID)
	s.writeTaskStartSuccess(w, requestID, req.ClientRequestID, taskMeta, false)
}

func (s *Server) startTaskHeadlessInvocation(ctx context.Context, repoRoot, repoID, taskID, requestFingerprint string, wtRecord *store.IntegrationWorktreeRecord, req TaskStartRequest, envKeys []string, gitEnv map[string]string) (*store.InvocationMeta, error) {
	invSvc := invocation.NewService(s.store, s.runner, s.fsys, s.clock)
	createResult, err := invSvc.Create(ctx, invocation.CreateOpts{
		IntegrationWorktreeID:   wtRecord.WorktreeID,
		IntegrationWorktreeMeta: wtRecord.Meta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  req.Runner,
		Mode:                    store.RunnerModeHeadless,
		InvocationName:          req.InvocationName,
		CheckoutRoot:            req.CheckoutRoot,
		ExecutionProfile:        req.ExecutionProfile,
		NoIncludeUntracked:      req.NoIncludeUntracked,
		ClientRequestID:         req.ClientRequestID,
		RequestFingerprint:      requestFingerprint,
		Env:                     gitEnv,
	})
	if err != nil {
		return nil, taskStartFailureFromError(http.StatusInternalServerError, errors.EInvocationCreateFailed, err, "")
	}

	logsDir := s.store.InvocationLogsDir(repoID, createResult.InvocationID)
	if err := s.fsys.MkdirAll(logsDir, 0o700); err != nil {
		s.cleanupFailedInvocation(ctx, repoID, createResult, repoRoot, "start_failed", gitEnv)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInternal, "failed to create logs directory: "+err.Error(), "")
	}

	promptPath := s.store.InvocationPromptPath(repoID, createResult.InvocationID)
	if err := s.fsys.WriteFile(promptPath, []byte(req.Prompt), 0o600); err != nil {
		s.cleanupFailedInvocation(ctx, repoID, createResult, repoRoot, "start_failed", gitEnv)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInternal, "failed to write prompt file: "+err.Error(), "")
	}

	startReq := ControlPlaneStartRequest{
		RepoRoot:           repoRoot,
		WorktreeRef:        wtRecord.WorktreeID,
		Runner:             req.Runner,
		Prompt:             req.Prompt,
		InvocationName:     req.InvocationName,
		RunnerArgs:         req.RunnerArgs,
		Env:                req.Env,
		ClientRequestID:    req.ClientRequestID,
		NoIncludeUntracked: req.NoIncludeUntracked,
	}
	promptHash := sha256.Sum256([]byte(req.Prompt))
	promptSHA := hex.EncodeToString(promptHash[:])
	runnerArgs := slices.Clone(req.RunnerArgs)
	claim := func(pid, pgid int) error {
		return s.claimTaskHeadlessInvocationStart(repoID, createResult.InvocationID, taskID, req.Runner, pid, pgid, promptPath, promptSHA, runnerArgs, envKeys)
	}
	_, _, err = s.startRunner(ctx, repoID, createResult, repoRoot, wtRecord.WorktreeID, startReq, gitEnv, claim)
	if err != nil {
		s.cleanupFailedInvocation(ctx, repoID, createResult, repoRoot, "spawn_failed", gitEnv)
		code := errors.CodeOr(err, errors.ERunnerStartFailed)
		return nil, newTaskStartFailure(http.StatusInternalServerError, code, err.Error(), "")
	}

	meta, err := s.store.ReadInvocationMeta(repoID, createResult.InvocationID)
	if err != nil {
		return nil, taskStartFailureFromError(http.StatusInternalServerError, errors.EInvocationBroken, err, "")
	}
	return meta, nil
}

func (s *Server) startTaskHeadedInvocation(ctx context.Context, repoRoot, repoID, taskID, requestFingerprint string, wtRecord *store.IntegrationWorktreeRecord, req TaskStartRequest, envKeys []string, gitEnv map[string]string) (*store.InvocationMeta, error) {
	headedRunnerArgs, err := buildRunnerArgsForHeaded(req.Runner, req.RunnerArgs)
	if err != nil {
		return nil, taskStartFailureFromError(http.StatusBadRequest, errors.EInvocationInvalidMode, err, "")
	}

	invSvc := invocation.NewService(s.store, s.runner, s.fsys, s.clock)
	createResult, err := invSvc.Create(ctx, invocation.CreateOpts{
		IntegrationWorktreeID:   wtRecord.WorktreeID,
		IntegrationWorktreeMeta: wtRecord.Meta,
		RepoRoot:                repoRoot,
		RepoID:                  repoID,
		Runner:                  req.Runner,
		Mode:                    store.RunnerModeHeaded,
		InvocationName:          req.InvocationName,
		CheckoutRoot:            req.CheckoutRoot,
		ExecutionProfile:        req.ExecutionProfile,
		NoIncludeUntracked:      req.NoIncludeUntracked,
		ClientRequestID:         req.ClientRequestID,
		RequestFingerprint:      requestFingerprint,
		Env:                     gitEnv,
	})
	if err != nil {
		return nil, taskStartFailureFromError(http.StatusInternalServerError, errors.EInvocationCreateFailed, err, "")
	}

	userCfg, err := s.LoadUserConfig()
	if err != nil {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInvalidUserConfig, "failed to load user config: "+err.Error(), "run `agency config init`")
	}
	runnerCmd, err := config.ResolveRunnerCmd(s.runner, s.fsys, s.configDir, userCfg, req.Runner)
	if err != nil {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.ERunnerNotFound, "failed to resolve runner command: "+err.Error(), "ensure runner is installed and configured")
	}
	if err := s.installHeadedRunnerHooks(ctx, repoID, createResult.InvocationID, req.Runner, headedRunnerArgs, createResult.SandboxPath, gitEnv); err != nil {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to install headed runner hooks: "+err.Error(), "ensure sandbox hook files can be written")
	}

	sessionName := createResult.TmuxSession
	exists, err := s.tmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		s.recordInvocationWarning(repoID, createResult.InvocationID, "task_start_headed_tmux_has_session_failed", err.Error(), map[string]any{
			"session_name": sessionName,
		})
	} else if exists {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusConflict, errors.ETmuxSessionExists, "tmux session already exists: "+sessionName, "a tmux session with this name already exists; kill it with 'tmux kill-session -t "+sessionName+"'")
	}

	terminalLogPath, err := s.prepareWritableInvocationLogPath(repoID, createResult.InvocationID, InvocationLogKindTerminal)
	if err != nil {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to prepare terminal log: "+err.Error(), "")
	}
	terminalFile, err := os.OpenFile(terminalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to create terminal log: "+err.Error(), "")
	}
	_ = terminalFile.Close()

	argv := append([]string{runnerCmd}, headedRunnerArgs...)
	if err := s.tmuxClient.NewSession(ctx, sessionName, createResult.SandboxPath, argv, req.Env); err != nil {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to create tmux session: "+err.Error(), "ensure tmux is installed and working")
	}
	target := sessionName + ":0.0"
	if scrollback, err := s.tmuxClient.CaptureScrollback(ctx, target); err != nil {
		s.recordInvocationWarning(repoID, createResult.InvocationID, "task_start_headed_tmux_capture_failed", err.Error(), map[string]any{
			"target": target,
		})
	} else if scrollback != "" {
		if err := os.WriteFile(terminalLogPath, []byte(scrollback), 0o600); err != nil {
			_ = s.tmuxClient.KillSession(ctx, sessionName)
			s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
			return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to write initial terminal capture: "+err.Error(), "")
		}
	}
	if err := s.tmuxClient.PipePane(ctx, target, terminalLogPath); err != nil {
		_ = s.tmuxClient.KillSession(ctx, sessionName)
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to pipe tmux pane output: "+err.Error(), "ensure tmux pipe-pane is available")
	}

	runnerArgs := slices.Clone(req.RunnerArgs)
	if err := s.claimTaskHeadedInvocation(repoID, createResult.InvocationID, taskID, req.Runner, sessionName, runnerArgs, envKeys); err != nil {
		_ = s.tmuxClient.KillSession(ctx, sessionName)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInternal, "failed to update invocation meta: "+err.Error(), "")
	}

	streamLogPath := s.store.InvocationStreamLogPath(repoID, createResult.InvocationID)
	parser := stream.NewParser(createResult.InvocationID, req.Runner, s.clock)
	parser.SetInitialSeq(loadMaxStreamSeq(streamLogPath))
	checkpointsDir := s.store.InvocationDir(repoID, createResult.InvocationID)
	eventsPath := s.store.InvocationEventsPath(repoID, createResult.InvocationID)
	cpConfig := checkpoint.DefaultConfig()
	cpConfig.IncludeUntracked = !req.NoIncludeUntracked
	cpConfig.Env = gitEnv
	cpEngine := checkpoint.NewEngineWithWriter(
		createResult.InvocationID,
		repoID,
		createResult.SandboxPath,
		repoRoot,
		checkpointsDir,
		eventsPath,
		cpConfig,
		s.runner,
		s.fsys,
		s.clock,
		s.invocationEvents,
	)
	s.configureCheckpointIgnoredDirs(daemonOwnedContext(ctx), repoID, createResult.InvocationID, cpEngine, createResult.SandboxPath, cpConfig.Env)
	proc := &supervisedProcess{
		invocationID:          createResult.InvocationID,
		repoID:                repoID,
		integrationWorktreeID: wtRecord.WorktreeID,
		mode:                  "headed",
		tmuxSession:           sessionName,
		sandboxPath:           createResult.SandboxPath,
		runner:                req.Runner,
		repoRoot:              repoRoot,
		runnerArgs:            runnerArgs,
		env:                   copyStringMap(req.Env),
		noIncludeUntracked:    req.NoIncludeUntracked,
		parser:                parser,
		checkpointEngine:      cpEngine,
		done:                  make(chan struct{}),
	}
	s.attachCheckpointTriggers(repoID, createResult.InvocationID, parser, cpEngine)
	parser.SetFinalNotify(func(n stream.FinalNotification) {
		s.handleSuccessfulFinalNotification(proc, n)
	})
	parser.SetSessionStartNotify(func(n stream.SessionStartNotification) {
		proc.setResumeSessionID(n.SessionID)
	})
	s.replaceInvocationProcess(createResult.InvocationID, proc)
	s.supervisionWg.Add(2)
	go s.runOutputFlushLoop(proc)
	go s.runCheckpointLoop(proc)

	meta, err := s.store.ReadInvocationMeta(repoID, createResult.InvocationID)
	if err != nil {
		return nil, taskStartFailureFromError(http.StatusInternalServerError, errors.EInvocationBroken, err, "")
	}
	return meta, nil
}

func taskStartFingerprint(repoRoot, checkoutRoot string, req TaskStartRequest, requestEnv map[string]string) string {
	promptHash := sha256.Sum256([]byte(req.Prompt))
	payload, _ := json.Marshal(struct {
		RepoRoot           string   `json:"repo_root"`
		Name               string   `json:"name"`
		BaseBranch         string   `json:"base_branch"`
		CheckoutRoot       string   `json:"checkout_root"`
		ExecutionProfile   string   `json:"execution_profile"`
		Mode               string   `json:"mode"`
		Runner             string   `json:"runner"`
		EnvKeys            []string `json:"env_keys,omitempty"`
		PromptSHA256       string   `json:"prompt_sha256,omitempty"`
		InvocationName     string   `json:"invocation_name,omitempty"`
		RunnerArgs         []string `json:"runner_args,omitempty"`
		NoIncludeUntracked bool     `json:"no_include_untracked,omitempty"`
	}{
		RepoRoot:           repoRoot,
		Name:               req.Name,
		BaseBranch:         req.BaseBranch,
		CheckoutRoot:       checkoutRoot,
		ExecutionProfile:   req.ExecutionProfile,
		Mode:               req.Mode,
		Runner:             req.Runner,
		EnvKeys:            sortedEnvKeys(requestEnv),
		PromptSHA256:       hex.EncodeToString(promptHash[:]),
		InvocationName:     req.InvocationName,
		RunnerArgs:         slices.Clone(req.RunnerArgs),
		NoIncludeUntracked: req.NoIncludeUntracked,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (s *Server) findTaskByClientRequestID(repoID, clientRequestID, fingerprint string) (*store.TaskRecord, bool, bool) {
	records, err := s.store.ScanTasksForRepo(repoID)
	if err != nil {
		return nil, false, false
	}
	for i := range records {
		record := &records[i]
		if record.Meta == nil || record.Meta.ClientRequestID != clientRequestID {
			continue
		}
		return record, true, record.Meta.RequestFingerprint != fingerprint
	}
	return nil, false, false
}

func (s *Server) writeTaskStartIdempotencyResult(w http.ResponseWriter, requestID, clientRequestID, repoID, fingerprint string, finalizeIncomplete bool) bool {
	existing, exists, conflict := s.findTaskByClientRequestID(repoID, clientRequestID, fingerprint)
	if !exists {
		return false
	}
	if conflict {
		s.writeTaskStartError(w, http.StatusConflict, requestID, errors.ETaskFingerprintConflict, "client_request_id was already used for a different task start request", "retry with the original request or choose a new client_request_id", clientRequestID, nil)
		return true
	}
	if existing.Meta == nil || existing.Broken {
		s.writeTaskStartError(w, http.StatusConflict, requestID, errors.ETaskBroken, "task idempotency record exists but meta.json is unreadable", "inspect task state before retrying", clientRequestID, nil)
		return true
	}

	meta := existing.Meta
	switch meta.State {
	case store.TaskStateRunning, store.TaskStateArchived:
		s.writeTaskStartSuccess(w, requestID, clientRequestID, meta, true)
		return true
	case store.TaskStateFailed:
		if !finalizeIncomplete {
			return false
		}
		repaired, repairErr := s.repairTaskStartFromClaimedInvocation(repoID, meta, clientRequestID, fingerprint)
		if repairErr != nil {
			code := errors.CodeOr(repairErr, errors.EPersistFailed)
			s.writeTaskStartError(w, http.StatusInternalServerError, requestID, code, repairErr.Error(), "inspect task state before retrying", clientRequestID, meta)
			return true
		}
		if repaired != nil {
			s.writeTaskStartSuccess(w, requestID, clientRequestID, repaired, true)
			return true
		}
		code := errors.ETaskCreateFailed
		if meta.ErrorCode != "" {
			code = errors.Code(meta.ErrorCode)
		}
		message := meta.Error
		if message == "" {
			message = "task start request previously failed"
		}
		s.writeTaskStartError(w, http.StatusConflict, requestID, code, message, "inspect task state before retrying", clientRequestID, meta)
		return true
	case store.TaskStateStarting:
		if !finalizeIncomplete {
			return false
		}
		repaired, repairErr := s.repairTaskStartFromClaimedInvocation(repoID, meta, clientRequestID, fingerprint)
		if repairErr != nil {
			code := errors.CodeOr(repairErr, errors.EPersistFailed)
			s.writeTaskStartError(w, http.StatusInternalServerError, requestID, code, repairErr.Error(), "inspect task state before retrying", clientRequestID, meta)
			return true
		}
		if repaired != nil {
			s.writeTaskStartSuccess(w, requestID, clientRequestID, repaired, true)
			return true
		}
		fail := newTaskStartFailure(http.StatusConflict, errors.ETaskCreateFailed, "task start request was already accepted but no running invocation was recorded", "inspect task state before retrying")
		s.markTaskFailed(repoID, meta.TaskID, "task_start_incomplete", fail)
		if latest, err := s.store.ReadTaskMeta(repoID, meta.TaskID); err == nil {
			meta = latest
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, clientRequestID, meta)
		return true
	default:
		s.writeTaskStartError(w, http.StatusConflict, requestID, errors.ETaskCreateFailed, "task start idempotency record has unsupported state", "inspect task state before retrying", clientRequestID, meta)
		return true
	}
}

func (s *Server) repairTaskStartFromClaimedInvocation(repoID string, meta *store.TaskMeta, clientRequestID, fingerprint string) (*store.TaskMeta, error) {
	invMeta, ok, err := s.findClaimedTaskInvocation(repoID, meta.TaskID, clientRequestID, fingerprint)
	if err != nil || !ok {
		return nil, err
	}
	if err := s.appendTaskEventOnceByInvocationID(repoID, meta.TaskID, "agency.task_running", invMeta.InvocationID, map[string]any{
		"invocation_id": invMeta.InvocationID,
		"worktree_id":   invMeta.IntegrationWorktreeID,
	}); err != nil {
		return nil, err
	}
	if err := s.markTaskRunning(repoID, meta.TaskID, invMeta); err != nil {
		return nil, err
	}
	return s.store.ReadTaskMeta(repoID, meta.TaskID)
}

func (s *Server) findClaimedTaskInvocation(repoID, taskID, clientRequestID, fingerprint string) (*store.InvocationMeta, bool, error) {
	records, err := s.store.ScanInvocationsForRepo(repoID)
	if err != nil {
		return nil, false, err
	}
	for _, record := range records {
		if record.Broken || record.Meta == nil {
			continue
		}
		meta := record.Meta
		if meta.TaskID != taskID || meta.ClientRequestID != clientRequestID || meta.RequestFingerprint != fingerprint {
			continue
		}
		switch meta.Status {
		case store.InvocationStatusRunning:
			if strings.HasPrefix(meta.FailureReason, "task_") || strings.HasPrefix(meta.FailureReason, "retry_") {
				continue
			}
			return meta, true, nil
		case store.InvocationStatusFinished:
			return meta, true, nil
		case store.InvocationStatusFailed:
			if meta.ExitReason != store.ExitReasonStartFailed && !strings.HasPrefix(meta.FailureReason, "task_") && !strings.HasPrefix(meta.FailureReason, "retry_") {
				return meta, true, nil
			}
		}
	}
	return nil, false, nil
}

func (s *Server) checkTaskNameUniqueness(repoID, name string) error {
	records, err := s.store.ScanTasksForRepo(repoID)
	if err != nil {
		return fmt.Errorf("failed to scan tasks: %w", err)
	}
	for _, record := range records {
		if record.Broken || record.Meta == nil || record.Meta.State == store.TaskStateArchived {
			continue
		}
		if record.Name == name {
			return fmt.Errorf("task name '%s' is already used by task %s", name, record.TaskID)
		}
	}
	return nil
}

func normalizeTaskStartFailure(err error) taskStartFailure {
	var failure taskStartFailure
	if stderrors.As(err, &failure) {
		return failure
	}
	return taskStartFailureFromError(http.StatusInternalServerError, errors.EInternal, err, "")
}

func (s *Server) appendTaskEvent(repoID, taskID, kind string, data map[string]any) error {
	_, err := s.taskEvents.Append(s.store.TaskEventsPath(repoID, taskID), taskID, kind, data, eventlog.AppendOptions{})
	return err
}

func (s *Server) appendTaskEventOnceByInvocationID(repoID, taskID, kind, invocationID string, data map[string]any) error {
	_, err := s.taskEvents.Append(s.store.TaskEventsPath(repoID, taskID), taskID, kind, data, eventlog.AppendOptions{
		IdempotencyDataKey:   "invocation_id",
		IdempotencyDataValue: invocationID,
	})
	return err
}

func (s *Server) updateTaskWorktree(repoID, taskID string, wtMeta *store.IntegrationWorktreeMeta, wtCreate *integrationworktree.CreateResult) error {
	return s.store.UpdateTaskMeta(repoID, taskID, func(meta *store.TaskMeta) {
		meta.WorktreeID = wtCreate.WorktreeID
		meta.WorktreeName = wtMeta.Name
		meta.WorktreePath = wtCreate.TreePath
		meta.Branch = wtCreate.Branch
		meta.UpdatedAt = s.clock().UTC().Format(time.RFC3339)
	})
}

func (s *Server) markTaskRunning(repoID, taskID string, invMeta *store.InvocationMeta) error {
	return s.store.UpdateTaskMeta(repoID, taskID, func(meta *store.TaskMeta) {
		meta.State = store.TaskStateRunning
		meta.PrimaryInvocationID = invMeta.InvocationID
		meta.Mode = invMeta.Mode
		meta.Runner = invMeta.Runner
		meta.FailedPhase = ""
		meta.ErrorCode = ""
		meta.Error = ""
		meta.UpdatedAt = s.clock().UTC().Format(time.RFC3339)
	})
}

func (s *Server) claimTaskHeadlessInvocationStart(repoID, invocationID, taskID, runner string, pid, pgid int, promptPath, promptSHA string, runnerArgs, envKeys []string) error {
	now := s.nowRFC3339()
	daemonPID := os.Getpid()
	return s.store.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.Runner = runner
		meta.PID = &pid
		meta.PGID = &pgid
		meta.DaemonPID = &daemonPID
		meta.DaemonInstanceID = s.instanceID
		meta.ClaimedAt = now
		meta.LifecycleOwner = daemonLifecycleOwner
		meta.PromptPath = promptPath
		meta.PromptSHA256 = promptSHA
		meta.RunnerArgs = runnerArgs
		meta.CustomEnvKeys = envKeys
		meta.TaskID = taskID
		meta.FinishedAt = ""
		meta.ExitReason = ""
		meta.FailureReason = ""
		meta.ExitCode = nil
		meta.StopRequestedAt = ""
		meta.OrphanedAt = ""
		meta.Flags.NeedsAttention = false
		meta.Flags.Orphaned = false
	})
}

func (s *Server) claimTaskHeadedInvocation(repoID, invocationID, taskID, runner, sessionName string, runnerArgs, envKeys []string) error {
	now := s.nowRFC3339()
	daemonPID := os.Getpid()
	return s.store.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.Runner = runner
		meta.RunnerArgs = runnerArgs
		meta.CustomEnvKeys = envKeys
		meta.TmuxSession = sessionName
		meta.PID = nil
		meta.PGID = nil
		meta.DaemonPID = &daemonPID
		meta.DaemonInstanceID = s.instanceID
		meta.ClaimedAt = now
		meta.LifecycleOwner = daemonLifecycleOwner
		meta.TaskID = taskID
		meta.FinishedAt = ""
		meta.ExitReason = ""
		meta.FailureReason = ""
		meta.ExitCode = nil
		meta.StopRequestedAt = ""
		meta.OrphanedAt = ""
		meta.Flags.NeedsAttention = false
		meta.Flags.Orphaned = false
	})
}

func (s *Server) abortStartedTaskInvocation(repoID string, invMeta *store.InvocationMeta, failureReason string) {
	if invMeta == nil {
		return
	}
	if invMeta.Mode == store.RunnerModeHeaded {
		sessionName, ok := headedInvocationSessionName(invMeta)
		if !ok {
			s.recordInvocationWarning(repoID, invMeta.InvocationID, "task_abort_tmux_session_missing", "headed invocation is missing tmux_session", map[string]any{
				"invocation_id": invMeta.InvocationID,
			})
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.tmuxClient.KillSession(cleanupCtx, sessionName); err != nil && !tmux.IsNoSessionErr(err) {
			s.recordInvocationWarning(repoID, invMeta.InvocationID, "task_abort_tmux_kill_failed", err.Error(), map[string]any{
				"session_name": sessionName,
			})
			s.persistInvocationMeta(repoID, invMeta.InvocationID, func(meta *store.InvocationMeta) {
				meta.Flags.NeedsAttention = true
				meta.FailureReason = failureReason
			})
			return
		}
		s.failInvocationStart(repoID, invMeta.InvocationID, failureReason, true)
		s.clearInvocationProcess(invMeta.InvocationID)
		return
	}

	s.mu.RLock()
	proc, supervised := s.processes[invMeta.InvocationID]
	s.mu.RUnlock()
	if supervised && proc != nil {
		proc.exitReason.Store(store.ExitReasonKilled)
		proc.failureReason.Store(failureReason)
	}
	s.failInvocationStart(repoID, invMeta.InvocationID, failureReason, true)
	pgid := safeIntPtr(invMeta.PGID)
	if pgid <= 0 {
		pgid = safeIntPtr(invMeta.PID)
	}
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}

func (s *Server) markTaskFailed(repoID, taskID, phase string, failure taskStartFailure) {
	if err := s.store.UpdateTaskMeta(repoID, taskID, func(meta *store.TaskMeta) {
		meta.State = store.TaskStateFailed
		meta.FailedPhase = phase
		meta.ErrorCode = string(failure.code)
		meta.Error = failure.msg
		meta.UpdatedAt = s.clock().UTC().Format(time.RFC3339)
	}); err != nil {
		log.Printf("agencyd: persist failed task %s/%s: %v", repoID, taskID, err)
	}
	if err := s.appendTaskEvent(repoID, taskID, "agency.task_failed", map[string]any{
		"failed_phase": phase,
		"error_code":   string(failure.code),
		"error":        failure.msg,
	}); err != nil {
		log.Printf("agencyd: append task_failed event for %s/%s: %v", repoID, taskID, err)
	}
}

func (s *Server) writeTaskStartError(w http.ResponseWriter, status int, requestID string, code errors.Code, message, hint, clientRequestID string, meta *store.TaskMeta) {
	setRequestIDHeader(w, requestID)
	resp := TaskStartResponse{
		OK:              false,
		RequestID:       requestID,
		APIVersion:      APIVersion,
		BuildVersion:    daemonBuildVersion(),
		ClientRequestID: clientRequestID,
		ErrorCode:       string(code),
		Message:         message,
		Hint:            hint,
	}
	if meta != nil {
		resp.Partial = true
		resp.TaskID = meta.TaskID
		resp.TaskName = meta.Name
		resp.State = meta.State
		resp.RepoID = meta.RepoID
		resp.RepoName = s.repoName(meta.RepoID)
		resp.WorktreeID = meta.WorktreeID
		resp.WorktreeName = meta.WorktreeName
		resp.WorktreePath = meta.WorktreePath
		resp.Branch = meta.Branch
		resp.ExecutionProfile = meta.ExecutionProfile
		resp.CheckoutRoot = meta.CheckoutRoot
		resp.InvocationID = meta.PrimaryInvocationID
		resp.Mode = meta.Mode
		resp.Runner = meta.Runner
		resp.FailedPhase = meta.FailedPhase
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) writeTaskStartSuccess(w http.ResponseWriter, requestID, clientRequestID string, meta *store.TaskMeta, duplicate bool) {
	setRequestIDHeader(w, requestID)
	resp := TaskStartResponse{
		OK:               true,
		RequestID:        requestID,
		APIVersion:       APIVersion,
		BuildVersion:     daemonBuildVersion(),
		ClientRequestID:  clientRequestID,
		Duplicate:        duplicate,
		TaskID:           meta.TaskID,
		TaskName:         meta.Name,
		State:            meta.State,
		RepoID:           meta.RepoID,
		RepoName:         s.repoName(meta.RepoID),
		WorktreeID:       meta.WorktreeID,
		WorktreeName:     meta.WorktreeName,
		WorktreePath:     meta.WorktreePath,
		Branch:           meta.Branch,
		ExecutionProfile: meta.ExecutionProfile,
		CheckoutRoot:     meta.CheckoutRoot,
		InvocationID:     meta.PrimaryInvocationID,
		Mode:             meta.Mode,
		Runner:           meta.Runner,
	}
	if meta.PrimaryInvocationID != "" {
		if invMeta, err := s.store.ReadInvocationMeta(meta.RepoID, meta.PrimaryInvocationID); err == nil {
			resp.SandboxPath = invMeta.SandboxPath
			resp.TmuxSession = invMeta.TmuxSession
			resp.DaemonInstanceID = s.instanceID
			resp.CustomEnvKeys = slices.Clone(invMeta.CustomEnvKeys)
			resp.LogPaths = s.invocationLogPaths(meta.RepoID, invMeta.InvocationID)
			if invMeta.PID != nil {
				resp.PID = *invMeta.PID
			}
			if invMeta.PGID != nil {
				resp.PGID = *invMeta.PGID
			}
		}
	}
	s.writeJSON(w, http.StatusOK, resp)
}
