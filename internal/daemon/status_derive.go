// Package daemon implements the agency daemon supervisor.
package daemon

import (
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// StallThreshold is the duration after which an invocation with no output is considered stalled.
const StallThreshold = 5 * time.Minute

// InvocationMetaToDTO converts an InvocationMeta to an InvocationDTO.
func InvocationMetaToDTO(
	meta *store.InvocationMeta,
	repoID string,
	logsDir string,
	runnerMeta *runnerstatus.RunnerStatus,
	runnerStatusErr error,
	now time.Time,
) InvocationDTO {
	flags := make([]string, 0, 4)
	if meta.Flags.NeedsAttention {
		flags = append(flags, AttentionFlagNeedsAttention)
	}
	if meta.Flags.Orphaned {
		flags = append(flags, AttentionFlagOrphaned)
	}
	if meta.Status == store.InvocationStatusRunning && meta.LastOutputAt != "" {
		lastOutput, err := time.Parse(time.RFC3339, meta.LastOutputAt)
		if err == nil && now.Sub(lastOutput) > StallThreshold {
			flags = append(flags, AttentionFlagStalled)
		}
	}
	if meta.Status == store.InvocationStatusFinished &&
		meta.LandingStatus != store.LandingStatusLanded &&
		meta.LandingStatus != store.LandingStatusDiscarded {
		flags = append(flags, AttentionFlagLandable)
	}

	runnerState := ""
	runnerReason := ""
	runnerValid := false
	if runnerStatusErr == nil && runnerMeta != nil && runnerMeta.SchemaVersion == runnerstatus.SchemaVersion {
		if err := runnerMeta.Validate(); err == nil {
			runnerState = string(runnerMeta.State)
			runnerReason = strings.TrimSpace(runnerMeta.Reason)
			runnerValid = true
		}
	}

	state := InvocationStateStarting
	reason := ""
	sortKey := SortKeyStarting

	switch meta.Status {
	case store.InvocationStatusStarting:
		state = InvocationStateStarting
		sortKey = SortKeyStarting
	case store.InvocationStatusStopping:
		state = InvocationStateStopping
		sortKey = SortKeyStopping
	case store.InvocationStatusFailed:
		state = InvocationStateFailed
		reason = strings.TrimSpace(meta.FailureReason)
		sortKey = SortKeyFailed
	case store.InvocationStatusRunning:
		state = InvocationStateRunning
		sortKey = SortKeyRunning
		switch runnerState {
		case string(runnerstatus.StateWaiting):
			state = InvocationStateWaiting
			reason = runnerReason
			sortKey = SortKeyWaiting
		case string(runnerstatus.StateFailed):
			state = InvocationStateFailed
			reason = firstNonEmpty(runnerReason, strings.TrimSpace(meta.FailureReason))
			sortKey = SortKeyFailed
		}
	case store.InvocationStatusFinished:
		switch {
		case runnerStatusErr != nil:
			state = InvocationStateFailed
			reason = "runner_status_unreadable"
			sortKey = SortKeyFailed
		case runnerMeta == nil:
			state = InvocationStateFailed
			reason = "runner_status_missing"
			sortKey = SortKeyFailed
		case runnerMeta.SchemaVersion != runnerstatus.SchemaVersion:
			state = InvocationStateFailed
			reason = "runner_status_invalid"
			sortKey = SortKeyFailed
		case !runnerValid:
			state = InvocationStateFailed
			reason = "runner_status_invalid"
			sortKey = SortKeyFailed
		case runnerState == string(runnerstatus.StateSucceeded):
			state = InvocationStateSucceeded
			sortKey = SortKeySucceeded
		case runnerState == string(runnerstatus.StateWaiting):
			state = InvocationStateWaiting
			reason = runnerReason
			sortKey = SortKeyWaiting
		case runnerState == string(runnerstatus.StateFailed):
			state = InvocationStateFailed
			reason = firstNonEmpty(runnerReason, "runner_failed")
			sortKey = SortKeyFailed
		default:
			state = InvocationStateFailed
			reason = "invalid_runner_state"
			sortKey = SortKeyFailed
		}
	}

	if meta.LandingStatus == store.LandingStatusLanded {
		sortKey = SortKeyLanded
	}
	if meta.LandingStatus == store.LandingStatusDiscarded {
		sortKey = SortKeyDiscarded
	}
	if meta.Flags.NeedsAttention && sortKey > SortKeyNeedsAttention {
		sortKey = SortKeyNeedsAttention
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
		State:          string(state),
		Reason:         reason,
		ExitReason:     meta.ExitReason,
		ExitCode:       meta.ExitCode,
		LandingStatus:  string(meta.LandingStatus),
		AttentionFlags: flags,
		SortKey:        sortKey,
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
