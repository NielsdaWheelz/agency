package daemon

import (
	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func (s *Server) appendWorktreeEvent(repoID, worktreeID, kind string, data map[string]any) error {
	_, err := s.worktreeEvents.Append(
		s.store.IntegrationWorktreeEventsPath(repoID, worktreeID),
		worktreeID,
		kind,
		data,
		eventlog.AppendOptions{},
	)
	if err != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to append worktree event", err)
	}
	return nil
}

// recordWorktreeOpFailure appends a failure event for op (e.g. pr sync, rebase)
// carrying op's error code/message, then returns op's original error. If the
// event append itself fails, the append error wins (the operation failed and the
// audit trail also failed; surfacing the second condition matters more).
func (s *Server) recordWorktreeOpFailure(repoID, worktreeID, eventType string, opErr error) error {
	code := errors.CodeOr(opErr, errors.EInternal)
	if appendErr := s.appendWorktreeEvent(repoID, worktreeID, eventType, map[string]any{
		"error_code": string(code),
		"message":    apiErrorMessage(opErr),
	}); appendErr != nil {
		return appendErr
	}
	return opErr
}
