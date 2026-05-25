package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// recoveryScanTimeout bounds the per-repo recovery scan that runs at daemon
// startup before Serve takes traffic; a slow tmux probe must not block startup
// indefinitely.
const recoveryScanTimeout = 30 * time.Second

func (s *Server) runRecoveryScan() error {
	repoIDs, err := s.discoverDurableRepoIDs()
	if err != nil {
		return err
	}

	for _, repoID := range repoIDs {
		if err := s.recoverRepoInvocations(repoID); err != nil {
			return fmt.Errorf("recover repo %s: %w", repoID, err)
		}
		if err := s.recoverRepoWorktreeMerges(repoID); err != nil {
			return fmt.Errorf("recover repo worktree merges %s: %w", repoID, err)
		}
	}
	return nil
}

func (s *Server) discoverDurableRepoIDs() ([]string, error) {
	reposDir := filepath.Join(s.store.DataDir, "repos")
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	repoIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			repoIDs = append(repoIDs, entry.Name())
		}
	}
	slices.Sort(repoIDs)
	return repoIDs, nil
}

func (s *Server) recoverRepoInvocations(repoID string) error {
	records, err := s.store.ScanInvocationsForRepo(repoID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), recoveryScanTimeout)
	defer cancel()

	nowTime := s.clock()
	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}
		if r.Meta.Mode == store.RunnerModeHeaded {
			s.recoverHeadedInvocation(ctx, repoID, r, nowTime)
			continue
		}
		if r.Meta.Status == store.InvocationStatusStarting {
			startedAt, err := time.Parse(time.RFC3339, r.Meta.StartedAt)
			if err == nil && nowTime.Sub(startedAt) > 60*time.Second && r.Meta.PID == nil {
				s.failInvocationStart(repoID, r.InvocationID, "start_incomplete", true)
			}
			continue
		}
		if r.Meta.Status != store.InvocationStatusRunning &&
			r.Meta.Status != store.InvocationStatusStopping {
			continue
		}
		if r.Meta.PID == nil {
			if r.Meta.Status == store.InvocationStatusStopping {
				s.failInvocationStopped(repoID, r.InvocationID, "stopped")
			} else {
				s.failInvocationUnknown(repoID, r.InvocationID, "runner_pid_missing", true)
			}
			continue
		}

		pid := *r.Meta.PID
		if r.Meta.Status == store.InvocationStatusStopping {
			if !s.pidChecker(pid) {
				s.failInvocationStopped(repoID, r.InvocationID, "stopped")
				continue
			}

			pgid := safeIntPtr(r.Meta.PGID)
			if pgid <= 0 {
				pgid = pid
			}
			if pgid <= 0 {
				s.failInvocationStopped(repoID, r.InvocationID, "stopped")
				continue
			}

			go s.stopEscalation(repoID, r.InvocationID, pgid, false, nil)
			continue
		}
		if !s.pidChecker(pid) {
			s.failInvocationUnknown(repoID, r.InvocationID, "runner_exit_nonzero", true)
			continue
		}
		if r.Meta.DaemonInstanceID != s.instanceID {
			s.markInvocationOrphaned(repoID, r.InvocationID)
		}
	}
	return nil
}

