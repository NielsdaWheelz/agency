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
	checkReasonInvocationFailed       = "invocation_failed"
	checkReasonInvocationMetaInvalid  = "invocation_metadata_invalid"
	checkReasonAlreadyLanded          = "already_landed"
	checkReasonAlreadyDiscarded       = "already_discarded"
	checkReasonRunnerNeedsInput       = "runner_needs_input"
	checkReasonRunnerBlocked          = "runner_blocked"
	checkReasonRunnerWorking          = "runner_working"
	checkReasonRunnerStatusMissing    = "runner_status_missing"
	checkReasonRunnerStatusUnreadable = "runner_status_unreadable"
	checkReasonRunnerStatusInvalid    = "runner_status_invalid"
	checkReasonRunnerNotReady         = "runner_not_ready_for_review"
)

// handleGetInvocationChecks handles GET /invocations/{ref}/checks.
func (s *Server) handleGetInvocationChecks(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)
	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	repoID := r.URL.Query().Get("repo_id")
	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		code := errors.GetCode(resolveErr)
		status := http.StatusNotFound
		var details interface{}
		if code == errors.EInvocationIDAmbiguous {
			status = http.StatusConflict
			if ae, ok := errors.AsAgencyError(resolveErr); ok && ae.Details != nil {
				if candidates, ok := ae.Details["candidates"]; ok {
					details = AmbiguousDetails{Candidates: strings.Split(candidates, ",")}
				}
			}
		}
		s.writeAPIError(w, status, requestID, string(code), resolveErr.Error(), "use 'agent ls' to list invocations", details)
		return
	}

	checksData := s.buildInvocationChecks(record)
	s.writeAPIResponse(w, requestID, checksData)
}

func (s *Server) buildInvocationChecks(record *resolvedInvocation) InvocationChecksData {
	meta := record.Meta
	derived := DeriveDisplayStatus(meta, s.Clock())

	data := InvocationChecksData{
		InvocationID:    record.InvocationID,
		RepoID:          record.RepoID,
		Status:          string(meta.Status),
		DisplayStatus:   derived.DisplayStatus,
		LandingStatus:   string(meta.LandingStatus),
		BlockingReasons: make([]InvocationCheckReason, 0, 8),
		Navigation: InvocationChecksNavigation{
			InvocationRef:  record.InvocationID,
			RepoID:         record.RepoID,
			HistoryCommand: fmt.Sprintf("agency agent history %s --repo %s", record.InvocationID, record.RepoID),
			DiffCommand:    fmt.Sprintf("agency agent diff %s --repo %s", record.InvocationID, record.RepoID),
		},
	}
	if meta.SemanticStatus != nil {
		data.SemanticStatus = string(*meta.SemanticStatus)
	}

	timelineEntries := s.collectTimelineEntries(record)
	if len(timelineEntries) > 0 {
		latestTurnID := timelineEntries[len(timelineEntries)-1].dto.EntryID
		data.Navigation.LatestTurnID = latestTurnID
		data.Navigation.DiffCommand = fmt.Sprintf("agency agent diff %s --repo %s --turn %s", record.InvocationID, record.RepoID, latestTurnID)
	}

	sandboxPath := strings.TrimSpace(meta.SandboxPath)
	if sandboxPath == "" {
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonInvocationMetaInvalid,
			Message: "invocation metadata is missing sandbox path",
			Hint:    "inspect invocation meta.json and recreate invocation if needed",
		})
	} else if runnerMeta, _, err := runnerstatus.LoadWithModTime(sandboxPath); err != nil {
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

	switch meta.Status {
	case store.InvocationStatusStarting, store.InvocationStatusRunning:
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonInvocationActive,
			Message: "invocation is still active",
			Hint:    "wait for completion before review/merge progression",
		})
	case store.InvocationStatusFailed:
		message := "invocation failed before reaching review-ready state"
		if meta.FailureReason != "" {
			message = fmt.Sprintf("invocation failed (%s)", meta.FailureReason)
		}
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonInvocationFailed,
			Message: message,
			Hint:    "inspect logs and restart from checkpoint if needed",
		})
	}

	switch meta.LandingStatus {
	case store.LandingStatusLanded:
		data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
			Code:    checkReasonAlreadyLanded,
			Message: "invocation is already landed",
			Hint:    "review progression is complete for this invocation",
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
			Hint:    "wait until status becomes ready_for_review",
		})
	}

	if meta.Status == store.InvocationStatusFinished &&
		meta.LandingStatus != store.LandingStatusLanded &&
		meta.LandingStatus != store.LandingStatusDiscarded {
		if effectiveSemantic == "" {
			if !hasCheckReason(data.BlockingReasons, checkReasonRunnerStatusUnreadable) &&
				!hasCheckReason(data.BlockingReasons, checkReasonRunnerStatusInvalid) &&
				!hasCheckReason(data.BlockingReasons, checkReasonInvocationMetaInvalid) {
				data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
					Code:    checkReasonRunnerStatusMissing,
					Message: "no runner readiness status is available",
					Hint:    "ensure .agency/state/runner_status.json is updated",
				})
			}
		} else if effectiveSemantic != string(runnerstatus.StatusReadyForReview) &&
			effectiveSemantic != string(runnerstatus.StatusNeedsInput) &&
			effectiveSemantic != string(runnerstatus.StatusBlocked) &&
			effectiveSemantic != string(runnerstatus.StatusWorking) {
			data.BlockingReasons = append(data.BlockingReasons, InvocationCheckReason{
				Code:    checkReasonRunnerNotReady,
				Message: "runner status is not review-ready",
				Hint:    fmt.Sprintf("current status: %s", effectiveSemantic),
			})
		}
	}

	data.Ready = len(data.BlockingReasons) == 0
	if data.Ready {
		data.Readiness = "ready"
	} else {
		data.Readiness = "blocked"
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

func hasCheckReason(reasons []InvocationCheckReason, code string) bool {
	for _, reason := range reasons {
		if reason.Code == code {
			return true
		}
	}
	return false
}
