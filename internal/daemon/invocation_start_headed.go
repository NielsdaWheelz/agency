package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

func (s *Server) handleControlPlaneStartHeaded(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)

	var req ControlPlaneStartHeadedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "", "", requestID)
		return
	}
	if req.ClientRequestID == "" {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "client_request_id is required", "provide a UUID for idempotency", "", requestID)
		return
	}
	if req.RepoRoot == "" {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "repo_root is required", "", req.ClientRequestID, requestID)
		return
	}
	if req.WorktreeRef == "" {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "worktree_ref is required", "", req.ClientRequestID, requestID)
		return
	}
	if req.Runner == "" {
		s.writeHeadedError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "runner is required", "", req.ClientRequestID, requestID)
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

	if entry, isDuplicate := s.checkHeadedIdempotency(repoIdentity.RepoID, req.ClientRequestID); isDuplicate {
		meta, err := s.Store.ReadInvocationMeta(repoIdentity.RepoID, entry.InvocationID)
		if err == nil {
			s.writeHeadedSuccess(w, entry.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, true)
			return
		}
	}

	prep, ok := s.prepareControlPlaneStart(ctx, repoRoot, req.WorktreeRef, "control_plane_start_headed", func(status int, code, message, hint string) {
		s.writeHeadedError(w, status, code, message, hint, req.ClientRequestID, requestID)
	}, repoIdentity)
	if !ok {
		return
	}
	defer func() { _ = prep.unlockRepo() }()

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
		NoIncludeUntracked:      req.NoIncludeUntracked,
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
		userCfg = config.UserConfig{}
	}
	runnerCmd, err := config.ResolveRunnerCmd(s.Runner, s.FS, s.ConfigDir, userCfg, req.Runner)
	if err != nil {
		s.markHeadedInvocationFailed(repoIdentity.RepoID, createResult.InvocationID, "start_failed")
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.ERunnerNotFound), "failed to resolve runner command: "+err.Error(), "ensure runner is installed and configured", req.ClientRequestID, requestID)
		return
	}

	sessionName := tmux.SessionName(createResult.InvocationID)
	exists, err := s.TmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		s.recordInvocationWarning(repoIdentity.RepoID, createResult.InvocationID, "start_headed_tmux_has_session_failed", err.Error(), map[string]any{
			"session_name": sessionName,
		})
	} else if exists {
		s.markHeadedInvocationFailed(repoIdentity.RepoID, createResult.InvocationID, "start_failed")
		s.writeHeadedError(w, http.StatusConflict, string(errors.ETmuxSessionExists), "tmux session already exists: "+sessionName, "a tmux session with this name already exists; kill it with 'tmux kill-session -t "+sessionName+"'", req.ClientRequestID, requestID)
		return
	}

	argv := append([]string{runnerCmd}, headedRunnerArgs...)
	if err := s.TmuxClient.NewSession(ctx, sessionName, createResult.SandboxPath, argv); err != nil {
		s.markHeadedInvocationFailed(repoIdentity.RepoID, createResult.InvocationID, "start_failed")
		s.writeHeadedError(w, http.StatusInternalServerError, string(errors.EInvocationStartFailed), "failed to create tmux session: "+err.Error(), "ensure tmux is installed and working", req.ClientRequestID, requestID)
		return
	}

	now := s.Clock().UTC().Format(time.RFC3339)
	daemonPID := os.Getpid()
	runnerArgs := append([]string(nil), req.RunnerArgs...)
	err = s.Store.UpdateInvocationMeta(repoIdentity.RepoID, createResult.InvocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.TmuxSession = sessionName
		meta.DaemonPID = &daemonPID
		meta.DaemonInstanceID = s.InstanceID
		meta.ClaimedAt = now
		meta.LifecycleOwner = "daemon"
		meta.RunnerArgs = runnerArgs
	})
	if err != nil {
		_ = s.TmuxClient.KillSession(ctx, sessionName)
		s.writeHeadedError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to update invocation meta: "+err.Error(), "", req.ClientRequestID, requestID)
		return
	}

	proc := &SupervisedProcess{
		InvocationID:          createResult.InvocationID,
		RepoID:                repoIdentity.RepoID,
		IntegrationWorktreeID: prep.wtRecord.WorktreeID,
		Mode:                  "headed",
		TmuxSession:           sessionName,
		Runner:                req.Runner,
		RepoRoot:              repoRoot,
		done:                  make(chan struct{}),
	}
	s.mu.Lock()
	s.processes[createResult.InvocationID] = proc
	s.mu.Unlock()

	s.recordHeadedIdempotency(repoIdentity.RepoID, req.ClientRequestID, createResult.InvocationID, sessionName, createResult.SandboxPath)

	meta, _ := s.Store.ReadInvocationMeta(repoIdentity.RepoID, createResult.InvocationID)
	s.writeHeadedSuccess(w, createResult.InvocationID, meta, repoIdentity.RepoID, req.ClientRequestID, requestID, false)
}

func (s *Server) markHeadedInvocationFailed(repoID, invocationID, exitReason string) {
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFailed
		meta.ExitReason = exitReason
		meta.FinishedAt = now
		meta.Flags.NeedsAttention = true
	})
}
