package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// handleRestartFromCheckpoint handles POST /invocations/{ref}/restart.
// Canonical flow: stop running headless process (if needed), apply checkpoint,
// and restart runner in one invocation-scoped operation.
func (s *Server) handleRestartFromCheckpoint(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)
	setRequestIDHeader(w, requestID)

	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeRestartError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", "repo_id query parameter is required", "")
		return
	}

	var req RestartFromCheckpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeRestartError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "")
		return
	}
	if req.CheckpointID <= 0 {
		s.writeRestartError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", "checkpoint_id must be positive", "")
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		code := errors.GetCode(resolveErr)
		if code == "" {
			code = errors.EInvocationNotFound
		}
		s.writeRestartError(w, http.StatusNotFound, requestID, string(code), resolveErr.Error(), "use 'agency agent ls --repo <repo>' to list invocations")
		return
	}
	if record.Meta == nil {
		s.writeRestartError(w, http.StatusInternalServerError, requestID, string(errors.EInvocationBroken), "invocation exists but meta.json is unreadable", "")
		return
	}
	if record.Meta.Mode != store.RunnerModeHeadless {
		s.writeRestartError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvocationInvalidMode),
			"restart is only supported for headless invocations",
			"headed invocations should use 'agency agent attach' instead",
		)
		return
	}
	// Repo-scoped lock serializes checkpoint apply and runner lifecycle mutations.
	unlock, err := s.repoLock.Lock(record.RepoID, "restart")
	if err != nil {
		s.writeRestartError(w, http.StatusConflict, requestID, string(errors.ERepoLocked), "repository is locked by another operation", "wait for the other operation to complete")
		return
	}
	defer func() { _ = unlock() }()

	meta, err := s.Store.ReadInvocationMeta(record.RepoID, record.InvocationID)
	if err != nil {
		if errors.GetCode(err) == errors.EInvocationNotFound {
			s.writeRestartError(w, http.StatusNotFound, requestID, string(errors.EInvocationNotFound), "invocation not found", "")
			return
		}
		s.writeRestartError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", "failed to read invocation meta: "+err.Error(), "")
		return
	}

	promptText, err := loadInvocationPrompt(s.Store, record.RepoID, record.InvocationID, meta)
	if err != nil {
		s.writeRestartError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EPromptRequired),
			err.Error(),
			"restart requires a stored prompt from initial invocation start",
		)
		return
	}

	canonicalRunner, err := runners.Canonicalize(meta.Runner)
	if err != nil {
		s.writeRestartError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.ERunnerNotFound),
			err.Error(),
			"valid runners: "+strings.Join(runners.CanonicalIDs(), ", "),
		)
		return
	}

	effectiveRunnerArgs := mergeRestartRunnerArgs(canonicalRunner, meta.RunnerArgs, req.RunnerArgs)
	if _, err := validateControlPlaneStartRunner(canonicalRunner, effectiveRunnerArgs, true); err != nil {
		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerArgConflict
		}
		hint := "remove reserved flags from runner_args"
		if code == errors.ERunnerNotFound {
			hint = "valid runners: " + strings.Join(runners.CanonicalIDs(), ", ")
		}
		s.writeRestartError(
			w,
			http.StatusBadRequest,
			requestID,
			string(code),
			err.Error(),
			hint,
		)
		return
	}

	effectiveEnv := req.Env
	if message, hint := validateRestartEnvReplay(meta.CustomEnvKeys, req.Env); message != "" {
		s.writeRestartError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST", message, hint)
		return
	}

	// If running, stop first and wait for terminalization before apply/start.
	if meta.Status == store.InvocationStatusRunning || meta.Status == store.InvocationStatusStarting {
		if err := s.stopHeadlessForRestart(r.Context(), record.RepoID, record.InvocationID, meta); err != nil {
			s.writeRestartError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", "failed to stop running invocation: "+err.Error(), "")
			return
		}
	}

	// Re-read after stop to ensure latest persisted state.
	meta, err = s.Store.ReadInvocationMeta(record.RepoID, record.InvocationID)
	if err != nil {
		s.writeRestartError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", "failed to refresh invocation meta: "+err.Error(), "")
		return
	}

	applier := checkpoint.NewApplierWithWriter(
		record.InvocationID,
		meta.SandboxPath,
		s.Store.InvocationDir(record.RepoID, record.InvocationID),
		s.Store.InvocationEventsPath(record.RepoID, record.InvocationID),
		s.Runner,
		s.FS,
		s.Clock,
		s.InvocationEvents,
	)
	cp, err := applier.ApplyWithOptions(r.Context(), req.CheckpointID, checkpoint.ApplyOptions{
		RewindHeadToSnapshotBase: true,
	})
	if err != nil {
		switch errors.GetCode(err) {
		case errors.ECheckpointNotFound:
			s.writeRestartError(
				w,
				http.StatusNotFound,
				requestID,
				string(errors.ECheckpointNotFound),
				err.Error(),
				"run 'agency agent checkpoint ls' to see available checkpoints",
			)
		case errors.ERollbackFailed:
			s.writeRestartError(w, http.StatusInternalServerError, requestID, string(errors.ERollbackFailed), err.Error(), "")
		default:
			s.writeRestartError(w, http.StatusInternalServerError, requestID, string(errors.ECheckpointFailed), err.Error(), "")
		}
		return
	}

	startReq := ControlPlaneStartRequest{
		Runner:             canonicalRunner,
		Prompt:             promptText,
		RunnerArgs:         effectiveRunnerArgs,
		Env:                effectiveEnv,
		NoIncludeUntracked: !meta.CheckpointIncludeUntracked,
	}

	pid, pgid, err := s.startRunner(
		r.Context(),
		record.RepoID,
		&invocation.CreateResult{
			InvocationID: record.InvocationID,
			SandboxPath:  meta.SandboxPath,
		},
		meta.SandboxPath, // worktree root is sufficient for checkpoint ref operations
		meta.IntegrationWorktreeID,
		startReq,
	)
	if err != nil {
		now := s.Clock().UTC().Format(time.RFC3339)
		_ = s.Store.UpdateInvocationMeta(record.RepoID, record.InvocationID, func(m *store.InvocationMeta) {
			m.Status = store.InvocationStatusFailed
			m.ExitReason = "start_failed"
			m.FailureReason = "spawn_failed"
			m.FinishedAt = now
			m.PID = nil
			m.PGID = nil
			m.LifecycleOwner = ""
		})

		code := errors.GetCode(err)
		if code == "" {
			code = errors.ERunnerStartFailed
		}
		s.writeRestartError(w, http.StatusInternalServerError, requestID, string(code), err.Error(), "")
		return
	}

	now := s.Clock().UTC().Format(time.RFC3339)
	daemonPID := os.Getpid()
	promptHash := sha256.Sum256([]byte(promptText))
	promptSHA := hex.EncodeToString(promptHash[:])
	envKeys := sortedEnvKeys(effectiveEnv)
	restartedRunnerArgs := append([]string(nil), effectiveRunnerArgs...)
	_ = s.Store.UpdateInvocationMeta(record.RepoID, record.InvocationID, func(m *store.InvocationMeta) {
		m.Status = store.InvocationStatusRunning
		m.Runner = canonicalRunner
		m.PID = &pid
		m.PGID = &pgid
		m.DaemonPID = &daemonPID
		m.DaemonInstanceID = s.InstanceID
		m.ClaimedAt = now
		m.LifecycleOwner = "daemon"
		m.PromptPath = s.Store.InvocationPromptPath(record.RepoID, record.InvocationID)
		m.PromptSHA256 = promptSHA
		m.FinishedAt = ""
		m.ExitReason = ""
		m.FailureReason = ""
		m.ExitCode = nil
		m.StopRequestedAt = ""
		m.Flags.NeedsAttention = false
		m.RunnerArgs = restartedRunnerArgs
		m.CustomEnvKeys = envKeys
	})

	resp := RestartFromCheckpointResponse{
		OK:               true,
		APIVersion:       APIVersion,
		BuildVersion:     version.FullVersion(),
		RequestID:        requestID,
		InvocationID:     record.InvocationID,
		CheckpointID:     cp.ID,
		SnapshotCommit:   cp.SnapshotCommit,
		RestoredAt:       s.Clock().UTC().Format(time.RFC3339),
		PID:              pid,
		PGID:             pgid,
		DaemonInstanceID: s.InstanceID,
		LogPaths: &LogPaths{
			Raw:      s.readableInvocationLogPath(record.RepoID, record.InvocationID, "raw"),
			Stderr:   s.readableInvocationLogPath(record.RepoID, record.InvocationID, "stderr"),
			Stream:   s.readableInvocationLogPath(record.RepoID, record.InvocationID, "stream"),
			Hooks:    s.readableInvocationLogPath(record.RepoID, record.InvocationID, "hooks"),
			Terminal: s.readableInvocationLogPath(record.RepoID, record.InvocationID, "terminal"),
		},
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func loadInvocationPrompt(st *store.Store, repoID, invocationID string, meta *store.InvocationMeta) (string, error) {
	if st == nil {
		return "", fmt.Errorf("store is required to load prompt")
	}

	promptPath := st.InvocationPromptPath(repoID, invocationID)
	if meta != nil && meta.PromptPath != "" {
		promptPath = meta.PromptPath
	}

	data, err := os.ReadFile(promptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read stored prompt: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("stored prompt is empty")
	}
	return string(data), nil
}

func validateRestartEnvReplay(requiredKeys []string, provided map[string]string) (message, hint string) {
	if len(requiredKeys) == 0 {
		return "", ""
	}
	required := append([]string(nil), requiredKeys...)
	sort.Strings(required)
	if len(provided) == 0 {
		joined := strings.Join(required, ", ")
		return "restart requires explicit env values because original invocation used custom environment",
			"provide --env KEY=VALUE for keys: " + joined
	}

	missing := make([]string, 0)
	for _, key := range required {
		if _, ok := provided[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return "restart env is missing required keys: " + strings.Join(missing, ", "),
			"provide --env KEY=VALUE for all required keys"
	}
	return "", ""
}

func mergeRestartRunnerArgs(runner string, storedArgs, requestArgs []string) []string {
	stored := append([]string(nil), storedArgs...)
	if len(requestArgs) == 0 {
		return stored
	}
	requested := append([]string(nil), requestArgs...)
	if restartRunnerArgsShouldReplaceStored(runner, requested) {
		return requested
	}
	merged := append(stored, requested...)
	return merged
}

func restartRunnerArgsShouldReplaceStored(runner string, args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return false
		}
		switch runner {
		case runners.RunnerClaudeCode:
			if arg == "--model" || arg == "--effort" ||
				strings.HasPrefix(arg, "--model=") || strings.HasPrefix(arg, "--effort=") {
				return true
			}
		case runners.RunnerCursor:
			if arg == "--model" || strings.HasPrefix(arg, "--model=") {
				return true
			}
		case runners.RunnerCodex:
			if arg == "--model" || arg == "-m" ||
				strings.HasPrefix(arg, "--model=") || strings.HasPrefix(arg, "-m=") {
				return true
			}
			if arg == "--config" || arg == "-c" {
				// Missing next token is invalid input, but treat as replace-mode for safety.
				if i+1 >= len(args) {
					return true
				}
				key, ok := parseRestartCodexConfigAssignment(args[i+1])
				if ok && key == "model_reasoning_effort" {
					return true
				}
				i++
				continue
			}
			if strings.HasPrefix(arg, "--config=") {
				key, ok := parseRestartCodexConfigAssignment(strings.TrimPrefix(arg, "--config="))
				if ok && key == "model_reasoning_effort" {
					return true
				}
			}
			if strings.HasPrefix(arg, "-c=") {
				key, ok := parseRestartCodexConfigAssignment(strings.TrimPrefix(arg, "-c="))
				if ok && key == "model_reasoning_effort" {
					return true
				}
			}
		}
	}
	return false
}

