package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/relay"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/jsonl"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func nonInteractiveRunnerEnv() map[string]string {
	return map[string]string{
		"CI":                  "1",
		"GIT_TERMINAL_PROMPT": "0",
		"GH_PROMPT_DISABLED":  "1",
	}
}

func mergeEnvDeterministic(baseEnv []string, overlays ...map[string]string) []string {
	merged := make(map[string]string, len(baseEnv))
	for _, entry := range baseEnv {
		key, val, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		merged[key] = val
	}
	for _, overlay := range overlays {
		for k, v := range overlay {
			merged[k] = v
		}
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]string, 0, len(keys))
	for _, k := range keys {
		result = append(result, k+"="+merged[k])
	}
	return result
}

func sortedEnvKeys(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func loadMaxStreamSeq(path string) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	var maxSeq uint64
	_ = jsonl.Visit(f, maxTimelineLineBytes, jsonl.VisitOptions{OversizedPrefixBytes: 1024}, func(line jsonl.Line) error {
		if line.Oversized {
			if seq, ok := jsonl.ExtractUintField(line.Bytes, "seq"); ok && seq > maxSeq {
				maxSeq = seq
			}
			return nil
		}
		var event struct {
			Seq uint64 `json:"seq"`
		}
		if err := json.Unmarshal(line.Bytes, &event); err == nil && event.Seq > maxSeq {
			maxSeq = event.Seq
		}
		return nil
	})
	return maxSeq
}

func (s *Server) startRunner(ctx context.Context, repoID string, result *invocation.CreateResult, repoRoot, integrationWorktreeID string, req ControlPlaneStartRequest) (int, int, error) {
	args, err := buildRunnerArgsWithSandbox(req.Runner, req.Prompt, result.SandboxPath, req.RunnerArgs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to build runner args: %w", err)
	}
	return s.startRunnerWithArgs(ctx, repoID, result, repoRoot, integrationWorktreeID, req, args, "")
}

func (s *Server) startRunnerResumeTurn(ctx context.Context, proc *SupervisedProcess, prompt string) (int, int, error) {
	req := ControlPlaneStartRequest{
		Runner:             proc.Runner,
		Prompt:             prompt,
		RunnerArgs:         append([]string(nil), proc.RunnerArgs...),
		Env:                copyStringMap(proc.Env),
		NoIncludeUntracked: proc.NoIncludeUntracked,
	}
	resumeSessionID := proc.GetResumeSessionID()
	args, err := runners.BuildResumeArgs(proc.Runner, prompt, resumeSessionID, req.RunnerArgs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to build runner resume args: %w", err)
	}
	return s.startRunnerWithArgs(ctx, proc.RepoID, &invocation.CreateResult{
		InvocationID: proc.InvocationID,
		SandboxPath:  proc.SandboxPath,
	}, proc.RepoRoot, proc.IntegrationWorktreeID, req, args, resumeSessionID)
}

