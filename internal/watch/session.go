package watch

import (
	"context"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

// InvocationSessionLoader reads headed-session facts for one invocation.
type InvocationSessionLoader func(context.Context, string, string) (daemon.InvocationSessionData, error)

func sessionIsLive(session daemon.InvocationSessionData) bool {
	return strings.EqualFold(strings.TrimSpace(session.SessionStatus), "live")
}

func sessionIsMissing(session daemon.InvocationSessionData) bool {
	return strings.EqualFold(strings.TrimSpace(session.SessionStatus), "missing")
}

func sessionConnectedClientCount(session daemon.InvocationSessionData) int {
	if session.ClientCount > 0 {
		return session.ClientCount
	}
	return len(session.ConnectedClients)
}

func sessionHint(session daemon.InvocationSessionData) string {
	if sessionIsMissing(session) && session.RecreateAvailable {
		return "use recreate to start a new headed session in the same sandbox"
	}
	return ""
}
