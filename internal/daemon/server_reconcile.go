package daemon

import (
	"context"
	"time"

	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

func (s *Server) runHeadedReconcileLoop() {
	defer close(s.reconcileLoopDone)

	ticker := time.NewTicker(headedReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			s.reconcileHeadedInvocations()
		}
	}
}

func (s *Server) reconcileHeadedInvocations() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repoIDs, err := s.discoverDurableRepoIDs()
	if err != nil {
		return
	}
	for _, repoID := range repoIDs {
		s.reconcileHeadedInvocationsForRepo(ctx, repoID)
	}
}

func (s *Server) reconcileHeadedInvocationsForRepo(ctx context.Context, repoID string) {
	records, err := s.store.ScanInvocationsForRepo(repoID)
	if err != nil {
		return
	}

	now := s.clock().UTC().Format(time.RFC3339)
	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}
		if r.Meta.Status != store.InvocationStatusStarting &&
			r.Meta.Status != store.InvocationStatusRunning &&
			r.Meta.Status != store.InvocationStatusStopping {
			s.cleanupHeadedStartingTracking(r.InvocationID)
			continue
		}
		switch r.Meta.Mode {
		case store.RunnerModeHeaded:
			s.reconcileHeadedInvocation(ctx, repoID, r, now)
		case store.RunnerModeHeadless:
			s.reconcileHeadlessInvocation(repoID, r, now)
		}
	}
}

func (s *Server) reconcileHeadedInvocation(ctx context.Context, repoID string, r store.InvocationRecord, now string) {
	sessionName, ok := headedInvocationSessionName(r.Meta)
	if !ok {
		s.failInvocationStart(repoID, r.InvocationID, "tmux_session_missing", false)
		s.cleanupHeadedStartingTracking(r.InvocationID)
		s.clearInvocationProcess(r.InvocationID)
		return
	}

	exists, err := s.tmuxClient.HasSession(ctx, sessionName)
	if err != nil && !tmux.IsNoSessionErr(err) {
		s.recordInvocationWarning(repoID, r.InvocationID, "reconcile_tmux_has_session_failed", err.Error(), map[string]any{
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
			restoreSupervision = parseErr == nil && s.clock().Sub(startedAt) > 30*time.Second
		}
		if restoreSupervision {
			if err := s.restoreExistingHeadedSupervision(ctx, repoID, r.InvocationID, r.Meta, sessionName, "agency.headed_supervision_reconciled"); err != nil {
				s.recordInvocationWarning(repoID, r.InvocationID, "reconcile_headed_supervision_restore_failed", err.Error(), map[string]any{
					"session_name": sessionName,
				})
			}
		}
		if r.Meta.Status == store.InvocationStatusStopping && !supervised {
			if err := s.tmuxClient.InterruptSession(ctx, sessionName); err != nil {
				s.recordInvocationWarning(repoID, r.InvocationID, "reconcile_headed_stop_interrupt_failed", err.Error(), map[string]any{
					"session_name": sessionName,
				})
			}
		}
		s.cleanupHeadedStartingTracking(r.InvocationID)
		return
	}

	switch r.Meta.Status {
	case store.InvocationStatusStarting:
		s.reconcileHeadedStarting(repoID, r.InvocationID, r.Meta, now)
	case store.InvocationStatusRunning:
		s.finishInvocationExited(repoID, r.InvocationID)
		s.clearInvocationProcess(r.InvocationID)
	case store.InvocationStatusStopping:
		s.failInvocationStopped(repoID, r.InvocationID, "stopped")
		s.clearInvocationProcess(r.InvocationID)
	}
}

func (s *Server) reconcileHeadlessInvocation(repoID string, r store.InvocationRecord, now string) {
	if r.Meta.Status != store.InvocationStatusRunning &&
		r.Meta.Status != store.InvocationStatusStopping {
		return
	}
	if r.Meta.PID == nil {
		if r.Meta.Status == store.InvocationStatusStopping {
			s.failInvocationStopped(repoID, r.InvocationID, "stopped")
			return
		}
		s.failInvocationUnknown(repoID, r.InvocationID, "runner_pid_missing", true)
		return
	}
	pid := *r.Meta.PID

	s.mu.RLock()
	_, supervised := s.processes[r.InvocationID]
	s.mu.RUnlock()
	if supervised || s.pidChecker(pid) {
		return
	}

	_ = now
	if r.Meta.Status == store.InvocationStatusStopping {
		s.failInvocationStopped(repoID, r.InvocationID, "stopped")
		return
	}
	s.failInvocationUnknown(repoID, r.InvocationID, "runner_pid_dead", false)
}

func (s *Server) reconcileHeadedStarting(repoID, invocationID string, meta *store.InvocationMeta, now string) {
	s.headedStartingTickCountMu.Lock()
	defer s.headedStartingTickCountMu.Unlock()

	count := s.headedStartingTickCount[invocationID] + 1
	s.headedStartingTickCount[invocationID] = count
	if count < headedStartingGraceCount {
		return
	}

	s.failInvocationStart(repoID, invocationID, "tmux_session_missing", false)
	delete(s.headedStartingTickCount, invocationID)

	s.clearInvocationProcess(invocationID)
}

func (s *Server) cleanupHeadedStartingTracking(invocationID string) {
	s.headedStartingTickCountMu.Lock()
	delete(s.headedStartingTickCount, invocationID)
	s.headedStartingTickCountMu.Unlock()
}
