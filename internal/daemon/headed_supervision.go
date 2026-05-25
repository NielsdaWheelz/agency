package daemon

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// restoreExistingHeadedSupervision reattaches an existing tmux session to a
// supervised process and returns the post-claim invocation meta.
func (s *Server) restoreExistingHeadedSupervision(ctx context.Context, repoID, invocationID string, meta *store.InvocationMeta, sessionName, eventKind string) (*store.InvocationMeta, error) {
	repoRoot, err := s.resolveRegisteredRepoRoot(repoID)
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}
	canonicalRunner, err := validateControlPlaneStartRunner(meta.Runner, meta.RunnerArgs, false)
	if err != nil {
		return nil, err
	}
	headedRunnerArgs, err := buildRunnerArgsForHeaded(canonicalRunner, meta.RunnerArgs)
	if err != nil {
		return nil, err
	}
	profileEnv, err := s.executionProfileEnv(meta.ExecutionProfile)
	if err != nil {
		return nil, fmt.Errorf("resolve execution profile env: %w", err)
	}
	env := withNonInteractiveEnv(profileEnv)
	if err := s.installHeadedRunnerHooks(ctx, repoID, invocationID, canonicalRunner, headedRunnerArgs, meta.SandboxPath, env); err != nil {
		return nil, fmt.Errorf("install headed runner hooks: %w", err)
	}

	terminalLogPath, err := s.prepareWritableInvocationLogPath(repoID, invocationID, InvocationLogKindTerminal)
	if err != nil {
		return nil, fmt.Errorf("prepare terminal log: %w", err)
	}
	terminalFile, err := os.OpenFile(terminalLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create terminal log: %w", err)
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
			return nil, fmt.Errorf("append initial terminal capture: %w", err)
		}
		_, writeErr := f.WriteString(scrollback)
		closeErr := f.Close()
		if writeErr != nil {
			return nil, fmt.Errorf("append initial terminal capture: %w", writeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close terminal log: %w", closeErr)
		}
	}
	if err := s.tmuxClient.PipePane(ctx, target, terminalLogPath); err != nil {
		return nil, fmt.Errorf("pipe tmux pane output: %w", err)
	}

	runnerArgs := slices.Clone(meta.RunnerArgs)
	updatedMeta, err := s.claimHeadedInvocation(repoID, invocationID, "", canonicalRunner, sessionName, runnerArgs, slices.Clone(meta.CustomEnvKeys))
	if err != nil {
		return nil, fmt.Errorf("update invocation meta: %w", err)
	}

	proc := s.buildSupervisedHeadedProcess(ctx, supervisedHeadedSetup{
		invocationID:          invocationID,
		repoID:                repoID,
		integrationWorktreeID: meta.IntegrationWorktreeID,
		repoRoot:              repoRoot,
		sandboxPath:           meta.SandboxPath,
		sessionName:           sessionName,
		runner:                canonicalRunner,
		runnerArgs:            runnerArgs,
		includeUntracked:      meta.CheckpointIncludeUntracked,
		gitEnv:                env,
	})
	if _, err := s.invocationEvents.Append(s.store.InvocationEventsPath(repoID, invocationID), invocationID, eventKind, map[string]any{
		"tmux_session": sessionName,
	}, eventlog.AppendOptions{}); err != nil {
		return nil, fmt.Errorf("append supervision event: %w", err)
	}
	s.launchSupervisedHeadedProcess(proc)
	return updatedMeta, nil
}

// supervisedHeadedSetup carries the parameters needed to bring a headed
// invocation under supervision.
type supervisedHeadedSetup struct {
	invocationID          string
	repoID                string
	integrationWorktreeID string
	repoRoot              string
	sandboxPath           string
	sessionName           string
	runner                string
	runnerArgs            []string
	launchEnv             map[string]string
	includeUntracked      bool
	gitEnv                map[string]string
}

// buildSupervisedHeadedProcess assembles the supervised process for a headed
// invocation: stream parser, checkpoint engine, and a supervisedProcess wired
// to its triggers and notifiers. The returned proc is not yet registered;
// callers should append any pre-launch events and then call
// launchSupervisedHeadedProcess.
func (s *Server) buildSupervisedHeadedProcess(ctx context.Context, setup supervisedHeadedSetup) *supervisedProcess {
	streamLogPath := s.store.InvocationStreamLogPath(setup.repoID, setup.invocationID)
	parser, cpEngine := s.buildParserAndCheckpointEngine(daemonOwnedContext(ctx), setup.repoID, setup.invocationID, setup.sandboxPath, setup.repoRoot, setup.runner, streamLogPath, setup.gitEnv, !setup.includeUntracked)
	proc := &supervisedProcess{
		invocationID:          setup.invocationID,
		repoID:                setup.repoID,
		integrationWorktreeID: setup.integrationWorktreeID,
		mode:                  "headed",
		tmuxSession:           setup.sessionName,
		sandboxPath:           setup.sandboxPath,
		runner:                setup.runner,
		repoRoot:              setup.repoRoot,
		runnerArgs:            setup.runnerArgs,
		env:                   copyStringMap(setup.launchEnv),
		noIncludeUntracked:    !setup.includeUntracked,
		parser:                parser,
		checkpointEngine:      cpEngine,
		done:                  make(chan struct{}),
	}
	parser.SetFinalNotify(func(n stream.FinalNotification) {
		s.handleSuccessfulFinalNotification(proc, n)
	})
	parser.SetSessionStartNotify(func(n stream.SessionStartNotification) {
		proc.setResumeSessionID(n.SessionID)
	})
	return proc
}

// launchSupervisedHeadedProcess registers a built proc with the server and
// starts its output and checkpoint supervision goroutines.
func (s *Server) launchSupervisedHeadedProcess(proc *supervisedProcess) {
	s.replaceInvocationProcess(proc.invocationID, proc)
	s.supervisionWg.Add(2)
	go s.runOutputFlushLoop(proc)
	go s.runCheckpointLoop(proc)
}

// resolveRegisteredRepoRoot loads a registered repo and returns its first
// accessible root (preferred, then last-seen). Returns ERepoNotFound when the
// repo has no record and ERepoRootInaccessible when both stored roots are
// missing or non-directories.
func (s *Server) resolveRegisteredRepoRoot(repoID string) (string, error) {
	rec, exists, err := s.store.LoadRepoRecord(repoID)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", errors.New(errors.ERepoNotFound, "repo "+repoID+" not found")
	}
	for _, root := range []string{rec.PreferredRoot, rec.RepoRootLastSeen} {
		if resolved, ok := canonicalAccessibleDir(root); ok {
			return resolved, nil
		}
	}
	return "", errors.New(errors.ERepoRootInaccessible, "repo "+repoID+" stored roots are not accessible")
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
