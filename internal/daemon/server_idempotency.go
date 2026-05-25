package daemon

import (
	"context"

	"github.com/NielsdaWheelz/agency/internal/store"
)

// idempotencyScope discriminates entries within the single idempotency map so
// that headed and headless starts (or other future flows) cannot collide on a
// reused client_request_id.
type idempotencyScope string

const (
	idempotencyScopeHeadlessStart idempotencyScope = ""
	idempotencyScopeHeadedStart   idempotencyScope = "headed"
	idempotencyScopeFollowUp      idempotencyScope = "followup"
)

func idempotencyKey(repoID string, scope idempotencyScope, clientRequestID string) string {
	if scope == "" {
		return repoID + ":" + clientRequestID
	}
	return repoID + ":" + string(scope) + ":" + clientRequestID
}

func (s *Server) checkIdempotency(scope idempotencyScope, repoID, clientRequestID, fingerprint string) (idempotencyEntry, bool, bool) {
	if clientRequestID == "" {
		return idempotencyEntry{}, false, false
	}

	s.idempotencyMu.RLock()
	defer s.idempotencyMu.RUnlock()

	entry, exists := s.idempotency[idempotencyKey(repoID, scope, clientRequestID)]
	if !exists || s.clock().Unix()-entry.createdAt > idempotencyTTL {
		return idempotencyEntry{}, false, false
	}
	return entry, true, entry.fingerprint != fingerprint
}

func (s *Server) recordIdempotency(scope idempotencyScope, repoID, clientRequestID, invocationID, fingerprint string) {
	if clientRequestID == "" {
		return
	}

	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	s.idempotency[idempotencyKey(repoID, scope, clientRequestID)] = idempotencyEntry{
		invocationID: invocationID,
		fingerprint:  fingerprint,
		createdAt:    s.clock().Unix(),
	}
	if len(s.idempotency) > 100 {
		s.cleanupExpiredIdempotency()
	}
}

func (s *Server) cleanupExpiredIdempotency() {
	now := s.clock().Unix()
	for key, entry := range s.idempotency {
		if now-entry.createdAt > idempotencyTTL {
			delete(s.idempotency, key)
		}
	}
}

func (s *Server) findInvocationByClientRequestID(repoID, clientRequestID, fingerprint string) (*store.InvocationRecord, bool, bool, error) {
	if clientRequestID == "" {
		return nil, false, false, nil
	}
	records, err := s.store.ScanInvocationsForRepo(repoID)
	if err != nil {
		return nil, false, false, err
	}
	for i := range records {
		record := &records[i]
		if record.Meta == nil || record.Meta.ClientRequestID != clientRequestID {
			continue
		}
		if record.Meta.TaskID != "" {
			continue
		}
		return record, true, record.Meta.RequestFingerprint != fingerprint, nil
	}
	return nil, false, false, nil
}

func (s *Server) findInvocationRecordByClientRequestID(repoID, clientRequestID string) (*store.InvocationRecord, bool, error) {
	if clientRequestID == "" {
		return nil, false, nil
	}
	records, err := s.store.ScanInvocationsForRepo(repoID)
	if err != nil {
		return nil, false, err
	}
	for i := range records {
		record := &records[i]
		if record.Meta == nil || record.Meta.ClientRequestID != clientRequestID {
			continue
		}
		if record.Meta.TaskID != "" {
			continue
		}
		return record, true, nil
	}
	return nil, false, nil
}

func worktreeIdempotencyKey(repoID, idempotencyKey string) string {
	return repoID + ":worktree:" + idempotencyKey
}

func worktreeCreateFingerprint(repoRoot string, req WorktreeCreateRequest, execCtx executionContext) string {
	return hashStructFingerprint(struct {
		RepoRoot         string `json:"repo_root"`
		Name             string `json:"name"`
		BaseBranch       string `json:"base_branch"`
		ExecutionProfile string `json:"execution_profile"`
		CheckoutRoot     string `json:"checkout_root"`
	}{
		RepoRoot:         repoRoot,
		Name:             req.Name,
		BaseBranch:       req.BaseBranch,
		ExecutionProfile: execCtx.Profile,
		CheckoutRoot:     execCtx.CheckoutRoot,
	})
}

