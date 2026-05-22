// Package daemon implements the agency daemon supervisor.
package daemon

import (
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// stallThreshold is the duration after which an invocation with no output is considered stalled.
const stallThreshold = 5 * time.Minute

// invocationMetaToDTO converts an InvocationMeta to an InvocationDTO.
func invocationMetaToDTO(
	meta *store.InvocationMeta,
	repoID string,
	logsDir string,
	runnerMeta *runnerstatus.RunnerStatus,
	runnerStatusErr error,
	now time.Time,
) InvocationDTO {
	flags := make([]string, 0, 4)
	if meta.Flags.NeedsAttention {
		flags = append(flags, attentionFlagNeedsAttention)
	}
	if meta.Flags.Orphaned {
		flags = append(flags, attentionFlagOrphaned)
	}
	if meta.Status == store.InvocationStatusRunning && meta.LastOutputAt != "" {
		lastOutput, err := time.Parse(time.RFC3339, meta.LastOutputAt)
		if err == nil && now.Sub(lastOutput) > stallThreshold {
			flags = append(flags, attentionFlagStalled)
		}
	}
	if meta.Status == store.InvocationStatusFinished &&
		meta.LandingStatus != store.LandingStatusLanded &&
		meta.LandingStatus != store.LandingStatusDiscarded {
		flags = append(flags, attentionFlagLandable)
	}

	runnerState, runnerReason, _, runnerValid := projectRunnerStatus(runnerMeta, runnerStatusErr)

	state := invocationStateStarting
	reason := ""
	sortKey := sortKeyStarting

	switch meta.Status {
	case store.InvocationStatusStarting:
		state = invocationStateStarting
		sortKey = sortKeyStarting
	case store.InvocationStatusStopping:
		state = invocationStateStopping
		sortKey = sortKeyStopping
	case store.InvocationStatusFailed:
		state = invocationStateFailed
		reason = strings.TrimSpace(meta.FailureReason)
		sortKey = sortKeyFailed
	case store.InvocationStatusRunning:
		state = invocationStateRunning
		sortKey = sortKeyRunning
		switch runnerState {
		case string(runnerstatus.StateWaiting):
			state = invocationStateWaiting
			reason = runnerReason
			sortKey = sortKeyWaiting
		case string(runnerstatus.StateFailed):
			state = invocationStateFailed
			reason = firstNonEmpty(runnerReason, strings.TrimSpace(meta.FailureReason))
			sortKey = sortKeyFailed
		}
	case store.InvocationStatusFinished:
		switch {
		case runnerStatusErr != nil:
			state = invocationStateFailed
			reason = "runner_status_unreadable"
			sortKey = sortKeyFailed
		case runnerMeta == nil:
			state = invocationStateFailed
			reason = "runner_status_missing"
			sortKey = sortKeyFailed
		case runnerMeta.SchemaVersion != runnerstatus.SchemaVersion:
			state = invocationStateFailed
			reason = "runner_status_invalid"
			sortKey = sortKeyFailed
		case !runnerValid:
			state = invocationStateFailed
			reason = "runner_status_invalid"
			sortKey = sortKeyFailed
		case runnerState == string(runnerstatus.StateSucceeded):
			state = invocationStateSucceeded
			sortKey = sortKeySucceeded
		case runnerState == string(runnerstatus.StateWaiting):
			state = invocationStateWaiting
			reason = runnerReason
			sortKey = sortKeyWaiting
		case runnerState == string(runnerstatus.StateFailed):
			state = invocationStateFailed
			reason = firstNonEmpty(runnerReason, "runner_failed")
			sortKey = sortKeyFailed
		default:
			state = invocationStateFailed
			reason = "invalid_runner_state"
			sortKey = sortKeyFailed
		}
	}

	if meta.LandingStatus == store.LandingStatusLanded {
		sortKey = sortKeyLanded
	}
	if meta.LandingStatus == store.LandingStatusDiscarded {
		sortKey = sortKeyDiscarded
	}
	if meta.Flags.NeedsAttention && sortKey > sortKeyNeedsAttention {
		sortKey = sortKeyNeedsAttention
	}

	return InvocationDTO{
		InvocationID:     meta.InvocationID,
		InvocationName:   meta.InvocationName,
		WorktreeID:       meta.IntegrationWorktreeID,
		RepoID:           repoID,
		Runner:           meta.Runner,
		Mode:             string(meta.Mode),
		TmuxSession:      meta.TmuxSession,
		CheckoutRoot:     meta.CheckoutRoot,
		ExecutionProfile: meta.ExecutionProfile,
		StartedAt:        meta.StartedAt,
		FinishedAt:       meta.FinishedAt,
		LastOutputAt:     meta.LastOutputAt,
		State:            string(state),
		Reason:           reason,
		ExitReason:       meta.ExitReason,
		ExitCode:         meta.ExitCode,
		LandingStatus:    string(meta.LandingStatus),
		PRSyncEligible:   invocationPRSyncEligible(state, meta.LandingStatus),
		AttentionFlags:   flags,
		SortKey:          sortKey,
		SandboxPath:      meta.SandboxPath,
		LogsDir:          logsDir,
	}
}

func projectRunnerStatus(
	runnerMeta *runnerstatus.RunnerStatus,
	runnerStatusErr error,
) (state string, reason string, summary string, valid bool) {
	if runnerStatusErr != nil || runnerMeta == nil || runnerMeta.SchemaVersion != runnerstatus.SchemaVersion {
		return "", "", "", false
	}
	if err := runnerMeta.Validate(); err != nil {
		return "", "", "", false
	}
	return string(runnerMeta.State), strings.TrimSpace(runnerMeta.Reason), strings.TrimSpace(runnerMeta.Summary), true
}

// worktreeMetaToDTO converts an IntegrationWorktreeMeta and optional merge state to a WorktreeDTO.
func worktreeMetaToDTO(meta *store.IntegrationWorktreeMeta, mergeMeta *store.IntegrationWorktreeMergeMeta) WorktreeDTO {
	return WorktreeDTO{
		WorktreeID:       meta.WorktreeID,
		WorktreeName:     strings.TrimSpace(meta.Name),
		RepoID:           meta.RepoID,
		Branch:           meta.Branch,
		BaseBranch:       meta.BaseBranch,
		TreePath:         meta.TreePath,
		CheckoutRoot:     meta.CheckoutRoot,
		ExecutionProfile: meta.ExecutionProfile,
		State:            string(meta.State),
		CreatedAt:        meta.CreatedAt,
		LastUsedAt:       meta.LastUsedAt,
		Merge:            worktreeMergeMetaToDTO(mergeMeta),
	}
}

func invocationPRSyncEligible(state invocationState, landingStatus store.LandingStatus) bool {
	switch landingStatus {
	case store.LandingStatusLanded:
		return true
	case store.LandingStatusPending, store.LandingStatusDiscarded:
		return false
	}

	switch state {
	case invocationStateSucceeded:
		return true
	case invocationStateStarting,
		invocationStateRunning,
		invocationStateWaiting,
		invocationStateStopping,
		invocationStateFailed:
		return false
	default:
		return false
	}
}

// worktreeMergeMetaToDTO converts durable merge state to the canonical daemon read shape.
func worktreeMergeMetaToDTO(meta *store.IntegrationWorktreeMergeMeta) *WorktreeMergeDTO {
	if meta == nil {
		return nil
	}
	return &WorktreeMergeDTO{
		AttemptID:      meta.AttemptID,
		RequestID:      meta.RequestID,
		State:          string(meta.Status),
		Stage:          string(meta.Stage),
		StatusSummary:  worktreeMergeStatusSummary(meta),
		Strategy:       meta.Strategy,
		DeleteBranch:   meta.DeleteBranch,
		Branch:         meta.Branch,
		PRNumber:       meta.PRNumber,
		PRURL:          meta.PRURL,
		MergeLogPath:   meta.MergeLogPath,
		VerifyLogPath:  meta.VerifyLogPath,
		ArchiveLogPath: meta.ArchiveLogPath,
		StartedAt:      meta.StartedAt,
		UpdatedAt:      meta.UpdatedAt,
		FinishedAt:     meta.FinishedAt,
		ErrorCode:      meta.ErrorCode,
		ErrorMessage:   meta.ErrorMessage,
		Hint:           meta.Hint,
	}
}

func worktreeMergeStatusSummary(meta *store.IntegrationWorktreeMergeMeta) string {
	if meta == nil {
		return ""
	}

	switch meta.Status {
	case store.WorktreeMergeStatusRunning:
		switch meta.Stage {
		case store.WorktreeMergeStagePreflight:
			return "preparing merge"
		case store.WorktreeMergeStageVerify:
			return "running verify"
		case store.WorktreeMergeStageMerge:
			return "merging pull request"
		case store.WorktreeMergeStageArchive:
			return "archiving worktree"
		case store.WorktreeMergeStageCompleted:
			return "finishing merge"
		}
	case store.WorktreeMergeStatusSucceeded:
		return "merge complete"
	case store.WorktreeMergeStatusFailed:
		switch meta.Stage {
		case store.WorktreeMergeStageVerify:
			return "merge failed during verify"
		case store.WorktreeMergeStageMerge:
			return "merge failed during pull request merge"
		case store.WorktreeMergeStageArchive:
			return "merge failed during archive cleanup"
		case store.WorktreeMergeStageCompleted:
			return "merge failed"
		default:
			return "merge failed before completion"
		}
	}

	return ""
}
