package daemon

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const (
	checkReasonInvocationActive       = "invocation_active"
	checkReasonInvocationWaiting      = "invocation_waiting"
	checkReasonInvocationFailed       = "invocation_failed"
	checkReasonLandingPending         = "landing_pending"
	checkReasonAlreadyDiscarded       = "already_discarded"
	checkReasonRunnerStatusMissing    = "runner_status_missing"
	checkReasonRunnerStatusUnreadable = "runner_status_unreadable"
	checkReasonRunnerStatusInvalid    = "runner_status_invalid"
	checkReasonInvalidRunnerState     = "invalid_runner_state"
)

// handleGetInvocationCheck handles GET /invocations/{ref}/check.
func (s *Server) handleGetInvocationCheck(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)
	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	repoID := r.URL.Query().Get("repo_id")
	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agent ls' to list invocations", errors.EInvocationIDAmbiguous)
		return
	}

	checkData := s.buildInvocationCheck(record)
	s.writeAPIResponse(w, requestID, checkData)
}

func (s *Server) buildInvocationCheck(record *resolvedInvocation) InvocationCheckData {
	meta := record.Meta
	runnerMeta, runnerErr := s.loadRunnerStatusForInvocation(record)
	dto := InvocationMetaToDTO(
		meta,
		record.RepoID,
		s.preferredInvocationLogsDir(record.RepoID, record.InvocationID),
		runnerMeta,
		runnerErr,
		s.Clock(),
	)
	timelineEntries := s.collectTimelineEntries(record)
	activityProjection := s.buildInvocationActivityProjection(record, dto.State, "", timelineEntries)
	applyInvocationActivityProjection(&dto, activityProjection)

	data := InvocationCheckData{
		InvocationID:    dto.InvocationID,
		RepoID:          dto.RepoID,
		State:           dto.State,
		Reason:          dto.Reason,
		PRSyncEligible:  dto.PRSyncEligible,
		LandingStatus:   dto.LandingStatus,
		BlockingReasons: make([]InvocationCheckReason, 0, 8),
		Navigation: InvocationCheckNavigation{
			InvocationRef:  dto.InvocationID,
			RepoID:         dto.RepoID,
			HistoryCommand: fmt.Sprintf("agency agent %s history --repo %s", dto.InvocationID, dto.RepoID),
			DiffCommand:    fmt.Sprintf("agency agent %s diff --repo %s", dto.InvocationID, dto.RepoID),
			PRSyncCommand:  fmt.Sprintf("agency worktree %s pr sync --repo %s", firstNonEmpty(strings.TrimSpace(meta.IntegrationWorktreeID), "<worktree_ref>"), dto.RepoID),
		},
	}

	if runnerErr == nil && runnerMeta != nil && runnerMeta.SchemaVersion == runnerstatus.SchemaVersion {
		if err := runnerMeta.Validate(); err == nil {
			data.RunnerState = string(runnerMeta.State)
			data.RunnerReason = strings.TrimSpace(runnerMeta.Reason)
			data.RunnerSummary = runnerMeta.Summary
			data.RunnerUpdatedAt = runnerMeta.UpdatedAt
			data.HowToTest = runnerMeta.HowToTest
		}
	}
	if strings.TrimSpace(data.RunnerSummary) == "" {
		data.RunnerSummary = dto.StatusSummary
	}
	if dto.Navigation != nil {
		if strings.TrimSpace(dto.Navigation.HistoryCommand) != "" {
			data.Navigation.HistoryCommand = dto.Navigation.HistoryCommand
		}
		if strings.TrimSpace(dto.Navigation.DiffCommand) != "" {
			data.Navigation.DiffCommand = dto.Navigation.DiffCommand
		}
		if strings.TrimSpace(dto.Navigation.AttachCommand) != "" {
			data.Navigation.AttachCommand = dto.Navigation.AttachCommand
		}
		data.Navigation.LatestTurnID = dto.Navigation.LatestTurnID
	}

	switch meta.Status {
	case store.InvocationStatusStarting, store.InvocationStatusRunning, store.InvocationStatusStopping:
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonInvocationActive,
			Message: "invocation is still active",
			Hint:    "wait for completion before workflow progression",
		})
	}

	switch data.State {
	case string(InvocationStateWaiting):
		message := "invocation is waiting"
		hint := "send a follow-up prompt or inspect history/logs"
		switch data.Reason {
		case runnerstatus.ReasonAwaitingUserInput:
			message = "invocation is waiting for user input"
			hint = "answer the pending question and continue the invocation"
		case runnerstatus.ReasonAwaitingApproval:
			message = "invocation is waiting for approval"
			hint = "approve the requested action and continue the invocation"
		case runnerstatus.ReasonTurnComplete:
			message = "invocation is waiting for the next prompt"
		}
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonInvocationWaiting,
			Message: message,
			Hint:    hint,
		})
	case string(InvocationStateFailed):
		message := "invocation failed"
		hint := "inspect history/logs and restore a checkpoint if needed"
		switch data.Reason {
		case "runner_status_missing":
			message = "runner status file is missing after invocation completion"
			hint = "ensure .agency/state/runner_status.json is written before the runner exits"
		case "runner_status_unreadable":
			message = "runner status file could not be read"
			if runnerErr != nil {
				hint = runnerErr.Error()
			}
		case "runner_status_invalid":
			message = "runner status file is present but invalid"
			if runnerMeta != nil && runnerMeta.SchemaVersion != runnerstatus.SchemaVersion {
				hint = fmt.Sprintf("expected schema_version %s, got %s", runnerstatus.SchemaVersion, firstNonEmpty(runnerMeta.SchemaVersion, "<empty>"))
			} else if runnerMeta != nil {
				if err := runnerMeta.Validate(); err != nil {
					hint = err.Error()
				}
			}
		case "invalid_runner_state":
			message = "runner finished with a non-terminal state"
			hint = "runner must write waiting, succeeded, or failed before exiting"
		default:
			if strings.TrimSpace(data.Reason) != "" {
				message = fmt.Sprintf("invocation failed (%s)", data.Reason)
			}
		}
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonInvocationFailed,
			Message: message,
			Hint:    hint,
		})
	}

	switch meta.LandingStatus {
	case store.LandingStatusPending:
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonLandingPending,
			Message: "invocation changes are not landed into integration yet",
			Hint:    "run 'agency agent <invocation_ref> land' before 'agency worktree <worktree_ref> pr sync'",
		})
	case store.LandingStatusDiscarded:
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonAlreadyDiscarded,
			Message: "invocation is already discarded",
			Hint:    "diff and workflow progression are no longer applicable",
		})
	}

	if meta.Status == store.InvocationStatusFinished {
		if runnerErr != nil {
			data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
				Code:    checkReasonRunnerStatusUnreadable,
				Message: "runner status file could not be read",
				Hint:    runnerErr.Error(),
			})
		} else if runnerMeta == nil {
			data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
				Code:    checkReasonRunnerStatusMissing,
				Message: "no runner status file is available",
				Hint:    "ensure .agency/state/runner_status.json is updated before the runner exits",
			})
		} else if runnerMeta.SchemaVersion != runnerstatus.SchemaVersion {
			data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
				Code:    checkReasonRunnerStatusInvalid,
				Message: "runner status file schema_version is unsupported",
				Hint:    fmt.Sprintf("expected %s, got %s", runnerstatus.SchemaVersion, firstNonEmpty(runnerMeta.SchemaVersion, "<empty>")),
			})
		} else if err := runnerMeta.Validate(); err != nil {
			data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
				Code:    checkReasonRunnerStatusInvalid,
				Message: "runner status file is present but invalid",
				Hint:    err.Error(),
			})
		} else if runnerMeta.State != runnerstatus.StateWaiting &&
			runnerMeta.State != runnerstatus.StateSucceeded &&
			runnerMeta.State != runnerstatus.StateFailed {
			data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
				Code:    checkReasonInvalidRunnerState,
				Message: "runner finished with a non-terminal state",
				Hint:    fmt.Sprintf("current runner state: %s", runnerMeta.State),
			})
		}
	}
	return data
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