func parseRestartCodexConfigAssignment(raw string) (string, bool) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", false
	}
	parts := strings.SplitN(token, "=", 2)
	if len(parts) != 2 {
		return "", false
	}
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", false
	}
	return key, true
}

func (s *Server) stopHeadlessForRestart(ctx context.Context, repoID, invocationID string, meta *store.InvocationMeta) error {
	if meta == nil {
		return nil
	}
	if meta.Mode != store.RunnerModeHeadless {
		return nil
	}
	if meta.Status == store.InvocationStatusFinished || meta.Status == store.InvocationStatusFailed {
		return nil
	}

	pgid := 0
	if meta.PGID != nil {
		pgid = *meta.PGID
	} else if meta.PID != nil {
		pgid = *meta.PID
	}

	s.mu.RLock()
	proc, supervised := s.processes[invocationID]
	s.mu.RUnlock()

	if supervised && proc != nil {
		proc.exitReason.Store("stopped")
		proc.failureReason.Store("stopped")
	}

	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}

	if supervised && proc != nil {
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		select {
		case <-proc.done:
			return nil
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for process exit")
		}
	}

	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		m.Status = store.InvocationStatusFailed
		m.ExitReason = "stopped"
		m.FailureReason = "stopped"
		m.FinishedAt = now
		m.PID = nil
		m.PGID = nil
		m.LifecycleOwner = ""
	})
	return nil
}
