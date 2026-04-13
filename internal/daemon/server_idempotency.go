package daemon

import "context"

func idempotencyKey(repoID, clientRequestID string) string {
	return repoID + ":" + clientRequestID
}

func (s *Server) checkIdempotency(repoID, clientRequestID string) (string, bool) {
	if clientRequestID == "" {
		return "", false
	}

	s.idempotencyMu.RLock()
	defer s.idempotencyMu.RUnlock()

	entry, exists := s.idempotency[idempotencyKey(repoID, clientRequestID)]
	if !exists || s.Clock().Unix()-entry.CreatedAt > IdempotencyTTL {
		return "", false
	}
	return entry.InvocationID, true
}

func (s *Server) recordIdempotency(repoID, clientRequestID, invocationID string) {
	if clientRequestID == "" {
		return
	}

	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	s.idempotency[idempotencyKey(repoID, clientRequestID)] = IdempotencyEntry{
		InvocationID: invocationID,
		CreatedAt:    s.Clock().Unix(),
	}
	if len(s.idempotency) > 100 {
		s.cleanupExpiredIdempotency()
	}
}

func (s *Server) cleanupExpiredIdempotency() {
	now := s.Clock().Unix()
	for key, entry := range s.idempotency {
		if now-entry.CreatedAt > IdempotencyTTL {
			delete(s.idempotency, key)
		}
	}
}

func worktreeIdempotencyKey(repoID, idempotencyKey string) string {
	return repoID + ":worktree:" + idempotencyKey
}

func (s *Server) checkWorktreeIdempotency(repoID, idempotencyKey string) (WorktreeIdempotencyEntry, bool) {
	if idempotencyKey == "" {
		return WorktreeIdempotencyEntry{}, false
	}

	s.worktreeIdempotencyMu.RLock()
	defer s.worktreeIdempotencyMu.RUnlock()

	entry, exists := s.worktreeIdempotency[worktreeIdempotencyKey(repoID, idempotencyKey)]
	if !exists || s.Clock().Unix()-entry.CreatedAt > IdempotencyTTL {
		return WorktreeIdempotencyEntry{}, false
	}
	return entry, true
}

func (s *Server) recordWorktreeIdempotency(repoID, idempotencyKey, worktreeID, treePath, branch string) {
	if idempotencyKey == "" {
		return
	}

	s.worktreeIdempotencyMu.Lock()
	defer s.worktreeIdempotencyMu.Unlock()
	s.worktreeIdempotency[worktreeIdempotencyKey(repoID, idempotencyKey)] = WorktreeIdempotencyEntry{
		WorktreeID: worktreeID,
		TreePath:   treePath,
		Branch:     branch,
		CreatedAt:  s.Clock().Unix(),
	}
	if len(s.worktreeIdempotency) > 100 {
		s.cleanupExpiredWorktreeIdempotency()
	}
}

func (s *Server) cleanupExpiredWorktreeIdempotency() {
	now := s.Clock().Unix()
	for key, entry := range s.worktreeIdempotency {
		if now-entry.CreatedAt > IdempotencyTTL {
			delete(s.worktreeIdempotency, key)
		}
	}
}

func headedIdempotencyKey(repoID, clientRequestID string) string {
	return repoID + ":headed:" + clientRequestID
}

func (s *Server) checkHeadedIdempotency(repoID, clientRequestID string) (HeadedIdempotencyEntry, bool) {
	if clientRequestID == "" {
		return HeadedIdempotencyEntry{}, false
	}

	s.headedIdempotencyMu.RLock()
	defer s.headedIdempotencyMu.RUnlock()

	entry, exists := s.headedIdempotency[headedIdempotencyKey(repoID, clientRequestID)]
	if !exists || s.Clock().Unix()-entry.CreatedAt > IdempotencyTTL {
		return HeadedIdempotencyEntry{}, false
	}
	return entry, true
}

func (s *Server) recordHeadedIdempotency(repoID, clientRequestID, invocationID, tmuxSession, sandboxPath string) {
	if clientRequestID == "" {
		return
	}

	s.headedIdempotencyMu.Lock()
	defer s.headedIdempotencyMu.Unlock()
	s.headedIdempotency[headedIdempotencyKey(repoID, clientRequestID)] = HeadedIdempotencyEntry{
		InvocationID: invocationID,
		TmuxSession:  tmuxSession,
		SandboxPath:  sandboxPath,
		CreatedAt:    s.Clock().Unix(),
	}
	if len(s.headedIdempotency) > 100 {
		s.cleanupExpiredHeadedIdempotency()
	}
}

func (s *Server) cleanupExpiredHeadedIdempotency() {
	now := s.Clock().Unix()
	for key, entry := range s.headedIdempotency {
		if now-entry.CreatedAt > IdempotencyTTL {
			delete(s.headedIdempotency, key)
		}
	}
}

func (s *Server) runCheckpointLoop(proc *SupervisedProcess) {
	if proc.CheckpointEngine == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-proc.done
		cancel()
		proc.CheckpointEngine.Stop()
	}()
	_ = proc.CheckpointEngine.Run(ctx)
}
