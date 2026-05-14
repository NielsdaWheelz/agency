package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/daemon/taskevents"
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

func taskStartFailureFromError(status int, fallback errors.Code, err error, hint string) taskStartFailure {
	code := errors.GetCode(err)
	if code == "" {
		code = fallback
	}
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
		writeErr(http.StatusBadRequest, errors.EInvalidArgument, "invalid request body: "+err.Error(), "", "")
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
		writeErr(http.StatusBadRequest, errors.EInvalidArgument, "client_request_id is required", "provide a UUID for idempotency", "")
		return
	}
	if req.RepoRoot == "" {
		writeErr(http.StatusBadRequest, errors.EInvalidArgument, "repo_root is required", "", req.ClientRequestID)
		return
	}
	if req.Name == "" {
		writeErr(http.StatusBadRequest, errors.EInvalidArgument, "name is required", "", req.ClientRequestID)
		return
	}
	if err := core.ValidateName(req.Name); err != nil {
		writeErr(http.StatusBadRequest, errors.EInvalidName, "invalid task name: "+err.Error(), "names must be 2-40 chars, lowercase alphanumeric + hyphens", req.ClientRequestID)
		return
	}
	if req.BaseBranch == "" {
		writeErr(http.StatusBadRequest, errors.EInvalidArgument, "base_branch is required", "", req.ClientRequestID)
		return
	}
	if req.Runner == "" {
		writeErr(http.StatusBadRequest, errors.EInvalidArgument, "runner is required", "", req.ClientRequestID)
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
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerArgConflict
		}
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
			code := errors.GetCode(err)
			if code == "" {
				code = errors.EInternal
			}
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
		code := errors.GetCode(err)
		if code == "" {
			code = errors.EInternal
		}
		writeErr(http.StatusBadRequest, code, apiErrorMessage(err), "", req.ClientRequestID)
		return
	}
	req.ExecutionProfile = execCtx.Profile
	req.CheckoutRoot = execCtx.CheckoutRoot
	gitEnv := prSyncNonInteractiveEnv(execCtx.ProfileEnv)
	req.Env = envForLaunch(execCtx.ProfileEnv, requestEnv)

	fingerprint := taskStartFingerprint(repoRoot, execCtx.CheckoutRoot, req, requestEnv)
	if existing, exists, conflict := s.findTaskByClientRequestID(repoIdentity.RepoID, req.ClientRequestID, fingerprint); exists {
		if conflict {
			writeErr(http.StatusConflict, errors.ETaskFingerprintConflict, "client_request_id was already used for a different task start request", "retry with the original request or choose a new client_request_id", req.ClientRequestID)
			return
		}
		if existing.Meta == nil || existing.Broken {
			writeErr(http.StatusConflict, errors.ETaskBroken, "task idempotency record exists but meta.json is unreadable", "inspect task state before retrying", req.ClientRequestID)
			return
		}
		s.writeTaskStartSuccess(w, requestID, req.ClientRequestID, existing.Meta, true)
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

	if existing, exists, conflict := s.findTaskByClientRequestID(repoIdentity.RepoID, req.ClientRequestID, fingerprint); exists {
		if conflict {
			writeErr(http.StatusConflict, errors.ETaskFingerprintConflict, "client_request_id was already used for a different task start request", "retry with the original request or choose a new client_request_id", req.ClientRequestID)
			return
		}
		if existing.Meta == nil || existing.Broken {
			writeErr(http.StatusConflict, errors.ETaskBroken, "task idempotency record exists but meta.json is unreadable", "inspect task state before retrying", req.ClientRequestID)
			return
		}
		s.writeTaskStartSuccess(w, requestID, req.ClientRequestID, existing.Meta, true)
		return
	}

	originInfo := git.GetOriginInfo(ctx, s.Runner, repoRoot, gitEnv)
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

	taskID, err := core.NewRunID(s.Clock())
	if err != nil {
		writeErr(http.StatusInternalServerError, errors.EInternal, "failed to generate task_id: "+err.Error(), "", req.ClientRequestID)
		return
	}
	if _, err := s.Store.EnsureTaskDir(repoIdentity.RepoID, taskID); err != nil {
		writeErr(http.StatusInternalServerError, errors.GetCode(err), err.Error(), "", req.ClientRequestID)
		return
	}
	now := s.Clock().UTC().Format(time.RFC3339)
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
	if err := s.Store.WriteTaskMeta(repoIdentity.RepoID, taskID, taskMeta); err != nil {
		_ = s.Store.RemoveTaskDir(repoIdentity.RepoID, taskID)
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
		_ = s.Store.RemoveTaskDir(repoIdentity.RepoID, taskID)
		writeErr(http.StatusInternalServerError, errors.EPersistFailed, "failed to append task event: "+err.Error(), "", req.ClientRequestID)
		return
	}

	wtSvc := integrationworktree.NewService(s.Store, s.Runner, s.FS, s.Clock)
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
	wtMeta, err := s.Store.ReadIntegrationWorktreeMeta(repoIdentity.RepoID, wtCreate.WorktreeID)
	if err != nil {
		fail := taskStartFailureFromError(http.StatusInternalServerError, errors.EWorktreeBroken, err, "")
		s.markTaskFailed(repoIdentity.RepoID, taskID, "worktree_read", fail)
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, taskMeta)
		return
	}
	if err := s.Store.UpdateIntegrationWorktreeMeta(repoIdentity.RepoID, wtCreate.WorktreeID, func(meta *store.IntegrationWorktreeMeta) {
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
		WorktreeDir: s.Store.IntegrationWorktreeDir(repoIdentity.RepoID, wtCreate.WorktreeID),
	}

	var invMeta *store.InvocationMeta
	envKeys := sortedEnvKeys(requestEnv)
	if headless {
		invMeta, err = s.startTaskHeadlessInvocation(ctx, repoRoot, repoIdentity.RepoID, taskID, wtRecord, req, envKeys, gitEnv)
	} else {
		invMeta, err = s.startTaskHeadedInvocation(ctx, repoRoot, repoIdentity.RepoID, taskID, wtRecord, req, envKeys, gitEnv)
	}
	if err != nil {
		fail := normalizeTaskStartFailure(err)
		s.markTaskFailed(repoIdentity.RepoID, taskID, "invocation_start", fail)
		if latest, readErr := s.Store.ReadTaskMeta(repoIdentity.RepoID, taskID); readErr == nil {
			taskMeta = latest
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, taskMeta)
		return
	}

	if err := s.markTaskRunning(repoIdentity.RepoID, taskID, invMeta); err != nil {
		fail := taskStartFailureFromError(http.StatusInternalServerError, errors.EMetaWriteFailed, err, "")
		s.markTaskFailed(repoIdentity.RepoID, taskID, "task_running_update", fail)
		if latest, readErr := s.Store.ReadTaskMeta(repoIdentity.RepoID, taskID); readErr == nil {
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
		s.markTaskFailed(repoIdentity.RepoID, taskID, "task_event_running", fail)
		if latest, readErr := s.Store.ReadTaskMeta(repoIdentity.RepoID, taskID); readErr == nil {
			taskMeta = latest
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, taskMeta)
		return
	}

	taskMeta, _ = s.Store.ReadTaskMeta(repoIdentity.RepoID, taskID)
	s.writeTaskStartSuccess(w, requestID, req.ClientRequestID, taskMeta, false)
}

func (s *Server) startTaskHeadlessInvocation(ctx context.Context, repoRoot, repoID, taskID string, wtRecord *store.IntegrationWorktreeRecord, req TaskStartRequest, envKeys []string, gitEnv map[string]string) (*store.InvocationMeta, error) {
	invSvc := invocation.NewService(s.Store, s.Runner, s.FS, s.Clock)
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
		Env:                     gitEnv,
	})
	if err != nil {
		return nil, taskStartFailureFromError(http.StatusInternalServerError, errors.EInvocationCreateFailed, err, "")
	}

	logsDir := s.Store.InvocationLogsDir(repoID, createResult.InvocationID)
	if err := s.FS.MkdirAll(logsDir, 0o700); err != nil {
		s.cleanupFailedInvocation(ctx, repoID, createResult, repoRoot, "start_failed", gitEnv)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInternal, "failed to create logs directory: "+err.Error(), "")
	}

	promptPath := s.Store.InvocationPromptPath(repoID, createResult.InvocationID)
	if err := s.FS.WriteFile(promptPath, []byte(req.Prompt), 0o600); err != nil {
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
	pid, pgid, err := s.startRunner(ctx, repoID, createResult, repoRoot, wtRecord.WorktreeID, startReq, gitEnv)
	if err != nil {
		s.cleanupFailedInvocation(ctx, repoID, createResult, repoRoot, "spawn_failed", gitEnv)
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerStartFailed
		}
		return nil, newTaskStartFailure(http.StatusInternalServerError, code, err.Error(), "")
	}

	promptHash := sha256.Sum256([]byte(req.Prompt))
	promptSHA := hex.EncodeToString(promptHash[:])
	runnerArgs := append([]string(nil), req.RunnerArgs...)
	if err := s.claimHeadlessInvocationStart(repoID, createResult.InvocationID, req.Runner, pid, pgid, promptPath, promptSHA, runnerArgs, envKeys); err != nil {
		return nil, taskStartFailureFromError(http.StatusInternalServerError, errors.EMetaWriteFailed, err, "")
	}
	if err := s.Store.UpdateInvocationMeta(repoID, createResult.InvocationID, func(meta *store.InvocationMeta) {
		meta.TaskID = taskID
	}); err != nil {
		return nil, taskStartFailureFromError(http.StatusInternalServerError, errors.EMetaWriteFailed, err, "")
	}

	meta, err := s.Store.ReadInvocationMeta(repoID, createResult.InvocationID)
	if err != nil {
		return nil, taskStartFailureFromError(http.StatusInternalServerError, errors.EInvocationBroken, err, "")
	}
	return meta, nil
}

