// Package tmux provides tmux integration for agency.
package tmux

import "fmt"

// SessionName returns the tmux session name for an invocation.
// Format: agency_<invocation_id>
func SessionName(invocationID string) string {
	return fmt.Sprintf("agency_%s", invocationID)
}
