package daemon

import (
	"log"
	"os"
	"time"

	"github.com/NielsdaWheelz/agency/internal/store"
)

const daemonLifecycleOwner = "daemon"

func (s *Server) nowRFC3339() string {
	return s.clock().UTC().Format(time.RFC3339)
}

// persistInvocationMeta applies a terminal-state update and logs — rather than
// silently dropping — a persistence failure. Used by the fire-and-forget
// lifecycle transitions below, which run with no error channel to a caller.
func (s *Server) persistInvocationMeta(repoID, invocationID string, mutate func(*store.InvocationMeta)) {
	if _, err := s.store.UpdateInvocationMeta(repoID, invocationID, mutate); err != nil {
		log.Printf("agencyd: persist invocation %s/%s lifecycle state: %v", repoID, invocationID, err)
	}
}

func (s *Server) requestInvocationStop(repoID, invocationID string) {
	now := s.nowRFC3339()
	s.persistInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		if meta.StopRequestedAt == "" {
			meta.StopRequestedAt = now
		}
		meta.Status = store.InvocationStatusStopping
		meta.Flags.NeedsAttention = true
	})
}

func (s *Server) claimHeadlessInvocationStart(repoID, invocationID, taskID, runner string, pid, pgid int, promptPath, promptSHA string, runnerArgs, envKeys []string) error {
	now := s.nowRFC3339()
	daemonPID := os.Getpid()
	_, err := s.store.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.Runner = runner
		meta.PID = &pid
		meta.PGID = &pgid
		meta.DaemonPID = &daemonPID
		meta.DaemonInstanceID = s.instanceID
		meta.ClaimedAt = now
		meta.LifecycleOwner = daemonLifecycleOwner
		meta.PromptPath = promptPath
		meta.PromptSHA256 = promptSHA
		meta.RunnerArgs = runnerArgs
		meta.CustomEnvKeys = envKeys
		if taskID != "" {
			meta.TaskID = taskID
		}
		meta.FinishedAt = ""
		meta.ExitReason = ""
		meta.FailureReason = ""
		meta.ExitCode = nil
		meta.StopRequestedAt = ""
		meta.OrphanedAt = ""
		meta.Flags.NeedsAttention = false
		meta.Flags.Orphaned = false
	})
	return err
}

func (s *Server) claimHeadlessInvocationResume(repoID, invocationID string, pid, pgid int) error {
	now := s.nowRFC3339()
	daemonPID := os.Getpid()
	_, err := s.store.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.PID = &pid
		meta.PGID = &pgid
		meta.DaemonPID = &daemonPID
		meta.DaemonInstanceID = s.instanceID
		meta.ClaimedAt = now
		meta.LifecycleOwner = daemonLifecycleOwner
		meta.FinishedAt = ""
		meta.ExitReason = ""
		meta.FailureReason = ""
		meta.ExitCode = nil
		meta.StopRequestedAt = ""
		meta.OrphanedAt = ""
		meta.Flags.NeedsAttention = false
		meta.Flags.Orphaned = false
	})
	return err
}

func (s *Server) claimHeadedInvocation(repoID, invocationID, taskID, runner, sessionName string, runnerArgs, envKeys []string) (*store.InvocationMeta, error) {
	now := s.nowRFC3339()
	daemonPID := os.Getpid()
	return s.store.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusRunning
		meta.Runner = runner
		meta.RunnerArgs = runnerArgs
		meta.CustomEnvKeys = envKeys
		meta.TmuxSession = sessionName
		meta.PID = nil
		meta.PGID = nil
		meta.DaemonPID = &daemonPID
		meta.DaemonInstanceID = s.instanceID
		meta.ClaimedAt = now
		meta.LifecycleOwner = daemonLifecycleOwner
		if taskID != "" {
			meta.TaskID = taskID
		}
		meta.FinishedAt = ""
		meta.ExitReason = ""
		meta.FailureReason = ""
		meta.ExitCode = nil
		meta.StopRequestedAt = ""
		meta.OrphanedAt = ""
		meta.Flags.NeedsAttention = false
		meta.Flags.Orphaned = false
	})
}

func (s *Server) finishInvocationExited(repoID, invocationID string) {
	now := s.nowRFC3339()
	s.persistInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFinished
		meta.ExitReason = store.ExitReasonExited
		meta.FailureReason = ""
		meta.FinishedAt = now
		meta.ExitCode = nil
		meta.PID = nil
		meta.PGID = nil
		meta.LifecycleOwner = ""
	})
}

func (s *Server) failInvocationStart(repoID, invocationID, failureReason string, needsAttention bool) {
	now := s.nowRFC3339()
	s.persistInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFailed
		meta.ExitReason = store.ExitReasonStartFailed
		meta.FailureReason = failureReason
		meta.FinishedAt = now
		meta.ExitCode = nil
		meta.PID = nil
		meta.PGID = nil
		meta.LifecycleOwner = ""
		meta.Flags.NeedsAttention = needsAttention
	})
}

func (s *Server) failInvocationStopped(repoID, invocationID, exitReason string) {
	now := s.nowRFC3339()
	s.persistInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFailed
		meta.ExitReason = exitReason
		meta.FailureReason = "stopped"
		meta.FinishedAt = now
		meta.ExitCode = nil
		meta.PID = nil
		meta.PGID = nil
		meta.LifecycleOwner = ""
	})
}

func (s *Server) failInvocationKilled(repoID, invocationID string) {
	now := s.nowRFC3339()
	s.persistInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFailed
		meta.ExitReason = store.ExitReasonKilled
		meta.FailureReason = "killed"
		meta.FinishedAt = now
		meta.ExitCode = nil
		meta.PID = nil
		meta.PGID = nil
		meta.LifecycleOwner = ""
	})
}

func (s *Server) failInvocationUnknown(repoID, invocationID, failureReason string, orphaned bool) {
	now := s.nowRFC3339()
	s.persistInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFailed
		meta.ExitReason = store.ExitReasonUnknown
		meta.FailureReason = failureReason
		meta.FinishedAt = now
		meta.ExitCode = nil
		meta.PID = nil
		meta.PGID = nil
		meta.LifecycleOwner = ""
		meta.Flags.NeedsAttention = true
		if orphaned {
			meta.Flags.Orphaned = true
			meta.OrphanedAt = now
		}
	})
}

func (s *Server) markInvocationOrphaned(repoID, invocationID string) {
	now := s.nowRFC3339()
	s.persistInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Flags.NeedsAttention = true
		meta.Flags.Orphaned = true
		meta.OrphanedAt = now
	})
}

func (s *Server) writeInvocationProcessExit(repoID, invocationID string, status store.InvocationStatus, exitReason, failureReason string, exitCode int) error {
	now := s.nowRFC3339()
	_, err := s.store.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = status
		meta.ExitReason = exitReason
		meta.FailureReason = failureReason
		meta.ExitCode = &exitCode
		meta.FinishedAt = now
		meta.PID = nil
		meta.PGID = nil
		meta.LifecycleOwner = ""
	})
	return err
}

func (s *Server) clearInvocationProcess(invocationID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if proc, ok := s.processes[invocationID]; ok {
		proc.closeDone()
		delete(s.processes, invocationID)
	}
}

func (s *Server) clearInvocationProcessIfCurrent(invocationID string, current *supervisedProcess) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if proc, ok := s.processes[invocationID]; ok && proc == current {
		delete(s.processes, invocationID)
	}
}

func (s *Server) replaceInvocationProcess(invocationID string, proc *supervisedProcess) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if current, ok := s.processes[invocationID]; ok {
		current.closeDone()
	}
	s.processes[invocationID] = proc
}
