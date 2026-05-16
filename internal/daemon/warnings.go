package daemon

import (
	"log"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
)

const daemonWarningEventKind = "agency.daemon_warning"

func (s *Server) recordInvocationWarning(repoID, invocationID, code, warning string, extra map[string]any) {
	if s == nil || s.Store == nil {
		return
	}
	if strings.TrimSpace(repoID) == "" || strings.TrimSpace(invocationID) == "" || strings.TrimSpace(warning) == "" {
		return
	}

	writer := s.InvocationEvents
	if writer == nil {
		writer = eventlog.NewWriter("invocation_id", s.Clock)
		s.InvocationEvents = writer
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

	if _, err := writer.Append(
		s.Store.InvocationEventsPath(repoID, invocationID),
		invocationID,
		daemonWarningEventKind,
		data,
		eventlog.AppendOptions{},
	); err != nil {
		log.Printf("agencyd: append daemon warning for %s/%s: %v", repoID, invocationID, err)
	}
}
