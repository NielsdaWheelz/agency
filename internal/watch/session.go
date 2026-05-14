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
