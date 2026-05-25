package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Server) handleTaskArchive(w http.ResponseWriter, r *http.Request, taskRef string) {
	requestID := getOrCreateRequestID(r)
	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.EInvalidRequest, "repo_id query parameter is required", "pass ?repo_id=<repo_id>", "", nil)
		return
	}
	record, err := s.resolveTaskRecord(repoID, taskRef)
	if err != nil {
		s.writeTaskStartResolveError(w, requestID, err, "")
		return
	}
	if record.Broken || record.Meta == nil {
		s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.ETaskBroken, "task exists but meta.json is unreadable", "inspect task state before retrying", "", nil)
		return
	}

	meta, err := s.store.UpdateTaskMeta(repoID, record.TaskID, func(meta *store.TaskMeta) {
		meta.State = store.TaskStateArchived
		meta.UpdatedAt = s.nowRFC3339()
	})
	if err != nil {
		s.writeTaskStartError(w, http.StatusInternalServerError, requestID, errors.GetCode(err), err.Error(), "", "", record.Meta)
		return
	}
	if err := s.appendTaskEvent(repoID, record.TaskID, "agency.task_archived", map[string]any{}); err != nil {
		s.writeTaskStartError(w, http.StatusInternalServerError, requestID, errors.EPersistFailed, "failed to append task event: "+err.Error(), "", "", record.Meta)
		return
	}
	s.writeTaskStartSuccess(w, requestID, meta.ClientRequestID, meta, false)
}

