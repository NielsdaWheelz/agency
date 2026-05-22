package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/relay"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/jsonl"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func sortedEnvKeys(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(env))
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

func (s *Server) startRunner(ctx context.Context, repoID string, result *invocation.CreateResult, repoRoot, integrationWorktreeID string, req ControlPlaneStartRequest, gitEnv map[string]string, claim func(pid, pgid int) error) (int, int, error) {
	args, err := runners.BuildHeadlessArgs(req.Runner, req.Prompt, result.SandboxPath, req.RunnerArgs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to build runner args: %w", err)
	}
	return s.startRunnerWithArgs(ctx, repoID, result, repoRoot, integrationWorktreeID, req, args, "", gitEnv, claim)
}

func (s *Server) startRunnerResumeTurn(ctx context.Context, proc *supervisedProcess, prompt string) (int, int, error) {
	meta, err := s.store.ReadInvocationMeta(proc.repoID, proc.invocationID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read invocation meta: %w", err)
	}
	profileEnv, err := s.executionProfileEnv(meta.ExecutionProfile)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to resolve execution profile env: %w", err)
	}
	req := ControlPlaneStartRequest{
		Runner:             proc.runner,
		Prompt:             prompt,
		RunnerArgs:         slices.Clone(proc.runnerArgs),
		ExecutionProfile:   meta.ExecutionProfile,
		NoIncludeUntracked: proc.noIncludeUntracked,
	}
	requestEnv := map[string]string{}
	for _, key := range meta.CustomEnvKeys {
		if value, ok := proc.env[key]; ok {
			requestEnv[key] = value
		}
	}
	req.Env = envForLaunch(profileEnv, requestEnv)
	resumeSessionID := proc.getResumeSessionID()
	args, err := runners.BuildResumeArgs(proc.runner, prompt, resumeSessionID, req.RunnerArgs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to build runner resume args: %w", err)
	}
	return s.startRunnerWithArgs(ctx, proc.repoID, &invocation.CreateResult{
		InvocationID: proc.invocationID,
		SandboxPath:  proc.sandboxPath,
	}, proc.repoRoot, proc.integrationWorktreeID, req, args, resumeSessionID, prSyncNonInteractiveEnv(profileEnv), func(pid, pgid int) error {
		return s.claimHeadlessInvocationResume(proc.repoID, proc.invocationID, pid, pgid)
	})
}