func (s *Server) startRunnerWithArgs(ctx context.Context, repoID string, result *invocation.CreateResult, repoRoot, integrationWorktreeID string, req ControlPlaneStartRequest, args []string, resumeSessionID string) (int, int, error) {
	userCfg, err := s.LoadUserConfig()
	if err != nil {
		userCfg = config.UserConfig{}
	}

	runnerCmd, err := config.ResolveRunnerCmd(s.Runner, s.FS, s.ConfigDir, userCfg, req.Runner)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to resolve runner command: %w", err)
	}

	logFiles, err := s.openInvocationLogFiles(repoID, result.InvocationID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open invocation log files: %w", err)
	}
	rawLogPath := logFiles.RawPath
	stderrLogPath := logFiles.StderrPath
	streamLogPath := logFiles.StreamPath
	rawFile := logFiles.RawFile
	stderrFile := logFiles.StderrFile
	streamFile := logFiles.StreamFile

	envOverlay := nonInteractiveRunnerEnv()
	for k, v := range req.Env {
		envOverlay[k] = v
	}

	stdinReader, chatRelay, relayWarning := createChatRelay(req.Runner)
	if relayWarning != nil {
		s.recordInvocationWarning(repoID, result.InvocationID, "chat_relay_setup_failed", relayWarning.Error(), nil)
	}
	startedProc, err := exec.StartProcess(context.Background(), runnerCmd, args, exec.StartOpts{
		Dir:        result.SandboxPath,
		Env:        envOverlay,
		Stdin:      stdinReader,
		StdoutPipe: true,
		StderrPipe: true,
		Setpgid:    true,
	})
	if stdinReader != nil {
		_ = stdinReader.Close()
	}
	if err != nil {
		if chatRelay != nil {
			_ = chatRelay.Close()
		}
		logFiles.Close()
		return 0, 0, fmt.Errorf("failed to start runner: %w", err)
	}
	if err := sendInitialPromptIfRequired(req.Runner, req.Prompt, chatRelay); err != nil {
		if chatRelay != nil {
			_ = chatRelay.Close()
		}
		if startedProc.PGID > 0 {
			_ = syscall.Kill(-startedProc.PGID, syscall.SIGKILL)
		}
		logFiles.Close()
		return 0, 0, fmt.Errorf("failed to deliver initial prompt: %w", err)
	}

	pid := startedProc.PID
	pgid := startedProc.PGID
	parser := stream.NewParser(result.InvocationID, req.Runner, s.Clock)
	parser.SetInitialSeq(loadMaxStreamSeq(streamLogPath))

	checkpointsDir := s.Store.InvocationDir(repoID, result.InvocationID)
	eventsPath := s.Store.InvocationEventsPath(repoID, result.InvocationID)
	cpConfig := checkpoint.DefaultConfig()
	cpConfig.IncludeUntracked = !req.NoIncludeUntracked
	if s.CheckpointDebounceOverride != nil {
		cpConfig.DebounceInterval = *s.CheckpointDebounceOverride
		cpConfig.DriftInterval = *s.CheckpointDebounceOverride
	}

	cpEngine := checkpoint.NewEngineWithWriter(
		result.InvocationID,
		repoID,
		result.SandboxPath,
		repoRoot,
		checkpointsDir,
		eventsPath,
		cpConfig,
		s.Runner,
		s.FS,
		s.Clock,
		s.InvocationEvents,
	)
	cpEngine.SetGitIgnoredDirs(checkpoint.ReadGitIgnoredDirs(result.SandboxPath))

	triggerCh := make(chan checkpoint.TriggerEvent, 32)
	cpEngine.SetTriggerChannel(triggerCh)
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
			s.recordInvocationWarning(repoID, result.InvocationID, "checkpoint_trigger_dropped", "checkpoint trigger queue full; dropped semantic trigger", map[string]any{
				"seq":       n.Seq,
				"tool_name": n.ToolName,
			})
		}
	})

	proc := &SupervisedProcess{
		InvocationID:          result.InvocationID,
		RepoID:                repoID,
		IntegrationWorktreeID: integrationWorktreeID,
		PID:                   pid,
		PGID:                  pgid,
		SandboxPath:           result.SandboxPath,
		RawLogFile:            rawLogPath,
		StderrFile:            stderrLogPath,
		StreamLogFile:         streamLogPath,
		Runner:                req.Runner,
		RepoRoot:              repoRoot,
		RunnerArgs:            append([]string(nil), req.RunnerArgs...),
		Env:                   copyStringMap(req.Env),
		NoIncludeUntracked:    req.NoIncludeUntracked,
		Parser:                parser,
		CheckpointEngine:      cpEngine,
		Relay:                 chatRelay,
		done:                  make(chan struct{}),
	}
	if proc.Relay != nil {
		proc.InitializeTurnTracking()
	}
	proc.SetResumeSessionID(resumeSessionID)
	parser.SetFinalNotify(func(n stream.FinalNotification) {
		s.handleSuccessfulFinalNotification(proc, n)
	})
	parser.SetSessionStartNotify(func(n stream.SessionStartNotification) {
		proc.SetResumeSessionID(n.SessionID)
	})

	s.mu.Lock()
	s.processes[result.InvocationID] = proc
	s.mu.Unlock()

	proc.streamWg.Add(2)
	go s.streamAndParseOutput(proc, startedProc.StdoutPipe, rawFile, streamFile)
	go s.streamOutput(proc, startedProc.StderrPipe, stderrFile)
	go s.waitForExitWithFailureReason(proc, startedProc, rawFile, stderrFile, streamFile)
	go s.runOutputFlushLoop(proc)
	go s.runSemanticStatusFlushLoop(proc)
	go s.runCheckpointLoop(proc)

	return pid, pgid, nil
}

