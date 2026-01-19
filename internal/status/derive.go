// Package status provides pure status derivation logic for agency runs.
// No filesystem, tmux, or network calls are made in this package.
package status

import (
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/watchdog"
)

// Derived status string constants (user-visible contract, must remain stable across v1.x).
const (
	StatusBroken         = "broken"
	StatusMerged         = "merged"
	StatusAbandoned      = "abandoned"
	StatusFailed         = "failed"
	StatusNeedsAttention = "needs attention"
	StatusReadyForReview = "ready for review"
	StatusNeedsInput     = "needs input"
	StatusBlocked        = "blocked"
	StatusWorking        = "working"
	StatusStalled        = "stalled"
	StatusActive         = "active"
	StatusIdle           = "idle"
)

// Snapshot contains local-only inputs for status derivation.
// These values must be computed by the caller from filesystem and tmux state.
type Snapshot struct {
	// TmuxActive is true iff the tmux session exists (v1 definition of "active").
	TmuxActive bool

	// WorktreePresent is true iff the worktree path exists on disk.
	WorktreePresent bool

	// RunnerStatus is the parsed runner_status.json file, or nil if missing/invalid.
	RunnerStatus *runnerstatus.RunnerStatus

	// StallResult contains the result of stall detection, or nil if not computed.
	StallResult *watchdog.StallResult
}

// Derived contains the computed status values.
type Derived struct {
	// DerivedStatus is the human-readable status string.
	// Does not include "(archived)" suffix; that's the render layer's responsibility.
	DerivedStatus string

	// Archived is true iff the worktree is not present.
	Archived bool
}

// Derive computes the derived status from meta and local snapshot.
// meta may be nil for broken runs; in that case DerivedStatus is "broken".
// This function is pure and must not panic.
func Derive(meta *store.RunMeta, in Snapshot) Derived {
	// Compute presence-derived fields (independent of meta)
	archived := !in.WorktreePresent

	// Handle broken runs (nil meta)
	if meta == nil {
		return Derived{
			DerivedStatus: StatusBroken,
			Archived:      archived,
		}
	}

	// Compute derived status using precedence rules
	status := deriveStatus(meta, in)

	return Derived{
		DerivedStatus: status,
		Archived:      archived,
	}
}

// deriveStatus implements the precedence rules for status derivation.
// Precondition: meta is non-nil.
//
// Precedence (highest to lowest):
//  1. broken           → meta.json unreadable (handled before this function)
//  2. merged           → archive.merged_at set
//  3. abandoned        → flags.abandoned set
//  4. failed           → flags.setup_failed set
//  5. needs attention  → flags.needs_attention set
//  6. ready for review → runner_status.status == "ready_for_review"
//  7. needs input      → runner_status.status == "needs_input"
//  8. blocked          → runner_status.status == "blocked"
//  9. working          → runner_status.status == "working"
//  10. stalled         → watchdog.IsStalled && tmux exists
//  11. active          → tmux exists (fallback)
//  12. idle            → no tmux (fallback)
func deriveStatus(meta *store.RunMeta, in Snapshot) string {
	// 1) Terminal outcomes always win (broken handled above)
	// 2) merged
	if isMerged(meta) {
		return StatusMerged
	}
	// 3) abandoned
	if isAbandoned(meta) {
		return StatusAbandoned
	}

	// 4) setup_failed
	if isSetupFailed(meta) {
		return StatusFailed
	}
	// 5) needs_attention
	if isNeedsAttention(meta) {
		return StatusNeedsAttention
	}

	// 6-9) Runner-reported status (if available and valid)
	if in.RunnerStatus != nil && in.RunnerStatus.Status.IsValid() {
		switch in.RunnerStatus.Status {
		case runnerstatus.StatusReadyForReview:
			return StatusReadyForReview
		case runnerstatus.StatusNeedsInput:
			return StatusNeedsInput
		case runnerstatus.StatusBlocked:
			return StatusBlocked
		case runnerstatus.StatusWorking:
			return StatusWorking
		}
	}

	// 10) Stalled detection
	if in.StallResult != nil && in.StallResult.IsStalled && in.TmuxActive {
		return StatusStalled
	}

	// 11-12) Activity fallbacks
	if in.TmuxActive {
		return StatusActive
	}
	return StatusIdle
}

// isMerged returns true if archive.merged_at is set.
func isMerged(meta *store.RunMeta) bool {
	return meta.Archive != nil && meta.Archive.MergedAt != ""
}

// isAbandoned returns true if flags.abandoned is set.
func isAbandoned(meta *store.RunMeta) bool {
	return meta.Flags != nil && meta.Flags.Abandoned
}

// isSetupFailed returns true if flags.setup_failed is set.
func isSetupFailed(meta *store.RunMeta) bool {
	return meta.Flags != nil && meta.Flags.SetupFailed
}

// isNeedsAttention returns true if flags.needs_attention is set.
func isNeedsAttention(meta *store.RunMeta) bool {
	return meta.Flags != nil && meta.Flags.NeedsAttention
}
