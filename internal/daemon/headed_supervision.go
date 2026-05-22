package daemon

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Server) restoreExistingHeadedSupervision(ctx context.Context, repoID, invocationID string, meta *store.InvocationMeta, sessionName, eventKind string) error {
	repoRoot, err := s.resolveHeadedSupervisionRepoRoot(repoID)
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}
	canonicalRunner, err := validateControlPlaneStartRunner(meta.Runner, meta.RunnerArgs, false)
	if err != nil {
		return err
	}
	headedRunnerArgs, err := buildRunnerArgsForHeaded(canonicalRunner, meta.RunnerArgs)
	if err != nil {
		return err
	}
	profileEnv, err := s.executionProfileEnv(meta.ExecutionProfile)
	if err != nil {
		return fmt.Errorf("resolve execution profile env: %w", err)
	}
	env := prSyncNonInteractiveEnv(profileEnv)
	if err := s.installHeadedRunnerHooks(ctx, repoID, invocationID, canonicalRunner, headedRunnerArgs, meta.SandboxPath, env); err != nil {
		return fmt.Errorf("install headed runner hooks: %w", err)
	}

	terminalLogPath, err := s.prepareWritableInvocationLogPath(repoID, invocationID, InvocationLogKindTerminal)
	if err != nil {
		return fmt.Errorf("prepare terminal log: %w", err)
	}
	terminalFile, err := os.OpenFile(terminalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create terminal log: %w", err)
	}
	_ = terminalFile.Close()

	target := sessionName + ":0.0"
	if scrollback, err := s.tmuxClient.CaptureScrollback(ctx, target); err != nil {
		s.recordInvocationWarning(repoID, invocationID, "restore_headed_tmux_capture_failed", err.Error(), map[string]any{
			"target": target,
		})
	} else if scrollback != "" {
		f, err := os.OpenFile(terminalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("append initial terminal capture: %w", err)
		}
		_, writeErr := f.WriteString(scrollback)
		closeErr := f.Close()
		if writeErr != nil {
			return fmt.Errorf("append initial terminal capture: %w", writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close terminal log: %w", closeErr)
		}
	}
	if err := s.tmuxClient.PipePane(ctx, target, terminalLogPath); err != nil {
		return fmt.Errorf("pipe tmux pane output: %w", err)
	}

	runnerArgs := slices.Clone(meta.RunnerArgs)
	if err := s.claimHeadedInvocation(repoID, invocationID, canonicalRunner, sessionName, runnerArgs, slices.Clone(meta.CustomEnvKeys)); err != nil {
		return fmt.Errorf("update invocation meta: %w", err)
	}

	streamLogPath := s.store.InvocationStreamLogPath(repoID, invocationID)
	parser := stream.NewParser(invocationID, canonicalRunner, s.clock)
	parser.SetInitialSeq(loadMaxStreamSeq(streamLogPath))
	checkpointsDir := s.store.InvocationDir(repoID, invocationID)
	eventsPath := s.store.InvocationEventsPath(repoID, invocationID)
	cpConfig := checkpoint.DefaultConfig()
	cpConfig.IncludeUntracked = meta.CheckpointIncludeUntracked
	cpConfig.Env = env
	cpEngine := checkpoint.NewEngineWithWriter(
		invocationID,
		repoID,
		meta.SandboxPath,
		repoRoot,
		checkpointsDir,
		eventsPath,
		cpConfig,
		s.runner,
		s.fsys,
		s.clock,
		s.invocationEvents,
	)
	s.configureCheckpointIgnoredDirs(daemonOwnedContext(ctx), repoID, invocationID, cpEngine, meta.SandboxPath, cpConfig.Env)
	proc := &supervisedProcess{
		invocationID:          invocationID,
		repoID:                repoID,
		integrationWorktreeID: meta.IntegrationWorktreeID,
		mode:                  "headed",
		tmuxSession:           sessionName,
		sandboxPath:           meta.SandboxPath,
		runner:                canonicalRunner,
		repoRoot:              repoRoot,
		runnerArgs:            runnerArgs,
		noIncludeUntracked:    !meta.CheckpointIncludeUntracked,
		parser:                parser,
		checkpointEngine:      cpEngine,
		done:                  make(chan struct{}),
	}
	s.attachCheckpointTriggers(repoID, invocationID, parser, cpEngine)
	parser.SetFinalNotify(func(n stream.FinalNotification) {
		s.handleSuccessfulFinalNotification(proc, n)
	})
	parser.SetSessionStartNotify(func(n stream.SessionStartNotification) {
		proc.setResumeSessionID(n.SessionID)
	})
	if _, err := s.invocationEvents.Append(eventsPath, invocationID, eventKind, map[string]any{
		"tmux_session": sessionName,
	}, eventlog.AppendOptions{}); err != nil {
		return fmt.Errorf("append supervision event: %w", err)
	}

	s.replaceInvocationProcess(invocationID, proc)
	s.supervisionWg.Add(2)
	go s.runOutputFlushLoop(proc)
	go s.runCheckpointLoop(proc)
	return nil
}

func (s *Server) resolveHeadedSupervisionRepoRoot(repoID string) (string, error) {
	rec, exists, err := s.store.LoadRepoRecord(repoID)
	if err != nil {
		return "", err
	}
	if exists {
		for _, root := range []string{rec.PreferredRoot, rec.RepoRootLastSeen} {
			if resolved, ok := canonicalAccessibleDir(root); ok {
				return resolved, nil
			}
		}
		return "", fmt.Errorf("repo %s stored roots are not accessible", repoID)
	}
	return "", fmt.Errorf("repo %s not found", repoID)
}

func canonicalAccessibleDir(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	clean := agencyfs.CanonicalizePath(path)
	info, err := os.Stat(clean)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return clean, true
}
