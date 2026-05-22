package daemon

import (
	"strings"

	"github.com/NielsdaWheelz/agency/internal/store"
)

func headedInvocationSessionName(meta *store.InvocationMeta) (string, bool) {
	if meta == nil {
		return "", false
	}
	if sessionName := strings.TrimSpace(meta.TmuxSession); sessionName != "" {
		return sessionName, true
	}
	return "", false
}