func (s *Server) startRunnerWithArgs(ctx context.Context, repoID string, result *invocation.CreateResult, repoRoot, integrationWorktreeID string, req ControlPlaneStartRequest, args []string, resumeSessionID string, gitEnv map[string]string, claim func(pid, pgid int) error) (int, int, error) {
	userCfg, err := s.LoadUserConfig()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to load user config: %w", err)
	}

	runnerCmd, err := config.ResolveRunnerCmd(s.runner, s.fsys, s.configDir, userCfg, req.Runner)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to resolve runner command: %w", err)
	}

	logFiles, err := s.openInvocationLogFiles(repoID, result.InvocationID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open invocation log files: %w", err)
	}
	streamLogPath := logFiles.StreamPath
	rawFile := logFiles.RawFile
	stderrFile := logFiles.StderrFile
	streamFile := logFiles.StreamFile

	envOverlay := copyStringMap(req.Env)
	if envOverlay == nil {
		envOverlay = map[string]string{}
	}
	for k, v := range exec.NonInteractiveEnv() {
		envOverlay[k] = v
	}

	var stdinReader *os.File
	var followUpRelay relay.FollowUpRelay
	initialPromptFromStdin := false
	if followUpMode, promptMode, err := runners.FollowUpPolicy(req.Runner); err == nil {
		initialPromptFromStdin = promptMode == runners.InitialPromptStdin
		switch followUpMode {
		case runners.FollowUpModeStdin:
			pr, pw, err := os.Pipe()
			if err != nil {
				s.recordInvocationWarning(repoID, result.InvocationID, "followup_relay_setup_failed", fmt.Errorf("failed to create stdin pipe for follow-up relay: %w", err).Error(), nil)
			} else {
				stdinReader = pr
				followUpRelay = relay.NewStdinRelay(pw, req.Runner)
			}
		case runners.FollowUpModeResume:
			followUpRelay = relay.NewResumeRelay(req.Runner)
		}
	}
	runnerCtx := daemonOwnedContext(ctx)
	startedProc, err := exec.StartProcess(runnerCtx, runnerCmd, args, exec.StartOpts{
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
		if followUpRelay != nil {
			_ = followUpRelay.Close()
		}
		logFiles.Close()
		return 0, 0, fmt.Errorf("failed to start runner: %w", err)
	}
	if followUpRelay != nil {
		if initialPromptFromStdin {
			if err := followUpRelay.Send(runnerCtx, req.Prompt); err != nil {
				_ = followUpRelay.Close()
				if startedProc.PGID > 0 {
					_ = syscall.Kill(-startedProc.PGID, syscall.SIGKILL)
				}
				logFiles.Close()
				return 0, 0, fmt.Errorf("failed to deliver initial prompt: %w", err)
			}
		}
	}

	pid := startedProc.PID
	pgid := startedProc.PGID
	parser := stream.NewParser(result.InvocationID, req.Runner, s.clock)
	parser.SetInitialSeq(loadMaxStreamSeq(streamLogPath))

	checkpointsDir := s.store.InvocationDir(repoID, result.InvocationID)
	eventsPath := s.store.InvocationEventsPath(repoID, result.InvocationID)
	cpConfig := checkpoint.DefaultConfig()
	cpConfig.IncludeUntracked = !req.NoIncludeUntracked
	cpConfig.Env = gitEnv

	cpEngine := checkpoint.NewEngineWithWriter(
		result.InvocationID,
		repoID,
		result.SandboxPath,
		repoRoot,
		checkpointsDir,
		eventsPath,
		cpConfig,
		s.runner,
		s.fsys,
		s.clock,
		s.invocationEvents,
	)
	s.configureCheckpointIgnoredDirs(runnerCtx, repoID, result.InvocationID, cpEngine, result.SandboxPath, cpConfig.Env)

	s.attachCheckpointTriggers(repoID, result.InvocationID, parser, cpEngine)

	proc := &supervisedProcess{
		invocationID:          result.InvocationID,
		repoID:                repoID,
		integrationWorktreeID: integrationWorktreeID,
		pgid:                  pgid,
		sandboxPath:           result.SandboxPath,
		runner:                req.Runner,
		repoRoot:              repoRoot,
		runnerArgs:            slices.Clone(req.RunnerArgs),
		env:                   copyStringMap(req.Env),
		noIncludeUntracked:    req.NoIncludeUntracked,
		parser:                parser,
		checkpointEngine:      cpEngine,
		relay:                 followUpRelay,
		done:                  make(chan struct{}),
	}
	if proc.relay != nil {
		proc.initializeTurnTracking()
	}
	proc.setResumeSessionID(resumeSessionID)
	parser.SetFinalNotify(func(n stream.FinalNotification) {
		s.handleSuccessfulFinalNotification(proc, n)
	})
	parser.SetSessionStartNotify(func(n stream.SessionStartNotification) {
		proc.setResumeSessionID(n.SessionID)
	})

	s.mu.Lock()
	s.processes[result.InvocationID] = proc
	s.mu.Unlock()

	if claim != nil {
		if err := claim(pid, pgid); err != nil {
			s.clearInvocationProcessIfCurrent(result.InvocationID, proc)
			if pgid > 0 {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
			if proc.relay != nil {
				_ = proc.relay.Close()
			}
			logFiles.Close()
			return 0, 0, err
		}
	}

	proc.streamWg.Add(2)
	s.supervisionWg.Add(3)
	go s.streamAndParseOutput(proc, startedProc.StdoutPipe, rawFile, streamFile)
	go s.streamOutput(proc, startedProc.StderrPipe, stderrFile)
	go s.waitForExitWithFailureReason(proc, startedProc, rawFile, stderrFile, streamFile)
	go s.runOutputFlushLoop(proc)
	go s.runCheckpointLoop(proc)

	return pid, pgid, nil
}

func buildRunnerArgsForHeaded(runner string, extraArgs []string) ([]string, error) {
	return runners.BuildHeadedArgs(runner, extraArgs)
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	return maps.Clone(in)
}

func (s *Server) cleanupFailedInvocation(ctx context.Context, repoID string, result *invocation.CreateResult, repoRoot, failureReason string, env map[string]string) {
	s.failInvocationStart(repoID, result.InvocationID, failureReason, true)

	args := []string{"-C", repoRoot, "worktree", "remove", "--force", result.SandboxPath}
	_, _ = s.runner.Run(ctx, "git", args, exec.RunOpts{Env: env})
	args = []string{"-C", repoRoot, "branch", "-D", result.SandboxBranch}
	_, _ = s.runner.Run(ctx, "git", args, exec.RunOpts{Env: env})
}

func (s *Server) waitForExitWithFailureReason(proc *supervisedProcess, startedProc *exec.StartedProcess, rawFile, stderrFile, streamFile *os.File) {
	defer s.supervisionWg.Done()
	defer func() { _ = rawFile.Close() }()
	defer func() { _ = stderrFile.Close() }()
	defer func() {
		if streamFile != nil {
			_ = streamFile.Close()
		}
	}()
	defer proc.closeDone()
	defer func() {
		if proc.relay != nil {
			_ = proc.relay.Close()
		}
	}()

	proc.streamWg.Wait()
	exitResult, waitErr := startedProc.WaitExit()
	if proc.parser != nil {
		proc.parser.Stop()
	}

	exitCode := exitResult.ExitCode
	exitReason := "exited"
	failureReason := ""
	status := store.InvocationStatusFinished
	completionSatisfied := proc.successfulCompletionObserved()
	if !completionSatisfied && (waitErr != nil || exitResult.ExitCode != 0 || exitResult.Signal != "") {
		status = store.InvocationStatusFailed
		failureReason = "runner_exit_nonzero"
	}

	if override, ok := proc.exitReason.Load().(string); ok && override != "" {
		if !completionSatisfied || override != "stopped" {
			exitReason = override
			status = store.InvocationStatusFailed
		}
	}
	if override, ok := proc.failureReason.Load().(string); ok && override != "" {
		if !completionSatisfied || override != "stopped" {
			failureReason = override
		}
	}
	if completionSatisfied && status != store.InvocationStatusFailed {
		status = store.InvocationStatusFinished
		failureReason = ""
		exitCode = 0
	}

	var queuedResumePrompts []string
	if resumeRelay, ok := proc.relay.(*relay.ResumeRelay); ok {
		queuedResumePrompts = resumeRelay.Drain()
	}

	if status == store.InvocationStatusFinished && len(queuedResumePrompts) > 0 && runners.SupportsResumeTurns(proc.runner) {
		_, _, resumeErr := s.startRunnerResumeTurn(context.Background(), proc, queuedResumePrompts[0])
		if resumeErr != nil {
			status = store.InvocationStatusFailed
			failureReason = "runner_resume_failed"
		} else {
			if len(queuedResumePrompts) > 1 {
				s.enqueueFollowUpPrompts(proc.invocationID, queuedResumePrompts[1:])
			}
			return
		}
	}

	if err := s.writeInvocationProcessExit(proc.repoID, proc.invocationID, status, exitReason, failureReason, exitCode); err != nil {
		s.recordInvocationWarning(proc.repoID, proc.invocationID, "meta_update_on_exit_failed", err.Error(), nil)
	}

	s.clearInvocationProcessIfCurrent(proc.invocationID, proc)
}

func (s *Server) enqueueFollowUpPrompts(invocationID string, prompts []string) {
	if len(prompts) == 0 {
		return
	}
	s.mu.Lock()
	proc, ok := s.processes[invocationID]
	s.mu.Unlock()
	if !ok || proc == nil || proc.relay == nil {
		return
	}
	requiresAckedTurn := proc.relay.Mode() == relay.ModeResume
	for _, prompt := range prompts {
		if strings.TrimSpace(prompt) == "" {
			continue
		}
		if requiresAckedTurn {
			proc.incrementExpectedTurns()
		}
		if err := proc.relay.Send(context.Background(), prompt); err != nil && requiresAckedTurn {
			proc.decrementExpectedTurns()
		}
	}
}
