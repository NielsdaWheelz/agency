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
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	agencylock "github.com/NielsdaWheelz/agency/internal/lock"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

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
	if fail := normalizeAndValidateTaskStartRequest(&req); fail != nil {
		writeErr(fail.status, fail.code, fail.msg, fail.hint, req.ClientRequestID)
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
	gitEnv := withNonInteractiveEnv(execCtx.ProfileEnv)
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
	now := s.nowRFC3339()
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

	finalMeta, phase, fail := s.executeTaskStartAfterCreated(ctx, req, repoRoot, repoIdentity.RepoID, taskID, fingerprint, execCtx, gitEnv, requestEnv)
	if fail != nil {
		s.markTaskFailed(repoIdentity.RepoID, taskID, phase, *fail)
		latestMeta := taskMeta
		if latest, err := s.store.ReadTaskMeta(repoIdentity.RepoID, taskID); err == nil {
			latestMeta = latest
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, latestMeta)
		return
	}

	s.writeTaskStartSuccess(w, requestID, req.ClientRequestID, finalMeta, false)
}

func normalizeAndValidateTaskStartRequest(req *TaskStartRequest) *startFailure {
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
		f := newStartFailure(http.StatusBadRequest, errors.EInvalidArgument, "agency_config_path must be absolute", "")
		return &f
	}
	if req.ClientRequestID == "" {
		f := newStartFailure(http.StatusBadRequest, errors.EInvalidRequest, "client_request_id is required", "provide a UUID for idempotency")
		return &f
	}
	if req.RepoRoot == "" {
		f := newStartFailure(http.StatusBadRequest, errors.EInvalidRequest, "repo_root is required", "")
		return &f
	}
	if req.Name == "" {
		f := newStartFailure(http.StatusBadRequest, errors.EInvalidRequest, "name is required", "")
		return &f
	}
	if err := core.ValidateName(req.Name); err != nil {
		f := newStartFailure(http.StatusBadRequest, errors.EInvalidName, "invalid task name: "+err.Error(), "names must be 2-40 chars, lowercase alphanumeric + hyphens")
		return &f
	}
	if req.BaseBranch == "" {
		f := newStartFailure(http.StatusBadRequest, errors.EInvalidRequest, "base_branch is required", "")
		return &f
	}
	if req.Runner == "" {
		f := newStartFailure(http.StatusBadRequest, errors.EInvalidRequest, "runner is required", "")
		return &f
	}

	headless := req.Mode == string(store.RunnerModeHeadless)
	switch req.Mode {
	case string(store.RunnerModeHeadless):
		if req.Prompt == "" {
			f := newStartFailure(http.StatusBadRequest, errors.EPromptRequired, "prompt is required for headless task", "")
			return &f
		}
		if len(req.Prompt) > MaxPromptSize {
			f := newStartFailure(http.StatusBadRequest, errors.EPromptTooLarge, fmt.Sprintf("prompt exceeds maximum size of %d bytes (got %d)", MaxPromptSize, len(req.Prompt)), "reduce prompt size or split into smaller chunks")
			return &f
		}
	case string(store.RunnerModeHeaded):
		if req.Prompt != "" {
			f := newStartFailure(http.StatusBadRequest, errors.EUsage, "headed task start does not accept a prompt", "omit --prompt/--prompt-file or use --mode headless")
			return &f
		}
	default:
		f := newStartFailure(http.StatusBadRequest, errors.EInvalidArgument, "mode must be headless or headed", "")
		return &f
	}

	canonicalRunner, err := validateControlPlaneStartRunner(req.Runner, req.RunnerArgs, headless)
	if err != nil {
		code := errors.CodeOr(err, errors.ERunnerArgConflict)
		hint := "remove reserved flags from runner_args"
		if code == errors.ERunnerNotFound {
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		}
		f := newStartFailure(http.StatusBadRequest, code, err.Error(), hint)
		return &f
	}
	req.Runner = canonicalRunner
	if !headless {
		if _, err := buildRunnerArgsForHeaded(req.Runner, req.RunnerArgs); err != nil {
			code := errors.CodeOr(err, errors.EInternal)
			status := http.StatusInternalServerError
			if code == errors.ERunnerNotFound || code == errors.EInvocationInvalidMode {
				status = http.StatusBadRequest
			}
			f := newStartFailure(status, code, err.Error(), "")
			return &f
		}
	}
	if err := validateControlPlaneStartInvocationName(req.InvocationName); err != nil {
		f := newStartFailure(http.StatusBadRequest, errors.EInvalidName, "invalid invocation name: "+err.Error(), "names must be 2-40 chars, lowercase alphanumeric + hyphens")
		return &f
	}
	return nil
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
		return nil, startFailureFromError(http.StatusInternalServerError, errors.EInvocationCreateFailed, err, "")
	}

	meta, fail := s.finishHeadlessInvocationStart(ctx, repoRoot, repoID, taskID, wtRecord, createResult, headlessInvocationStartParams{
		runner:             req.Runner,
		runnerArgs:         req.RunnerArgs,
		prompt:             req.Prompt,
		invocationName:     req.InvocationName,
		env:                req.Env,
		envKeys:            envKeys,
		gitEnv:             gitEnv,
		noIncludeUntracked: req.NoIncludeUntracked,
		clientRequestID:    req.ClientRequestID,
	})
	if fail != nil {
		return nil, *fail
	}
	return meta, nil
}

