// Package watchdog provides stall detection for agency runs.
package watchdog

import "time"

// DefaultStallThreshold is the default duration after which a run is considered stalled.
const DefaultStallThreshold = 15 * time.Minute

// ActivitySignals contains signals used to determine if a run is stalled.
type ActivitySignals struct {
	// StatusFileModTime is the modification time of runner_status.json.
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
func CheckStall(signals ActivitySignals, threshold time.Duration) StallResult {
	if !signals.TmuxSessionExists {
		return StallResult{IsStalled: false}
	}

	if signals.StatusFileModTime == nil {
		return StallResult{IsStalled: false}
	}

	stalledDuration := time.Since(*signals.StatusFileModTime)
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
