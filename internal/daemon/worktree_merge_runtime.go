package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func worktreeMergeKey(repoID, worktreeID string) string {
	return repoID + "/" + worktreeID
}

func sameNormalizedMergeRequest(a, b normalizedMergeRequest) bool {
	return a.Strategy == b.Strategy &&
		a.DeleteBranch == b.DeleteBranch &&
		strings.TrimSpace(a.AgencyConfigPath) == strings.TrimSpace(b.AgencyConfigPath)
}

func (s *Server) beginWorktreeMerge(
	repoID string,
	worktreeID string,
	attemptID string,
	requestID string,
	req normalizedMergeRequest,
) (*WorktreeMergeProcess, bool, error) {
	key := worktreeMergeKey(repoID, worktreeID)

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing := s.activeMerges[key]; existing != nil {
		if sameNormalizedMergeRequest(existing.Request, req) {
			return existing, true, nil
		}
		return nil, false, errors.NewWithDetails(
			errors.EWorktreeMergeActive,
			"worktree merge is already running for this worktree",
			map[string]string{
				"attempt_id": existing.AttemptID,
				"hint":       "wait for the active merge to finish or rerun with the same options to attach",
			},
		)
	}

	ctx, cancel := context.WithCancel(context.Background())
	proc := &WorktreeMergeProcess{
		RepoID:     repoID,
		WorktreeID: worktreeID,
		AttemptID:  attemptID,
		RequestID:  requestID,
		Request:    req,
		ctx:        ctx,
		cancel:     cancel,
		done:       make(chan struct{}),
	}
	s.activeMerges[key] = proc
	return proc, false, nil
}

func (s *Server) activeWorktreeMerge(repoID, worktreeID string) *WorktreeMergeProcess {
	key := worktreeMergeKey(repoID, worktreeID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeMerges[key]
}

func (s *Server) releaseWorktreeMerge(proc *WorktreeMergeProcess) {
	if proc == nil {
		return
	}

	key := worktreeMergeKey(proc.RepoID, proc.WorktreeID)
	s.mu.Lock()
	if current := s.activeMerges[key]; current == proc {
		delete(s.activeMerges, key)
	}
	s.mu.Unlock()

	proc.CloseDone()
}

func (s *Server) ensureWorktreeMergeInactive(repoID, worktreeID, action string) error {
	proc := s.activeWorktreeMerge(repoID, worktreeID)
	if proc == nil {
		return nil
	}
	return errors.NewWithDetails(
		errors.EWorktreeMergeActive,
		fmt.Sprintf("worktree merge is already running; cannot %s during an active merge", action),
		map[string]string{
			"attempt_id": proc.AttemptID,
			"hint":       "wait for the active merge to finish, or rerun 'agency worktree <worktree-ref> pr merge' to attach",
		},
	)
}

func (s *Server) cancelActiveWorktreeMerges(ctx context.Context) {
	s.mu.RLock()
	merges := make([]*WorktreeMergeProcess, 0, len(s.activeMerges))
	for _, proc := range s.activeMerges {
		merges = append(merges, proc)
	}
	s.mu.RUnlock()

	for _, proc := range merges {
		if proc != nil && proc.cancel != nil {
			proc.cancel()
		}
	}
	for _, proc := range merges {
		if proc == nil {
			continue
		}
		select {
		case <-proc.done:
		case <-ctx.Done():
			return
		}
	}
}
