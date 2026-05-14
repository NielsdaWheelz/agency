package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
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
	if err := s.installHeadedRunnerHooks(ctx, repoID, invocationID, canonicalRunner, headedRunnerArgs, meta.SandboxPath); err != nil {
		return fmt.Errorf("install headed runner hooks: %w", err)
	}

	terminalLogPath, err := s.prepareWritableInvocationLogPath(repoID, invocationID, "terminal")
	if err != nil {
		return fmt.Errorf("prepare terminal log: %w", err)
	}
	terminalFile, err := os.OpenFile(terminalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create terminal log: %w", err)
	}
	_ = terminalFile.Close()

	target := sessionName + ":0.0"
	if scrollback, err := s.TmuxClient.CaptureScrollback(ctx, target); err != nil {
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
	if err := s.TmuxClient.PipePane(ctx, target, terminalLogPath); err != nil {
		return fmt.Errorf("pipe tmux pane output: %w", err)
	}

	runnerArgs := append([]string(nil), meta.RunnerArgs...)
	if err := s.claimHeadedInvocation(repoID, invocationID, canonicalRunner, sessionName, runnerArgs, append([]string(nil), meta.CustomEnvKeys...)); err != nil {
		return fmt.Errorf("update invocation meta: %w", err)
	}

	streamLogPath := s.Store.InvocationStreamLogPath(repoID, invocationID)
	parser := stream.NewParser(invocationID, canonicalRunner, s.Clock)
	parser.SetInitialSeq(loadMaxStreamSeq(streamLogPath))
	checkpointsDir := s.Store.InvocationDir(repoID, invocationID)
	eventsPath := s.Store.InvocationEventsPath(repoID, invocationID)
	cpConfig := checkpoint.DefaultConfig()
	cpConfig.IncludeUntracked = meta.CheckpointIncludeUntracked
	if s.CheckpointDebounceOverride != nil {
		cpConfig.DebounceInterval = *s.CheckpointDebounceOverride
		cpConfig.DriftInterval = *s.CheckpointDebounceOverride
	}
	cpEngine := checkpoint.NewEngineWithWriter(
		invocationID,
		repoID,
		meta.SandboxPath,
		repoRoot,
		checkpointsDir,
		eventsPath,
		cpConfig,
		s.Runner,
		s.FS,
		s.Clock,
		s.InvocationEvents,
	)
	cpEngine.SetGitIgnoredDirs(checkpoint.ReadGitIgnoredDirs(meta.SandboxPath))
	triggerCh := make(chan checkpoint.TriggerEvent, 32)
	cpEngine.SetTriggerChannel(triggerCh)

	proc := &SupervisedProcess{
		InvocationID:          invocationID,
		RepoID:                repoID,
		IntegrationWorktreeID: meta.IntegrationWorktreeID,
		Mode:                  "headed",
		TmuxSession:           sessionName,
		SandboxPath:           meta.SandboxPath,
		StreamLogFile:         streamLogPath,
		Runner:                canonicalRunner,
		RepoRoot:              repoRoot,
		RunnerArgs:            runnerArgs,
		NoIncludeUntracked:    !meta.CheckpointIncludeUntracked,
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
			s.recordInvocationWarning(repoID, invocationID, "checkpoint_trigger_dropped", "checkpoint trigger queue full; dropped semantic trigger", map[string]any{
				"seq":       n.Seq,
				"tool_name": n.ToolName,
			})
		}
	})
	parser.SetFinalNotify(func(n stream.FinalNotification) {
		s.handleSuccessfulFinalNotification(proc, n)
	})
	parser.SetSessionStartNotify(func(n stream.SessionStartNotification) {
		proc.SetResumeSessionID(n.SessionID)
	})
	if _, err := s.InvocationEvents.Append(eventsPath, invocationID, eventKind, map[string]any{
		"tmux_session": sessionName,
	}, invocationevents.AppendOptions{}); err != nil {
		return fmt.Errorf("append supervision event: %w", err)
	}

	s.replaceInvocationProcess(invocationID, proc)
	go s.runOutputFlushLoop(proc)
	go s.runCheckpointLoop(proc)
	return nil
}

func (s *Server) resolveHeadedSupervisionRepoRoot(repoID string) (string, error) {
	rec, exists, err := s.Store.LoadRepoRecord(repoID)
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
	clean := filepath.Clean(path)
	if abs, err := filepath.Abs(clean); err == nil {
		clean = abs
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		clean = resolved
	}
	info, err := os.Stat(clean)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return clean, true
}
