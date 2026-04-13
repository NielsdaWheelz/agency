package daemon

import (
	"context"
	"encoding/json"
	"time"

	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

func (s *Server) runHeadedReconcileLoop() {
	defer close(s.reconcileLoopDone)

	interval := HeadedReconcileInterval
	if s.HeadedReconcileIntervalOverride != nil {
		interval = *s.HeadedReconcileIntervalOverride
	}

	ticker := time.NewTicker(interval)
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

	data, err := s.FS.ReadFile(s.Store.RepoIndexPath())
	if err != nil {
		return
	}

	var index store.RepoIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return
	}
	for repoID := range index.Repos {
		s.reconcileHeadedInvocationsForRepo(ctx, repoID)
	}
}

func (s *Server) reconcileHeadedInvocationsForRepo(ctx context.Context, repoID string) {
	records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return
	}

	now := s.Clock().UTC().Format(time.RFC3339)
	for _, r := range records {
		if r.Broken || r.Meta == nil {
			continue
		}
		if r.Meta.Status != store.InvocationStatusStarting && r.Meta.Status != store.InvocationStatusRunning {
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
	sessionName := r.Meta.TmuxSession
	if sessionName == "" {
		sessionName = tmux.SessionName(r.InvocationID)
	}

	exists, err := s.TmuxClient.HasSession(ctx, sessionName)
	if err != nil && !tmux.IsNoSessionErr(err) {
		s.recordInvocationWarning(repoID, r.InvocationID, "reconcile_tmux_has_session_failed", err.Error(), map[string]any{
			"session_name": sessionName,
		})
		return
	}
	if exists {
		s.cleanupHeadedStartingTracking(r.InvocationID)
		return
	}

	switch r.Meta.Status {
	case store.InvocationStatusStarting:
		s.reconcileHeadedStarting(repoID, r.InvocationID, r.Meta, now)
	case store.InvocationStatusRunning:
		_ = s.Store.UpdateInvocationMeta(repoID, r.InvocationID, func(meta *store.InvocationMeta) {
			meta.Status = store.InvocationStatusFinished
			meta.ExitReason = "exited"
			meta.FinishedAt = now
			meta.LifecycleOwner = ""
		})
		s.mu.Lock()
		if proc, ok := s.processes[r.InvocationID]; ok {
			proc.CloseDone()
			delete(s.processes, r.InvocationID)
		}
		s.mu.Unlock()
	}
}

func (s *Server) reconcileHeadlessInvocation(repoID string, r store.InvocationRecord, now string) {
	if r.Meta.Status != store.InvocationStatusRunning || r.Meta.PID == nil {
		return
	}
	pid := *r.Meta.PID

	s.mu.RLock()
	_, supervised := s.processes[r.InvocationID]
	s.mu.RUnlock()
	if supervised || s.PIDChecker(pid) {
		return
	}

	_ = s.Store.UpdateInvocationMeta(repoID, r.InvocationID, func(meta *store.InvocationMeta) {
		if meta.Status != store.InvocationStatusRunning {
			return
		}
		meta.Status = store.InvocationStatusFailed
		meta.ExitReason = "unknown"
		meta.FailureReason = "runner_pid_dead"
		meta.FinishedAt = now
		meta.PID = nil
		meta.Flags.NeedsAttention = true
		meta.LifecycleOwner = ""
	})
}

func (s *Server) reconcileHeadedStarting(repoID, invocationID string, meta *store.InvocationMeta, now string) {
	s.headedStartingTickCountMu.Lock()
	defer s.headedStartingTickCountMu.Unlock()

	count := s.headedStartingTickCount[invocationID] + 1
	s.headedStartingTickCount[invocationID] = count
	if count < HeadedStartingGraceCount {
		return
	}

	_ = s.Store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		m.Status = store.InvocationStatusFailed
		m.ExitReason = "start_failed"
		m.FailureReason = "tmux_session_missing"
		m.FinishedAt = now
		m.LifecycleOwner = ""
	})
	delete(s.headedStartingTickCount, invocationID)

	s.mu.Lock()
	if proc, ok := s.processes[invocationID]; ok {
		proc.CloseDone()
		delete(s.processes, invocationID)
	}
	s.mu.Unlock()
}

func (s *Server) cleanupHeadedStartingTracking(invocationID string) {
	s.headedStartingTickCountMu.Lock()
	delete(s.headedStartingTickCount, invocationID)
	s.headedStartingTickCountMu.Unlock()
}