func (s *Server) handleTaskRetry(w http.ResponseWriter, r *http.Request, taskRef string) {
	ctx := r.Context()
	requestID := getOrCreateRequestID(r)
	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.EInvalidRequest, "repo_id query parameter is required", "pass ?repo_id=<repo_id>", "", nil)
		return
	}
	var req TaskRetryRequest
	if err := decodeStrictJSON(r.Body, &req); err != nil {
		s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.EInvalidRequest, strictJSONDecodeErrorMessage(err), "", req.ClientRequestID, nil)
		return
	}
	if req.ClientRequestID == "" {
		s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.EInvalidRequest, "client_request_id is required", "provide a UUID for idempotency", "", nil)
		return
	}
	record, err := s.resolveTaskRecord(repoID, taskRef)
	if err != nil {
		s.writeTaskStartResolveError(w, requestID, err, req.ClientRequestID)
		return
	}
	if record.Broken || record.Meta == nil {
		s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.ETaskBroken, "task exists but meta.json is unreadable", "inspect task state before retrying", req.ClientRequestID, nil)
		return
	}
	meta := record.Meta
	if meta.WorktreeID == "" {
		s.writeTaskStartError(w, http.StatusConflict, requestID, errors.ETaskCreateFailed, "task has no worktree to retry", "start a new task", req.ClientRequestID, meta)
		return
	}

	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = string(meta.Mode)
	}
	if mode == "" {
		mode = string(store.RunnerModeHeadless)
	}
	runner := strings.TrimSpace(req.Runner)
	if runner == "" {
		runner = meta.Runner
	}
	if runner == "" {
		s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.EInvalidArgument, "runner is required", "", req.ClientRequestID, meta)
		return
	}
	headless := mode == string(store.RunnerModeHeadless)
	switch mode {
	case string(store.RunnerModeHeadless):
		if req.Prompt == "" {
			s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.EPromptRequired, "prompt is required for headless task retry", "", req.ClientRequestID, meta)
			return
		}
		if len(req.Prompt) > MaxPromptSize {
			s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.EPromptTooLarge, "prompt exceeds maximum size", "reduce prompt size or split into smaller chunks", req.ClientRequestID, meta)
			return
		}
	case string(store.RunnerModeHeaded):
		if req.Prompt != "" {
			s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.EUsage, "headed task retry does not accept a prompt", "omit --prompt/--prompt-file or use --mode headless", req.ClientRequestID, meta)
			return
		}
	default:
		s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.EInvalidArgument, "mode must be headless or headed", "", req.ClientRequestID, meta)
		return
	}
	canonicalRunner, err := validateControlPlaneStartRunner(runner, req.RunnerArgs, headless)
	if err != nil {
		fail := runnerValidationFailure(err)
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, meta)
		return
	}
	runner = canonicalRunner
	req.InvocationName = strings.TrimSpace(req.InvocationName)
	req.ExecutionProfile = strings.TrimSpace(req.ExecutionProfile)
	req.AgencyConfigPath = strings.TrimSpace(req.AgencyConfigPath)
	if req.AgencyConfigPath != "" && !filepath.IsAbs(req.AgencyConfigPath) {
		s.writeTaskStartError(w, http.StatusBadRequest, requestID, errors.EInvalidArgument, "agency_config_path must be absolute", "", req.ClientRequestID, meta)
		return
	}

	requestEnv := copyStringMap(req.Env)
	execCtx, err := s.resolveExecutionContext(meta.RepoRoot, repoID, req.AgencyConfigPath, req.ExecutionProfile)
	if err != nil {
		code := errors.CodeOr(err, errors.EInternal)
		s.writeTaskStartError(w, http.StatusBadRequest, requestID, code, apiErrorMessage(err), "", req.ClientRequestID, meta)
		return
	}
	req.ExecutionProfile = execCtx.Profile
	req.CheckoutRoot = execCtx.CheckoutRoot
	gitEnv := withNonInteractiveEnv(execCtx.ProfileEnv)
	req.Env = envForLaunch(execCtx.ProfileEnv, requestEnv)
	retryFingerprint := taskRetryFingerprint(meta, mode, runner, req, requestEnv)
	if s.writeTaskRetryIdempotencyResult(w, requestID, meta, req.ClientRequestID, retryFingerprint, false) {
		return
	}

	unlock, fail := s.acquireControlPlaneRepoLock(repoID, "task retry")
	if fail != nil {
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, meta)
		return
	}
	defer func() { _ = unlock() }()

	if latest, err := s.store.ReadTaskMeta(repoID, meta.TaskID); err == nil {
		meta = latest
	} else {
		s.writeTaskStartError(w, http.StatusInternalServerError, requestID, errors.GetCode(err), err.Error(), "", req.ClientRequestID, meta)
		return
	}
	if s.writeTaskRetryIdempotencyResult(w, requestID, meta, req.ClientRequestID, retryFingerprint, true) {
		return
	}
	if err := s.reserveTaskRetryRequest(repoID, meta, req.ClientRequestID, retryFingerprint); err != nil {
		s.writeTaskStartError(w, http.StatusInternalServerError, requestID, errors.GetCode(err), err.Error(), "", req.ClientRequestID, meta)
		return
	}

	wtMeta, err := s.store.ReadIntegrationWorktreeMeta(repoID, meta.WorktreeID)
	if err != nil {
		fail := startFailureFromError(http.StatusInternalServerError, errors.EWorktreeBroken, err, "")
		s.markTaskRetryFailed(repoID, meta.TaskID, req.ClientRequestID, fail)
		if latest, readErr := s.store.ReadTaskMeta(repoID, meta.TaskID); readErr == nil {
			meta = latest
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, meta)
		return
	}
	wtRecord := &store.IntegrationWorktreeRecord{
		WorktreeID:  meta.WorktreeID,
		RepoID:      repoID,
		Name:        wtMeta.Name,
		Meta:        wtMeta,
		WorktreeDir: s.store.IntegrationWorktreeDir(repoID, meta.WorktreeID),
	}
	startReq := TaskStartRequest{
		RepoRoot:           meta.RepoRoot,
		Name:               meta.Name,
		BaseBranch:         meta.BaseBranch,
		Mode:               mode,
		Runner:             runner,
		Prompt:             req.Prompt,
		InvocationName:     req.InvocationName,
		RunnerArgs:         req.RunnerArgs,
		Env:                req.Env,
		ExecutionProfile:   req.ExecutionProfile,
		AgencyConfigPath:   req.AgencyConfigPath,
		CheckoutRoot:       req.CheckoutRoot,
		ClientRequestID:    req.ClientRequestID,
		NoIncludeUntracked: req.NoIncludeUntracked,
	}
	var invMeta *store.InvocationMeta
	envKeys := sortedEnvKeys(requestEnv)
	if headless {
		invMeta, err = s.startTaskHeadlessInvocation(ctx, meta.RepoRoot, repoID, meta.TaskID, retryFingerprint, wtRecord, startReq, envKeys, gitEnv)
	} else {
		invMeta, err = s.startTaskHeadedInvocation(ctx, meta.RepoRoot, repoID, meta.TaskID, retryFingerprint, wtRecord, startReq, envKeys, gitEnv)
	}
	if err != nil {
		fail := asStartFailure(err)
		s.markTaskRetryFailed(repoID, meta.TaskID, req.ClientRequestID, fail)
		if latest, readErr := s.store.ReadTaskMeta(repoID, meta.TaskID); readErr == nil {
			meta = latest
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, meta)
		return
	}
	if err := s.appendTaskEventOnceByInvocationID(repoID, meta.TaskID, "agency.task_retried", invMeta.InvocationID, map[string]any{
		"invocation_id":     invMeta.InvocationID,
		"checkout_root":     invMeta.CheckoutRoot,
		"execution_profile": invMeta.ExecutionProfile,
	}); err != nil {
		fail := newStartFailure(http.StatusInternalServerError, errors.EPersistFailed, "failed to append task event: "+err.Error(), "")
		s.abortStartedTaskInvocation(repoID, invMeta, "task_retry_event_failed")
		s.markTaskRetryFailed(repoID, meta.TaskID, req.ClientRequestID, fail)
		if latest, readErr := s.store.ReadTaskMeta(repoID, meta.TaskID); readErr == nil {
			meta = latest
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, meta)
		return
	}
	latest, err := s.markTaskRetryRunning(repoID, meta.TaskID, req.ClientRequestID, invMeta)
	if err != nil {
		fail := startFailureFromError(http.StatusInternalServerError, errors.EMetaWriteFailed, err, "")
		s.abortStartedTaskInvocation(repoID, invMeta, "retry_task_update_failed")
		s.markTaskRetryFailed(repoID, meta.TaskID, req.ClientRequestID, fail)
		if reread, readErr := s.store.ReadTaskMeta(repoID, meta.TaskID); readErr == nil {
			meta = reread
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, req.ClientRequestID, meta)
		return
	}
	s.writeTaskStartSuccess(w, requestID, req.ClientRequestID, latest, false)
}

