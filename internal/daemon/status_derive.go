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
//  4. stopping → "stopping"
//  5. needs_attention flag → "needs attention"
//  6. semantic == needs_input → "needs input"
//  7. semantic == blocked → "blocked"
//  8. semantic == ready → "ready"
//  9. running + semantic working → "working"
//  10. running → "running"
//  11. finished → "finished"
//  12. starting → "starting"
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

	// 4. Stopping
	if meta.Status == store.InvocationStatusStopping {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusStopping,
			AttentionFlags: flags,
			SortKey:        SortKeyStopping,
		}
	}

	// 5. Needs attention
	if meta.Flags.NeedsAttention {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusNeedsAttention,
			AttentionFlags: flags,
			SortKey:        SortKeyNeedsAttention,
		}
	}

	// 6. Semantic: needs_input
	if semanticStatus == string(runnerstatus.StatusNeedsInput) {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusNeedsInput,
			AttentionFlags: flags,
			SortKey:        SortKeyNeedsInput,
		}
	}

	// 7. Semantic: blocked
	if semanticStatus == string(runnerstatus.StatusBlocked) {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusBlocked,
			AttentionFlags: flags,
			SortKey:        SortKeyBlocked,
		}
	}

	// 8. Semantic: ready
	if semanticStatus == string(runnerstatus.StatusReady) {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusReady,
			AttentionFlags: flags,
			SortKey:        SortKeyReady,
		}
	}

	// 9. Running + working
	if meta.Status == store.InvocationStatusRunning && semanticStatus == string(runnerstatus.StatusWorking) {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusWorking,
			AttentionFlags: flags,
			SortKey:        SortKeyWorking,
		}
	}

	// 10. Running
	if meta.Status == store.InvocationStatusRunning {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusRunning,
			AttentionFlags: flags,
			SortKey:        SortKeyRunning,
		}
	}

	// 11. Finished
	if meta.Status == store.InvocationStatusFinished {
		return DerivedStatus{
			DisplayStatus:  DisplayStatusFinished,
			AttentionFlags: flags,
			SortKey:        SortKeyFinished,
		}
	}

	// 12. Starting (default)
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
		WorktreeID: meta.WorktreeID,
		Name:       meta.Name,
		RepoID:     meta.RepoID,
		Branch:     meta.Branch,
		BaseBranch: meta.BaseBranch,
		TreePath:   meta.TreePath,
		State:      string(meta.State),
		CreatedAt:  meta.CreatedAt,
		LastUsedAt: meta.LastUsedAt,
	}
}