func buildRunnerArgsWithSandbox(runner, prompt, sandboxPath string, extraArgs []string) ([]string, error) {
	return runners.BuildHeadlessArgs(runner, prompt, sandboxPath, extraArgs)
}

func buildRunnerArgsForHeaded(runner string, extraArgs []string) ([]string, error) {
	return runners.BuildHeadedArgs(runner, extraArgs)
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func createChatRelay(runner string) (*os.File, relay.ChatRelay, error) {
	mode, err := runners.ResolveChatMode(runner)
	if err != nil {
		return nil, nil, nil
	}
	switch mode {
	case runners.ChatModeStdin:
		pr, pw, err := os.Pipe()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create stdin pipe for chat relay: %w", err)
		}
		return pr, relay.NewStdinRelay(pw, runner), nil
	case runners.ChatModeResume:
		return nil, relay.NewResumeRelay(runner), nil
	default:
		return nil, nil, nil
	}
}

func sendInitialPromptIfRequired(runner, prompt string, chatRelay relay.ChatRelay) error {
	if chatRelay == nil {
		return nil
	}
	mode, err := runners.ResolveInitialPromptMode(runner)
	if err != nil {
		return err
	}
	if mode != runners.InitialPromptStdin {
		return nil
	}
	return chatRelay.Send(context.Background(), prompt)
}

func (s *Server) cleanupFailedInvocation(ctx context.Context, repoID string, result *invocation.CreateResult, repoRoot, failureReason string) {
	now := s.Clock().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateInvocationMeta(repoID, result.InvocationID, func(m *store.InvocationMeta) {
		m.Status = store.InvocationStatusFailed
		m.ExitReason = "start_failed"
		m.FailureReason = failureReason
		m.FinishedAt = now
		m.Flags.NeedsAttention = true
	})

	args := []string{"-C", repoRoot, "worktree", "remove", "--force", result.SandboxPath}
	_, _ = s.Runner.Run(ctx, "git", args, exec.RunOpts{})
	args = []string{"-C", repoRoot, "branch", "-D", result.SandboxBranch}
	_, _ = s.Runner.Run(ctx, "git", args, exec.RunOpts{})
}

