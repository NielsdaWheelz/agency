package daemon

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/report"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const (
	checkReasonInvocationActive       = "invocation_active"
	checkReasonInvocationFailed       = "invocation_failed"
	checkReasonLandingPending         = "landing_pending"
	checkReasonAlreadyDiscarded       = "already_discarded"
	checkReasonRunnerNeedsInput       = "runner_needs_input"
	checkReasonRunnerBlocked          = "runner_blocked"
	checkReasonRunnerWorking          = "runner_working"
	checkReasonRunnerStatusMissing    = "runner_status_missing"
	checkReasonRunnerStatusUnreadable = "runner_status_unreadable"
	checkReasonRunnerStatusInvalid    = "runner_status_invalid"
	checkReasonRunnerNotReady         = "runner_not_ready"
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
	derived := DeriveDisplayStatus(meta, s.Clock())

	data := InvocationCheckData{
		InvocationID:    record.InvocationID,
		RepoID:          record.RepoID,
		Status:          string(meta.Status),
		DisplayStatus:   derived.DisplayStatus,
		LandingStatus:   string(meta.LandingStatus),
		BlockingReasons: make([]InvocationCheckReason, 0, 8),
		Navigation: InvocationCheckNavigation{
			InvocationRef:  record.InvocationID,
			RepoID:         record.RepoID,
			HistoryCommand: fmt.Sprintf("agency agent %s history --repo %s", record.InvocationID, record.RepoID),
			DiffCommand:    fmt.Sprintf("agency agent %s diff --repo %s", record.InvocationID, record.RepoID),
			PRSyncCommand:  fmt.Sprintf("agency worktree %s pr sync --repo %s", firstNonEmpty(strings.TrimSpace(meta.IntegrationWorktreeID), "<worktree_ref>"), record.RepoID),
		},
	}
	if meta.SemanticStatus != nil {
		data.SemanticStatus = string(*meta.SemanticStatus)
	}

	timelineEntries := s.collectTimelineEntries(record)

	if runnerMeta, err := s.loadRunnerStatusForInvocation(record); err != nil {
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonRunnerStatusUnreadable,
			Message: "runner status file could not be read",
			Hint:    err.Error(),
		})
	} else if runnerMeta != nil {
		if runnerMeta.SchemaVersion != runnerstatus.SchemaVersion {
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
		} else {
			data.RunnerStatus = string(runnerMeta.Status)
			data.RunnerSummary = runnerMeta.Summary
			data.RunnerUpdatedAt = runnerMeta.UpdatedAt
			data.HowToTest = runnerMeta.HowToTest
		}
	}

	activityProjection := s.buildInvocationActivityProjection(record, data.DisplayStatus, data.RunnerSummary, timelineEntries)
	data.StatusSummary = activityProjection.StatusSummary
	data.LatestActivity = activityProjection.LatestActivity
	if strings.TrimSpace(data.RunnerSummary) == "" {
		data.RunnerSummary = activityProjection.StatusSummary
	}
	if activityProjection.Navigation != nil {
		if strings.TrimSpace(activityProjection.Navigation.HistoryCommand) != "" {
			data.Navigation.HistoryCommand = activityProjection.Navigation.HistoryCommand
		}
		if strings.TrimSpace(activityProjection.Navigation.DiffCommand) != "" {
			data.Navigation.DiffCommand = activityProjection.Navigation.DiffCommand
		}
		data.Navigation.LatestTurnID = activityProjection.Navigation.LatestTurnID
	}

	switch meta.Status {
	case store.InvocationStatusStarting, store.InvocationStatusRunning, store.InvocationStatusStopping:
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonInvocationActive,
			Message: "invocation is still active",
			Hint:    "wait for completion before check/merge progression",
		})
	case store.InvocationStatusFailed:
		message := "invocation failed before reaching ready state"
		if meta.FailureReason != "" {
			message = fmt.Sprintf("invocation failed (%s)", meta.FailureReason)
		}
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonInvocationFailed,
			Message: message,
			Hint:    "inspect history/logs and restore a checkpoint if needed",
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
			Hint:    "diff and readiness progression are no longer applicable",
		})
	}

	effectiveSemantic := data.SemanticStatus
	if effectiveSemantic == "" && data.RunnerStatus != "" {
		effectiveSemantic = data.RunnerStatus
	}

	switch effectiveSemantic {
	case string(runnerstatus.StatusNeedsInput):
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonRunnerNeedsInput,
			Message: "runner status requires human input",
			Hint:    "resolve questions and continue the invocation",
		})
	case string(runnerstatus.StatusBlocked):
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonRunnerBlocked,
			Message: "runner reported blocked status",
			Hint:    firstNonEmpty(data.RunnerSummary, "address blockers and continue the invocation"),
		})
	case string(runnerstatus.StatusWorking):
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonRunnerWorking,
			Message: "runner is still working",
			Hint:    "wait until status becomes ready",
		})
	}

	if meta.Status == store.InvocationStatusFinished &&
		meta.LandingStatus != store.LandingStatusDiscarded {
		if effectiveSemantic == "" {
			if !hasCheckReason(data.BlockingReasons, checkReasonRunnerStatusUnreadable) &&
				!hasCheckReason(data.BlockingReasons, checkReasonRunnerStatusInvalid) {
				data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
					Code:    checkReasonRunnerStatusMissing,
					Message: "no runner readiness status is available",
					Hint:    "ensure .agency/state/runner_status.json is updated",
				})
			}
		} else if effectiveSemantic != string(runnerstatus.StatusReady) &&
			effectiveSemantic != string(runnerstatus.StatusNeedsInput) &&
			effectiveSemantic != string(runnerstatus.StatusBlocked) &&
			effectiveSemantic != string(runnerstatus.StatusWorking) {
			data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
				Code:    checkReasonRunnerNotReady,
				Message: "runner status is not ready",
				Hint:    fmt.Sprintf("current status: %s", effectiveSemantic),
			})
		}
	}

	if meta.Mode == store.RunnerModeHeadless &&
		meta.Status == store.InvocationStatusFinished &&
		meta.LandingStatus == store.LandingStatusLanded {
		wtMeta, wtErr := s.Store.ReadIntegrationWorktreeMeta(record.RepoID, meta.IntegrationWorktreeID)
		if wtErr != nil {
			data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
				Code:    string(report.ViolationMalformed),
				Message: "report contract could not be evaluated",
				Hint:    "integration worktree metadata is missing or unreadable",
			})
		} else {
			resolution, violation, resolveErr := report.ResolveCanonicalReport(s.FS, wtMeta.TreePath, report.ResolveOptions{
				MaxBytes: report.MaxPRBodyReportBytes,
			})
			if resolveErr != nil {
				data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
					Code:    string(report.ViolationMalformed),
					Message: "report contract could not be evaluated",
					Hint:    "failed to read report artifacts",
				})
			} else if violation != nil {
				data.BlockingReasons = append(data.BlockingReasons, reportViolationToCheckReason(violation))
			} else if resolution != nil {
				data.ReportSource = string(resolution.Source)
				data.ReportDiagnostics = reportDiagnostics(resolution.Diagnostics)
			}
		}
	}

	data.Ready = len(data.BlockingReasons) == 0
	if data.Ready {
		data.Readiness = "ready"
	} else {
		data.Readiness = "blocked"
	}
	data.PRSyncEligible = data.Ready

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

func hasCheckReason(reasons []InvocationCheckReason, code string) bool {
	for _, reason := range reasons {
		if reason.Code == code {
			return true
		}
	}
	return false
}
