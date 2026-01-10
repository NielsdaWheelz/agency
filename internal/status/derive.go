// Package status provides pure status derivation logic for agency runs.
// No filesystem, tmux, or network calls are made in this package.
package status

import "github.com/NielsdaWheelz/agency/internal/store"

// ReportNonemptyThresholdBytes is the minimum byte count for a report to be considered non-empty.
// Reports below this threshold are assumed to be template-only or effectively empty.
const ReportNonemptyThresholdBytes = 64

// Derived status string constants (user-visible contract, must remain stable across v1.x).
const (
	StatusBroken           = "broken"
	StatusMerged           = "merged"
	StatusAbandoned        = "abandoned"
	StatusFailed           = "failed"
	StatusNeedsAttention   = "needs attention"
	StatusReadyForReview   = "ready for review"
	StatusActivePR         = "active (pr)"
	StatusActive           = "active"
	StatusIdlePR           = "idle (pr)"
	StatusIdle             = "idle"
)

// Snapshot contains local-only inputs for status derivation.
// These values must be computed by the caller from filesystem and tmux state.
type Snapshot struct {
	// TmuxActive is true iff the tmux session exists (v1 definition of "active").
	TmuxActive bool

	// WorktreePresent is true iff the worktree path exists on disk.
	WorktreePresent bool

	// ReportBytes is the size of .agency/report.md in bytes.
	// Set to 0 if the file is missing or unreadable.
	ReportBytes int
}

// Derived contains the computed status values.
type Derived struct {
	// DerivedStatus is the human-readable status string.
	// Does not include "(archived)" suffix; that's the render layer's responsibility.
	DerivedStatus string

	// Archived is true iff the worktree is not present.
	Archived bool

	// ReportNonempty is true iff ReportBytes >= ReportNonemptyThresholdBytes.
	ReportNonempty bool
}

// Derive computes the derived status from meta and local snapshot.
// meta may be nil for broken runs; in that case DerivedStatus is "broken".
// This function is pure and must not panic.
func Derive(meta *store.RunMeta, in Snapshot) Derived {
	// Clamp negative report bytes to 0
	reportBytes := in.ReportBytes
	if reportBytes < 0 {
		reportBytes = 0
	}

	// Compute presence-derived fields (independent of meta)
	archived := !in.WorktreePresent
	reportNonempty := reportBytes >= ReportNonemptyThresholdBytes

	// Handle broken runs (nil meta)
	if meta == nil {
		return Derived{
			DerivedStatus:  StatusBroken,
			Archived:       archived,
			ReportNonempty: reportNonempty,
		}
	}

	// Compute derived status using precedence rules
	status := deriveStatus(meta, in.TmuxActive, reportNonempty)

	return Derived{
		DerivedStatus:  status,
		Archived:       archived,
		ReportNonempty: reportNonempty,
	}
}

// deriveStatus implements the precedence rules for status derivation.
// Precondition: meta is non-nil.
func deriveStatus(meta *store.RunMeta, tmuxActive bool, reportNonempty bool) string {
	// 1) Terminal outcome always wins
	if isMerged(meta) {
		return StatusMerged
	}
	if isAbandoned(meta) {
		return StatusAbandoned
	}

	// 2) Open-run failure flags
	if isSetupFailed(meta) {
		return StatusFailed
	}
	if isNeedsAttention(meta) {
		return StatusNeedsAttention
	}

	// 3) Ready for review (all predicates must be true)
	if isReadyForReview(meta, reportNonempty) {
		return StatusReadyForReview
	}

	// 4) Activity fallbacks
	hasPR := hasPRNumber(meta)
	if tmuxActive && hasPR {
		return StatusActivePR
	}
	if tmuxActive {
		return StatusActive
	}
	if hasPR {
		return StatusIdlePR
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

// hasPRNumber returns true if pr_number is set (non-zero).
func hasPRNumber(meta *store.RunMeta) bool {
	return meta.PRNumber != 0
}

// hasLastPushAt returns true if last_push_at is set (non-empty).
func hasLastPushAt(meta *store.RunMeta) bool {
	return meta.LastPushAt != ""
}

// isReadyForReview returns true if all ready-for-review predicates are met:
// - pr_number is set
// - last_push_at is set
// - report is non-empty (>= 64 bytes)
func isReadyForReview(meta *store.RunMeta, reportNonempty bool) bool {
	return hasPRNumber(meta) && hasLastPushAt(meta) && reportNonempty
}
