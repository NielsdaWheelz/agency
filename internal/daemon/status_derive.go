// Package daemon implements the agency daemon supervisor.
package daemon

import (
	"time"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// DerivedStatus contains the computed display status and related fields.
type DerivedStatus struct {
	DisplayStatus  string
	AttentionFlags []string
	SortKey        int
}

// StallThreshold is the duration after which an invocation with no output is considered stalled.
const StallThreshold = 5 * time.Minute

// DeriveDisplayStatus computes the display status from invocation metadata.
//
// Precedence:
//  1. lifecycle == failed → "failed"
//  2. landing_status == landed → "landed"
//  3. landing_status == discarded → "discarded"
//  4. needs_attention flag → "needs attention"
//  5. semantic == needs_input → "needs input"
//  6. semantic == blocked → "blocked"
//  7. semantic == ready_for_review → "ready for review"
//  8. running + semantic working → "working"
//  9. running → "running"
//  10. finished → "finished"
//  11. starting → "starting"
func DeriveDisplayStatus(meta *store.InvocationMeta, now time.Time) DerivedStatus {
	var flags []string

	// Collect attention flags
	if meta.Flags.NeedsAttention {
		flags = append(flags, AttentionFlagNeedsAttention)
	}
	if meta.Flags.Orphaned {
		flags = append(flags, AttentionFlagOrphaned)
	}

	// Check for stalled (no output for > threshold while running)
	if meta.Status == store.InvocationStatusRunning && meta.LastOutputAt != "" {
		lastOutput, err := time.Parse(time.RFC3339, meta.LastOutputAt)
		if err == nil && now.Sub(lastOutput) > StallThreshold {
			flags = append(flags, AttentionFlagStalled)
		}
	}

	// Check if landable (finished, not yet landed/discarded)
	if meta.Status == store.InvocationStatusFinished &&
		meta.LandingStatus != store.LandingStatusLanded &&
		meta.LandingStatus != store.LandingStatusDiscarded {
		flags = append(flags, AttentionFlagLandable)
	}

	// Get semantic status string
	var semanticStatus string
	if meta.SemanticStatus != nil {
		semanticStatus = string(*meta.SemanticStatus)
	}

	// Apply precedence rules
	//
	// 1. Failed
	if meta.Status == store.InvocationStatusFailed {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusFailed,
			AttentionFlags: flags,
			SortKey:        SortKeyFailed,
		}
	}

	// 2. Landed
	if meta.LandingStatus == store.LandingStatusLanded {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusLanded,
			AttentionFlags: flags,
			SortKey:        SortKeyLanded,
		}
	}

	// 3. Discarded
	if meta.LandingStatus == store.LandingStatusDiscarded {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusDiscarded,
			AttentionFlags: flags,
			SortKey:        SortKeyDiscarded,
		}
	}

	// 4. Needs attention
	if meta.Flags.NeedsAttention {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusNeedsAttention,
			AttentionFlags: flags,
			SortKey:        SortKeyNeedsAttention,
		}
	}

	// 5. Semantic: needs_input
	if semanticStatus == string(runnerstatus.StatusNeedsInput) {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusNeedsInput,
			AttentionFlags: flags,
			SortKey:        SortKeyNeedsInput,
		}
	}

	// 6. Semantic: blocked
	if semanticStatus == string(runnerstatus.StatusBlocked) {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusBlocked,
			AttentionFlags: flags,
			SortKey:        SortKeyBlocked,
		}
	}

	// 7. Semantic: ready_for_review
	if semanticStatus == string(runnerstatus.StatusReadyForReview) {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusReadyForReview,
			AttentionFlags: flags,
			SortKey:        SortKeyReadyForReview,
		}
	}

	// 8. Running + working
	if meta.Status == store.InvocationStatusRunning && semanticStatus == string(runnerstatus.StatusWorking) {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusWorking,
			AttentionFlags: flags,
			SortKey:        SortKeyWorking,
		}
	}

	// 9. Running
	if meta.Status == store.InvocationStatusRunning {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusRunning,
			AttentionFlags: flags,
			SortKey:        SortKeyRunning,
		}
	}

	// 10. Finished
	if meta.Status == store.InvocationStatusFinished {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusFinished,
			AttentionFlags: flags,
			SortKey:        SortKeyFinished,
		}
	}

	// 11. Starting (default)
	return DerivedStatus{
		DisplayStatus:  DisplayStatusStarting,
		AttentionFlags: flags,
		SortKey:        SortKeyStarting,
	}
}

// InvocationMetaToDTO converts an InvocationMeta to an InvocationDTO.
func InvocationMetaToDTO(meta *store.InvocationMeta, repoID string, logsDir string, now time.Time) InvocationDTO {
	derived := DeriveDisplayStatus(meta, now)

	var semanticStatus string
	if meta.SemanticStatus != nil {
		semanticStatus = string(*meta.SemanticStatus)
	}

	return InvocationDTO{
		InvocationID:   meta.InvocationID,
		InvocationName: meta.InvocationName,
		WorktreeID:     meta.IntegrationWorktreeID,
		RepoID:         repoID,
		Runner:         meta.Runner,
		Mode:           string(meta.Mode),
		TmuxSession:    meta.TmuxSession,
		StartedAt:      meta.StartedAt,
		FinishedAt:     meta.FinishedAt,
		LastOutputAt:   meta.LastOutputAt,
		Status:         string(meta.Status),
		ExitReason:     meta.ExitReason,
		ExitCode:       meta.ExitCode,
		SemanticStatus: semanticStatus,
		LandingStatus:  string(meta.LandingStatus),
		DisplayStatus:  derived.DisplayStatus,
		AttentionFlags: derived.AttentionFlags,
		SortKey:        derived.SortKey,
		SandboxPath:    meta.SandboxPath,
		LogsDir:        logsDir,
	}
}

// WorktreeMetaToDTO converts an IntegrationWorktreeMeta to a WorktreeDTO.
func WorktreeMetaToDTO(meta *store.IntegrationWorktreeMeta) WorktreeDTO {
	return WorktreeDTO{
		WorktreeID:   meta.WorktreeID,
		Name:         meta.Name,
		RepoID:       meta.RepoID,
		Branch:       meta.Branch,
		ParentBranch: meta.ParentBranch,
		TreePath:     meta.TreePath,
		State:        string(meta.State),
		CreatedAt:    meta.CreatedAt,
		LastUsedAt:   meta.LastUsedAt,
	}
}