func taskRetryFingerprint(meta *store.TaskMeta, mode, runner string, req TaskRetryRequest, requestEnv map[string]string) string {
	promptHash := sha256.Sum256([]byte(req.Prompt))
	payload, _ := json.Marshal(struct {
		TaskID             string   `json:"task_id"`
		WorktreeID         string   `json:"worktree_id"`
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
		TaskID:             meta.TaskID,
		WorktreeID:         meta.WorktreeID,
		CheckoutRoot:       req.CheckoutRoot,
		ExecutionProfile:   req.ExecutionProfile,
		Mode:               mode,
		Runner:             runner,
		EnvKeys:            sortedEnvKeys(requestEnv),
		PromptSHA256:       hex.EncodeToString(promptHash[:]),
		InvocationName:     req.InvocationName,
		RunnerArgs:         slices.Clone(req.RunnerArgs),
		NoIncludeUntracked: req.NoIncludeUntracked,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (s *Server) writeTaskRetryIdempotencyResult(w http.ResponseWriter, requestID string, meta *store.TaskMeta, clientRequestID, fingerprint string, finalizeIncomplete bool) bool {
	record, ok := meta.RetryRequests[clientRequestID]
	if !ok {
		return false
	}
	if record.RequestFingerprint != fingerprint {
		s.writeTaskStartError(w, http.StatusConflict, requestID, errors.ETaskFingerprintConflict, "client_request_id was already used for a different task retry request", "retry with the original request or choose a new client_request_id", clientRequestID, meta)
		return true
	}
	if record.InvocationID == "" {
		if !finalizeIncomplete {
			return false
		}
		repaired, repairErr := s.repairTaskRetryFromClaimedInvocation(meta.RepoID, meta, clientRequestID, fingerprint)
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
		message := "retry request was already accepted but no invocation was recorded"
		if record.Error != "" {
			message = record.Error
		}
		if record.ErrorCode != "" {
			code = errors.Code(record.ErrorCode)
		}
		fail := newStartFailure(http.StatusConflict, code, message, "inspect task state before retrying")
		if record.State == store.TaskRetryStateStarting && finalizeIncomplete {
			s.markTaskRetryFailed(meta.RepoID, meta.TaskID, clientRequestID, fail)
			if latest, err := s.store.ReadTaskMeta(meta.RepoID, meta.TaskID); err == nil {
				meta = latest
			}
		}
		s.writeTaskStartError(w, fail.status, requestID, fail.code, fail.msg, fail.hint, clientRequestID, meta)
		return true
	}
	respMeta := *meta
	respMeta.PrimaryInvocationID = record.InvocationID
	if invMeta, err := s.store.ReadInvocationMeta(meta.RepoID, record.InvocationID); err == nil {
		respMeta.Mode = invMeta.Mode
		respMeta.Runner = invMeta.Runner
	}
	s.writeTaskStartSuccess(w, requestID, clientRequestID, &respMeta, true)
	return true
}

func (s *Server) repairTaskRetryFromClaimedInvocation(repoID string, meta *store.TaskMeta, clientRequestID, fingerprint string) (*store.TaskMeta, error) {
	invMeta, ok, err := s.findClaimedTaskInvocation(repoID, meta.TaskID, clientRequestID, fingerprint)
	if err != nil || !ok {
		return nil, err
	}
	if err := s.appendTaskEventOnceByInvocationID(repoID, meta.TaskID, "agency.task_retried", invMeta.InvocationID, map[string]any{
		"invocation_id":     invMeta.InvocationID,
		"checkout_root":     invMeta.CheckoutRoot,
		"execution_profile": invMeta.ExecutionProfile,
	}); err != nil {
		return nil, err
	}
	return s.markTaskRetryRunning(repoID, meta.TaskID, clientRequestID, invMeta)
}

func (s *Server) reserveTaskRetryRequest(repoID string, meta *store.TaskMeta, clientRequestID, fingerprint string) error {
	_, err := s.store.UpdateTaskMeta(repoID, meta.TaskID, func(latest *store.TaskMeta) {
		now := s.nowRFC3339()
		if latest.RetryRequests == nil {
			latest.RetryRequests = make(map[string]store.TaskRetryRecord)
		}
		latest.RetryRequests[clientRequestID] = store.TaskRetryRecord{
			RequestFingerprint: fingerprint,
			State:              store.TaskRetryStateStarting,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		latest.UpdatedAt = now
	})
	return err
}

func (s *Server) markTaskRetryRunning(repoID, taskID, clientRequestID string, invMeta *store.InvocationMeta) (*store.TaskMeta, error) {
	return s.store.UpdateTaskMeta(repoID, taskID, func(meta *store.TaskMeta) {
		now := s.nowRFC3339()
		meta.State = store.TaskStateRunning
		meta.PrimaryInvocationID = invMeta.InvocationID
		meta.Mode = invMeta.Mode
		meta.Runner = invMeta.Runner
		meta.CheckoutRoot = invMeta.CheckoutRoot
		meta.ExecutionProfile = invMeta.ExecutionProfile
		meta.FailedPhase = ""
		meta.ErrorCode = ""
		meta.Error = ""
		if meta.RetryRequests == nil {
			meta.RetryRequests = make(map[string]store.TaskRetryRecord)
		}
		record := meta.RetryRequests[clientRequestID]
		record.InvocationID = invMeta.InvocationID
		record.State = store.TaskRetryStateRunning
		record.UpdatedAt = now
		meta.RetryRequests[clientRequestID] = record
		meta.UpdatedAt = now
	})
}

func (s *Server) markTaskRetryFailed(repoID, taskID, clientRequestID string, failure startFailure) {
	if _, err := s.store.UpdateTaskMeta(repoID, taskID, func(meta *store.TaskMeta) {
		record, ok := meta.RetryRequests[clientRequestID]
		if !ok {
			return
		}
		record.State = store.TaskRetryStateFailed
		record.ErrorCode = string(failure.code)
		record.Error = failure.msg
		record.UpdatedAt = s.nowRFC3339()
		meta.RetryRequests[clientRequestID] = record
	}); err != nil {
		log.Printf("agencyd: persist failed task retry %s/%s: %v", repoID, taskID, err)
	}
}