func (s *Server) startTaskHeadedInvocation(ctx context.Context, repoRoot, repoID, taskID, requestFingerprint string, wtRecord *store.IntegrationWorktreeRecord, req TaskStartRequest, envKeys []string, gitEnv map[string]string) (*store.InvocationMeta, error) {
	headedRunnerArgs, err := buildRunnerArgsForHeaded(req.Runner, req.RunnerArgs)
	if err != nil {
		return nil, startFailureFromError(http.StatusBadRequest, errors.EInvocationInvalidMode, err, "")
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
		return nil, startFailureFromError(http.StatusInternalServerError, errors.EInvocationCreateFailed, err, "")
	}

	meta, fail := s.finishHeadedInvocationStart(ctx, repoRoot, repoID, taskID, wtRecord, createResult, headedInvocationStartParams{
		runner:             req.Runner,
		runnerArgs:         slices.Clone(req.RunnerArgs),
		headedRunnerArgs:   headedRunnerArgs,
		launchEnv:          req.Env,
		envKeys:            envKeys,
		gitEnv:             gitEnv,
		noIncludeUntracked: req.NoIncludeUntracked,
	})
	if fail != nil {
		return nil, *fail
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
		if s.attemptTaskStartRepair(w, requestID, clientRequestID, repoID, fingerprint, meta) {
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
		if s.attemptTaskStartRepair(w, requestID, clientRequestID, repoID, fingerprint, meta) {
			return true
		}
		fail := newStartFailure(http.StatusConflict, errors.ETaskCreateFailed, "task start request was already accepted but no running invocation was recorded", "inspect task state before retrying")
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

// attemptTaskStartRepair tries to recover an incomplete task start by finding
// its claimed invocation and advancing the task to running. Writes a success
// response when repair succeeds or an error response when repair itself fails;
// returns false (without writing anything) when no repairable invocation
// exists, letting the caller fall through to a state-specific fallback.
func (s *Server) attemptTaskStartRepair(w http.ResponseWriter, requestID, clientRequestID, repoID, fingerprint string, meta *store.TaskMeta) bool {
	repaired, err := s.repairTaskStartFromClaimedInvocation(repoID, meta, clientRequestID, fingerprint)
	if err != nil {
		code := errors.CodeOr(err, errors.EPersistFailed)
		s.writeTaskStartError(w, http.StatusInternalServerError, requestID, code, err.Error(), "inspect task state before retrying", clientRequestID, meta)
		return true
	}
	if repaired != nil {
		s.writeTaskStartSuccess(w, requestID, clientRequestID, repaired, true)
		return true
	}
	return false
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
	return s.markTaskRunning(repoID, meta.TaskID, invMeta)
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
	_, err := s.store.UpdateTaskMeta(repoID, taskID, func(meta *store.TaskMeta) {
		meta.WorktreeID = wtCreate.WorktreeID
		meta.WorktreeName = wtMeta.Name
		meta.WorktreePath = wtCreate.TreePath
		meta.Branch = wtCreate.Branch
		meta.UpdatedAt = s.nowRFC3339()
	})
	return err
}

func (s *Server) markTaskRunning(repoID, taskID string, invMeta *store.InvocationMeta) (*store.TaskMeta, error) {
	return s.store.UpdateTaskMeta(repoID, taskID, func(meta *store.TaskMeta) {
		meta.State = store.TaskStateRunning
		meta.PrimaryInvocationID = invMeta.InvocationID
		meta.Mode = invMeta.Mode
		meta.Runner = invMeta.Runner
		meta.FailedPhase = ""
		meta.ErrorCode = ""
		meta.Error = ""
		meta.UpdatedAt = s.nowRFC3339()
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

func (s *Server) markTaskFailed(repoID, taskID, phase string, failure startFailure) {
	if _, err := s.store.UpdateTaskMeta(repoID, taskID, func(meta *store.TaskMeta) {
		meta.State = store.TaskStateFailed
		meta.FailedPhase = phase
		meta.ErrorCode = string(failure.code)
		meta.Error = failure.msg
		meta.UpdatedAt = s.nowRFC3339()
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