func (s *Server) startTaskHeadedInvocation(ctx context.Context, repoRoot, repoID, taskID string, wtRecord *store.IntegrationWorktreeRecord, req TaskStartRequest, envKeys []string, gitEnv map[string]string) (*store.InvocationMeta, error) {
	headedRunnerArgs, err := buildRunnerArgsForHeaded(req.Runner, req.RunnerArgs)
	if err != nil {
		return nil, taskStartFailureFromError(http.StatusBadRequest, errors.EInvocationInvalidMode, err, "")
	}

	invSvc := invocation.NewService(s.Store, s.Runner, s.FS, s.Clock)
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
	runnerCmd, err := config.ResolveRunnerCmd(s.Runner, s.FS, s.ConfigDir, userCfg, req.Runner)
	if err != nil {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.ERunnerNotFound, "failed to resolve runner command: "+err.Error(), "ensure runner is installed and configured")
	}
	if err := s.installHeadedRunnerHooks(ctx, repoID, createResult.InvocationID, req.Runner, headedRunnerArgs, createResult.SandboxPath, gitEnv); err != nil {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to install headed runner hooks: "+err.Error(), "ensure sandbox hook files can be written")
	}

	sessionName := tmux.SessionName(createResult.InvocationID)
	exists, err := s.TmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		s.recordInvocationWarning(repoID, createResult.InvocationID, "task_start_headed_tmux_has_session_failed", err.Error(), map[string]any{
			"session_name": sessionName,
		})
	} else if exists {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusConflict, errors.ETmuxSessionExists, "tmux session already exists: "+sessionName, "a tmux session with this name already exists; kill it with 'tmux kill-session -t "+sessionName+"'")
	}

	terminalLogPath, err := s.prepareWritableInvocationLogPath(repoID, createResult.InvocationID, "terminal")
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
	if err := s.TmuxClient.NewSession(ctx, sessionName, createResult.SandboxPath, argv, req.Env); err != nil {
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to create tmux session: "+err.Error(), "ensure tmux is installed and working")
	}
	target := tmux.SessionTarget(createResult.InvocationID)
	if scrollback, err := s.TmuxClient.CaptureScrollback(ctx, target); err != nil {
		s.recordInvocationWarning(repoID, createResult.InvocationID, "task_start_headed_tmux_capture_failed", err.Error(), map[string]any{
			"target": target,
		})
	} else if scrollback != "" {
		if err := os.WriteFile(terminalLogPath, []byte(scrollback), 0o600); err != nil {
			_ = s.TmuxClient.KillSession(ctx, sessionName)
			s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
			return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to write initial terminal capture: "+err.Error(), "")
		}
	}
	if err := s.TmuxClient.PipePane(ctx, target, terminalLogPath); err != nil {
		_ = s.TmuxClient.KillSession(ctx, sessionName)
		s.failInvocationStart(repoID, createResult.InvocationID, "start_failed", true)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInvocationStartFailed, "failed to pipe tmux pane output: "+err.Error(), "ensure tmux pipe-pane is available")
	}

	runnerArgs := append([]string(nil), req.RunnerArgs...)
	if err := s.claimHeadedInvocation(repoID, createResult.InvocationID, req.Runner, sessionName, runnerArgs, envKeys); err != nil {
		_ = s.TmuxClient.KillSession(ctx, sessionName)
		return nil, newTaskStartFailure(http.StatusInternalServerError, errors.EInternal, "failed to update invocation meta: "+err.Error(), "")
	}
	if err := s.Store.UpdateInvocationMeta(repoID, createResult.InvocationID, func(meta *store.InvocationMeta) {
		meta.TaskID = taskID
	}); err != nil {
		return nil, taskStartFailureFromError(http.StatusInternalServerError, errors.EMetaWriteFailed, err, "")
	}

	streamLogPath := s.Store.InvocationStreamLogPath(repoID, createResult.InvocationID)
	parser := stream.NewParser(createResult.InvocationID, req.Runner, s.Clock)
	parser.SetInitialSeq(loadMaxStreamSeq(streamLogPath))
	checkpointsDir := s.Store.InvocationDir(repoID, createResult.InvocationID)
	eventsPath := s.Store.InvocationEventsPath(repoID, createResult.InvocationID)
	cpConfig := checkpoint.DefaultConfig()
	cpConfig.IncludeUntracked = !req.NoIncludeUntracked
	cpConfig.Env = gitEnv
	if s.CheckpointDebounceOverride != nil {
		cpConfig.DebounceInterval = *s.CheckpointDebounceOverride
		cpConfig.DriftInterval = *s.CheckpointDebounceOverride
	}
	cpEngine := checkpoint.NewEngineWithWriter(
		createResult.InvocationID,
		repoID,
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
		RepoID:                repoID,
		IntegrationWorktreeID: wtRecord.WorktreeID,
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
			s.recordInvocationWarning(repoID, createResult.InvocationID, "checkpoint_trigger_dropped", "checkpoint trigger queue full; dropped semantic trigger", map[string]any{"seq": n.Seq, "tool_name": n.ToolName})
		}
	})
	parser.SetFinalNotify(func(n stream.FinalNotification) {
		s.handleSuccessfulFinalNotification(proc, n)
	})
	parser.SetSessionStartNotify(func(n stream.SessionStartNotification) {
		proc.SetResumeSessionID(n.SessionID)
	})
	s.replaceInvocationProcess(createResult.InvocationID, proc)
	go s.runOutputFlushLoop(proc)
	go s.runCheckpointLoop(proc)

	meta, err := s.Store.ReadInvocationMeta(repoID, createResult.InvocationID)
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
		RunnerArgs:         append([]string(nil), req.RunnerArgs...),
		NoIncludeUntracked: req.NoIncludeUntracked,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (s *Server) findTaskByClientRequestID(repoID, clientRequestID, fingerprint string) (*store.TaskRecord, bool, bool) {
	records, err := store.ScanTasksForRepo(s.Store.DataDir, repoID)
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

func (s *Server) checkTaskNameUniqueness(repoID, name string) error {
	records, err := store.ScanTasksForRepo(s.Store.DataDir, repoID)
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
	if s.TaskEvents == nil {
		s.TaskEvents = taskevents.NewWriter(func() time.Time {
			return s.Clock()
		})
	}
	_, err := s.TaskEvents.Append(s.Store.TaskEventsPath(repoID, taskID), taskID, kind, data, taskevents.AppendOptions{})
	return err
}

func (s *Server) updateTaskWorktree(repoID, taskID string, wtMeta *store.IntegrationWorktreeMeta, wtCreate *integrationworktree.CreateResult) error {
	return s.Store.UpdateTaskMeta(repoID, taskID, func(meta *store.TaskMeta) {
		meta.WorktreeID = wtCreate.WorktreeID
		meta.WorktreeName = wtMeta.Name
		meta.WorktreePath = wtCreate.TreePath
		meta.Branch = wtCreate.Branch
		meta.UpdatedAt = s.Clock().UTC().Format(time.RFC3339)
	})
}

func (s *Server) markTaskRunning(repoID, taskID string, invMeta *store.InvocationMeta) error {
	return s.Store.UpdateTaskMeta(repoID, taskID, func(meta *store.TaskMeta) {
		meta.State = store.TaskStateRunning
		meta.PrimaryInvocationID = invMeta.InvocationID
		meta.Mode = invMeta.Mode
		meta.Runner = invMeta.Runner
		meta.FailedPhase = ""
		meta.ErrorCode = ""
		meta.Error = ""
		meta.UpdatedAt = s.Clock().UTC().Format(time.RFC3339)
	})
}

func (s *Server) markTaskFailed(repoID, taskID, phase string, failure taskStartFailure) {
	_ = s.Store.UpdateTaskMeta(repoID, taskID, func(meta *store.TaskMeta) {
		meta.State = store.TaskStateFailed
		meta.FailedPhase = phase
		meta.ErrorCode = string(failure.code)
		meta.Error = failure.msg
		meta.UpdatedAt = s.Clock().UTC().Format(time.RFC3339)
	})
	_ = s.appendTaskEvent(repoID, taskID, "agency.task_failed", map[string]any{
		"failed_phase": phase,
		"error_code":   string(failure.code),
		"error":        failure.msg,
	})
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
		if invMeta, err := s.Store.ReadInvocationMeta(meta.RepoID, meta.PrimaryInvocationID); err == nil {
			resp.SandboxPath = invMeta.SandboxPath
			resp.TmuxSession = invMeta.TmuxSession
			resp.DaemonInstanceID = s.InstanceID
			resp.CustomEnvKeys = append([]string(nil), invMeta.CustomEnvKeys...)
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