func (s *Server) recoverHeadedInvocation(ctx context.Context, repoID string, r store.InvocationRecord, nowTime time.Time) {
	if r.Meta.Status != store.InvocationStatusStarting &&
		r.Meta.Status != store.InvocationStatusRunning &&
		r.Meta.Status != store.InvocationStatusStopping {
		return
	}

	sessionName, ok := headedInvocationSessionName(r.Meta)
	if !ok {
		s.failInvocationStart(repoID, r.InvocationID, "tmux_session_missing", false)
		s.clearInvocationProcess(r.InvocationID)
		return
	}
	exists, err := s.tmuxClient.HasSession(ctx, sessionName)
	if err != nil && !tmux.IsNoSessionErr(err) {
		s.recordInvocationWarning(repoID, r.InvocationID, "recovery_tmux_has_session_failed", err.Error(), map[string]any{
			"session_name": sessionName,
		})
		return
	}
	if exists {
		s.mu.RLock()
		_, supervised := s.processes[r.InvocationID]
		s.mu.RUnlock()
		restoreSupervision := !supervised && (r.Meta.Status == store.InvocationStatusRunning || r.Meta.Status == store.InvocationStatusStopping)
		if !restoreSupervision && !supervised && r.Meta.Status == store.InvocationStatusStarting {
			startedAt, parseErr := time.Parse(time.RFC3339, r.Meta.StartedAt)
			restoreSupervision = parseErr == nil && nowTime.Sub(startedAt) > 30*time.Second
		}
		if restoreSupervision {
			if _, err := s.restoreExistingHeadedSupervision(ctx, repoID, r.InvocationID, r.Meta, sessionName, "agency.headed_supervision_recovered"); err != nil {
				s.recordInvocationWarning(repoID, r.InvocationID, "recovery_headed_supervision_restore_failed", err.Error(), map[string]any{
					"session_name": sessionName,
				})
			}
		}
		if r.Meta.Status == store.InvocationStatusStopping && !supervised {
			if err := s.tmuxClient.InterruptSession(ctx, sessionName); err != nil {
				s.recordInvocationWarning(repoID, r.InvocationID, "recovery_headed_stop_interrupt_failed", err.Error(), map[string]any{
					"session_name": sessionName,
				})
			}
		}
		return
	}

	switch r.Meta.Status {
	case store.InvocationStatusStarting:
		startedAt, parseErr := time.Parse(time.RFC3339, r.Meta.StartedAt)
		if parseErr != nil {
			s.failInvocationStart(repoID, r.InvocationID, "tmux_session_missing", false)
			return
		}
		if nowTime.Sub(startedAt) > 30*time.Second {
			s.failInvocationStart(repoID, r.InvocationID, "tmux_session_missing", false)
		}
	case store.InvocationStatusRunning:
		s.finishInvocationExited(repoID, r.InvocationID)
	case store.InvocationStatusStopping:
		s.failInvocationStopped(repoID, r.InvocationID, "stopped")
	}
}

func (s *Server) recoverRepoWorktreeMerges(repoID string) error {
	records, err := s.store.ScanIntegrationWorktreesForRepo(repoID)
	if err != nil {
		return err
	}

	now := s.nowRFC3339()
	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}

		mergeMeta, err := s.store.ReadIntegrationWorktreeMerge(repoID, r.WorktreeID)
		if err != nil {
			return err
		}
		if mergeMeta == nil {
			continue
		}
		if mergeMeta.Status != store.WorktreeMergeStatusRunning {
			continue
		}

		if err := s.store.UpdateIntegrationWorktreeMerge(repoID, r.WorktreeID, func(m *store.IntegrationWorktreeMergeMeta) {
			m.Status = store.WorktreeMergeStatusFailed
			m.UpdatedAt = now
			m.FinishedAt = now
			m.ErrorCode = string(errors.EWorktreeMergeInterrupted)
			m.ErrorMessage = "merge attempt lost daemon supervision before reaching a terminal state"
			m.Hint = "rerun 'agency worktree <worktree-ref> pr merge' to resume from durable state"
		}); err != nil {
			return err
		}
		if err := s.appendWorktreeEvent(repoID, r.WorktreeID, mergeEventFailed, map[string]any{
			"error_code": string(errors.EWorktreeMergeInterrupted),
			"message":    "merge attempt lost daemon supervision before reaching a terminal state",
		}); err != nil {
			return err
		}
	}
	return nil
}

func WritePidFile(pidPath string) error {
	dir := filepath.Dir(pidPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

func ReadPidFile(pidPath string) (int, error) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data[:len(data)-1]))
}