func (s *Server) checkWorktreeIdempotency(repoID, idempotencyKey, fingerprint string) (worktreeIdempotencyEntry, bool, bool) {
	if idempotencyKey == "" {
		return worktreeIdempotencyEntry{}, false, false
	}

	s.worktreeIdempotencyMu.RLock()
	defer s.worktreeIdempotencyMu.RUnlock()

	entry, exists := s.worktreeIdempotency[worktreeIdempotencyKey(repoID, idempotencyKey)]
	if !exists || s.clock().Unix()-entry.createdAt > idempotencyTTL {
		return worktreeIdempotencyEntry{}, false, false
	}
	return entry, true, entry.fingerprint != fingerprint
}

func (s *Server) recordWorktreeIdempotency(repoID, idempotencyKey, worktreeID, fingerprint string) {
	if idempotencyKey == "" {
		return
	}

	s.worktreeIdempotencyMu.Lock()
	defer s.worktreeIdempotencyMu.Unlock()
	s.worktreeIdempotency[worktreeIdempotencyKey(repoID, idempotencyKey)] = worktreeIdempotencyEntry{
		worktreeID:  worktreeID,
		fingerprint: fingerprint,
		createdAt:   s.clock().Unix(),
	}
	if len(s.worktreeIdempotency) > 100 {
		s.cleanupExpiredWorktreeIdempotency()
	}
}

func (s *Server) cleanupExpiredWorktreeIdempotency() {
	now := s.clock().Unix()
	for key, entry := range s.worktreeIdempotency {
		if now-entry.createdAt > idempotencyTTL {
			delete(s.worktreeIdempotency, key)
		}
	}
}

func (s *Server) findWorktreeByIdempotencyKey(repoID, idempotencyKey, fingerprint string) (*store.IntegrationWorktreeRecord, bool, bool, error) {
	if idempotencyKey == "" {
		return nil, false, false, nil
	}
	records, err := s.store.ScanIntegrationWorktreesForRepo(repoID)
	if err != nil {
		return nil, false, false, err
	}
	for i := range records {
		record := &records[i]
		if record.Meta == nil || record.Meta.IdempotencyKey != idempotencyKey {
			continue
		}
		return record, true, record.Meta.RequestFingerprint != fingerprint, nil
	}
	return nil, false, false, nil
}

func followUpFingerprint(invocationID, prompt string) string {
	return hashStructFingerprint(struct {
		InvocationID string `json:"invocation_id"`
		PromptSHA256 string `json:"prompt_sha256"`
	}{
		InvocationID: invocationID,
		PromptSHA256: sha256Hex([]byte(prompt)),
	})
}

func (s *Server) reserveFollowUpIdempotency(repoID, clientRequestID, invocationID, fingerprint string) (idempotencyEntry, bool, bool) {
	if clientRequestID == "" {
		return idempotencyEntry{}, false, false
	}

	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()

	key := idempotencyKey(repoID, idempotencyScopeFollowUp, clientRequestID)
	entry, exists := s.idempotency[key]
	if exists && s.clock().Unix()-entry.createdAt <= idempotencyTTL {
		return entry, true, entry.fingerprint != fingerprint
	}

	entry = idempotencyEntry{
		invocationID: invocationID,
		fingerprint:  fingerprint,
		createdAt:    s.clock().Unix(),
	}
	s.idempotency[key] = entry
	if len(s.idempotency) > 100 {
		s.cleanupExpiredIdempotency()
	}
	return entry, false, false
}

func (s *Server) runCheckpointLoop(proc *supervisedProcess) {
	defer s.supervisionWg.Done()

	if proc.checkpointEngine == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopperDone := make(chan struct{})
	go func() {
		defer close(stopperDone)
		<-proc.done
		cancel()
		proc.checkpointEngine.Stop()
	}()
	_ = proc.checkpointEngine.Run(ctx)
	<-stopperDone
}