func (s *Server) waitForExitWithFailureReason(proc *SupervisedProcess, startedProc *exec.StartedProcess, rawFile, stderrFile, streamFile *os.File) {
	defer func() { _ = rawFile.Close() }()
	defer func() { _ = stderrFile.Close() }()
	defer func() {
		if streamFile != nil {
			_ = streamFile.Close()
		}
	}()
	defer proc.CloseDone()
	defer func() {
		if proc.Relay != nil {
			_ = proc.Relay.Close()
		}
	}()

	proc.streamWg.Wait()
	exitResult, waitErr := startedProc.WaitExit()
	if proc.Parser != nil {
		proc.Parser.Stop()
	}

	exitCode := exitResult.ExitCode
	exitReason := "exited"
	failureReason := ""
	status := store.InvocationStatusFinished
	completionSatisfied := proc.SuccessfulCompletionObserved()
	if !completionSatisfied && (waitErr != nil || exitResult.ExitCode != 0 || exitResult.Signal != "") {
		status = store.InvocationStatusFailed
		failureReason = "runner_exit_nonzero"
	}

	if override, ok := proc.exitReason.Load().(string); ok && override != "" {
		if !(completionSatisfied && override == "stopped") {
			exitReason = override
			status = store.InvocationStatusFailed
		}
	}
	if override, ok := proc.failureReason.Load().(string); ok && override != "" {
		if !(completionSatisfied && override == "stopped") {
			failureReason = override
		}
	}
	if completionSatisfied && status != store.InvocationStatusFailed {
		status = store.InvocationStatusFinished
		failureReason = ""
		exitCode = 0
	}

	var queuedResumePrompts []string
	if resumeRelay, ok := proc.Relay.(*relay.ResumeRelay); ok {
		queuedResumePrompts = resumeRelay.Drain()
	}

	var semanticStatus *string
	var semanticStatusUpdatedAt string
	if proc.Parser != nil {
		if s := proc.Parser.GetSemanticStatus(); s != nil {
			str := string(*s)
			semanticStatus = &str
			semanticStatusUpdatedAt = proc.Parser.GetSemanticStatusUpdatedAt().UTC().Format(time.RFC3339)
		}
		if status == store.InvocationStatusFailed {
			semanticStatus = nil
			semanticStatusUpdatedAt = ""
		}
	}

	if status == store.InvocationStatusFinished && len(queuedResumePrompts) > 0 && runners.SupportsResumeTurns(proc.Runner) {
		pid, pgid, resumeErr := s.startRunnerResumeTurn(context.Background(), proc, queuedResumePrompts[0])
		if resumeErr != nil {
			status = store.InvocationStatusFailed
			failureReason = "runner_resume_failed"
		} else {
			if len(queuedResumePrompts) > 1 {
				s.enqueueFollowUpPrompts(proc.InvocationID, queuedResumePrompts[1:])
			}
			now := s.Clock().UTC().Format(time.RFC3339)
			daemonPID := os.Getpid()
			_ = s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
				meta.Status = store.InvocationStatusRunning
				meta.PID = &pid
				meta.PGID = &pgid
				meta.DaemonPID = &daemonPID
				meta.DaemonInstanceID = s.InstanceID
				meta.ClaimedAt = now
				meta.ExitReason = ""
				meta.FailureReason = ""
				meta.ExitCode = nil
				meta.FinishedAt = ""
				meta.LifecycleOwner = "daemon"
				meta.SemanticStatus = nil
				meta.SemanticStatusUpdatedAt = ""
				meta.Flags.NeedsAttention = false
			})
			return
		}
	}

	now := s.Clock().UTC().Format(time.RFC3339)
	if err := s.Store.UpdateInvocationMeta(proc.RepoID, proc.InvocationID, func(meta *store.InvocationMeta) {
		meta.Status = status
		meta.ExitReason = exitReason
		if failureReason != "" {
			meta.FailureReason = failureReason
		}
		meta.ExitCode = &exitCode
		meta.FinishedAt = now
		meta.PID = nil
		meta.LifecycleOwner = ""
		if semanticStatus != nil {
			s := runnerstatus.Status(*semanticStatus)
			meta.SemanticStatus = &s
			meta.SemanticStatusUpdatedAt = semanticStatusUpdatedAt
		} else {
			meta.SemanticStatus = nil
			meta.SemanticStatusUpdatedAt = ""
		}
	}); err != nil {
		s.recordInvocationWarning(proc.RepoID, proc.InvocationID, "meta_update_on_exit_failed", err.Error(), nil)
	}

	s.mu.Lock()
	if current, ok := s.processes[proc.InvocationID]; ok && current == proc {
		delete(s.processes, proc.InvocationID)
	}
	s.mu.Unlock()
}

func (s *Server) enqueueFollowUpPrompts(invocationID string, prompts []string) {
	if len(prompts) == 0 {
		return
	}
	s.mu.Lock()
	proc, ok := s.processes[invocationID]
	s.mu.Unlock()
	if !ok || proc == nil || proc.Relay == nil {
		return
	}
	requiresAckedTurn := proc.Relay.Mode() == relay.ModeResume
	for _, prompt := range prompts {
		if strings.TrimSpace(prompt) == "" {
			continue
		}
		if requiresAckedTurn {
			proc.IncrementExpectedTurns()
		}
		if err := proc.Relay.Send(context.Background(), prompt); err != nil && requiresAckedTurn {
			proc.DecrementExpectedTurns()
		}
	}
}
