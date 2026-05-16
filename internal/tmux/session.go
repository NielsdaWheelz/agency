// Package tmux provides tmux integration for agency.
package tmux

import "fmt"

// SessionTarget returns the tmux target string for a run's primary pane.
// Format: agency_<run_id>:0.0
func SessionTarget(runID string) string {
	return fmt.Sprintf("agency_%s:0.0", runID)
}

// SessionName returns the tmux session name for a run.
// Format: agency_<run_id>
func SessionName(runID string) string {
	return fmt.Sprintf("agency_%s", runID)
}
