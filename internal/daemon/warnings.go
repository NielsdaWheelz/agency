package daemon

import (
	"log"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

const daemonWarningEventKind = "agency.daemon_warning"

// appendInvocationEvent appends a strict event to the invocation event log.
// Unlike recordInvocationWarning, append failure is returned to the caller
// so mutating handlers can fail the operation per binding rule 2.
func (s *Server) appendInvocationEvent(repoID, invocationID, kind string, data map[string]any) error {
	_, err := s.invocationEvents.Append(
		s.store.InvocationEventsPath(repoID, invocationID),
		invocationID,
		kind,
		data,
		eventlog.AppendOptions{},
	)
	if err != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to append invocation event", err)
	}
	return nil
}

func (s *Server) recordInvocationWarning(repoID, invocationID, code, warning string, extra map[string]any) {
	if s == nil || s.store == nil {
		return
	}
	if strings.TrimSpace(repoID) == "" || strings.TrimSpace(invocationID) == "" || strings.TrimSpace(warning) == "" {
		return
	}

	data := map[string]any{
		"warning": warning,
	}
	if strings.TrimSpace(code) != "" {
		data["code"] = code
	}
	for key, value := range extra {
		data[key] = value
	}

	if _, err := s.invocationEvents.Append(
		s.store.InvocationEventsPath(repoID, invocationID),
		invocationID,
		daemonWarningEventKind,
		data,
		eventlog.AppendOptions{},
	); err != nil {
		log.Printf("agencyd: append daemon warning for %s/%s: %v", repoID, invocationID, err)
	}
}
