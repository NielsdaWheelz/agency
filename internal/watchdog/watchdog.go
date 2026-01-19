// Package watchdog provides stall detection for agency runs.
//
// A run is considered stalled if the runner_status.json file has not been
// updated within the configured threshold and the tmux session still exists.
package watchdog

import "time"

// DefaultStallThreshold is the default duration after which a run is considered stalled.
const DefaultStallThreshold = 15 * time.Minute

// ActivitySignals contains signals used to determine if a run is stalled.
type ActivitySignals struct {
	// StatusFileModTime is the modification time of runner_status.json.
	// Nil if the file does not exist.
	StatusFileModTime *time.Time

	// TmuxSessionExists is true if the tmux session is running.
	TmuxSessionExists bool
}

// StallResult contains the result of a stall check.
type StallResult struct {
	// IsStalled is true if the run is considered stalled.
	IsStalled bool

	// StalledDuration is the duration since the last activity signal.
	// Only meaningful when IsStalled is true.
	StalledDuration time.Duration
}

// CheckStall determines if a run is stalled based on activity signals.
//
// A run is considered stalled if:
// - The tmux session exists (runner is supposed to be active)
// - The status file exists and hasn't been modified within the threshold
//
// If the status file doesn't exist, the run is not considered stalled
// (fallback to legacy tmux-only detection).
func CheckStall(signals ActivitySignals, threshold time.Duration) StallResult {
	// No tmux session = not running = not stalled
	if !signals.TmuxSessionExists {
		return StallResult{IsStalled: false}
	}

	// No status file = can't determine stall state = not stalled
	// This allows backward compatibility with runs that don't have the status file
	if signals.StatusFileModTime == nil {
		return StallResult{IsStalled: false}
	}

	// Calculate time since last status update
	stalledDuration := time.Since(*signals.StatusFileModTime)

	// Check if stalled
	if stalledDuration >= threshold {
		return StallResult{
			IsStalled:       true,
			StalledDuration: stalledDuration,
		}
	}

	return StallResult{IsStalled: false}
}

// CheckStallWithDefault calls CheckStall with the DefaultStallThreshold.
func CheckStallWithDefault(signals ActivitySignals) StallResult {
	return CheckStall(signals, DefaultStallThreshold)
}
