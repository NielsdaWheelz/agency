package daemon

import (
	"log"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
)

const daemonWarningEventKind = "agency.daemon_warning"

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
