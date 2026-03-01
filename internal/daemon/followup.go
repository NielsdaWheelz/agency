package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

const followUpPromptEventKind = "agency.followup_prompt"

// handleControlPlaneFollowUpPrompt handles POST /invocations/{ref}/chat (S3 PR-02).
func (s *Server) handleControlPlaneFollowUpPrompt(w http.ResponseWriter, r *http.Request, invocationRef string) {
	// Enforce explicit repo scoping for mutating operations.
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeFollowUpError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "repo_id query parameter is required", "", "")
		return
	}

	var req ControlPlaneFollowUpPromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeFollowUpError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "invalid request body: "+err.Error(), "", "")
		return
	}

	if req.ClientRequestID == "" {
		s.writeFollowUpError(w, http.StatusBadRequest, "E_INVALID_REQUEST", "client_request_id is required", "provide a stable request identity for idempotent retries", "")
		return
	}
	if req.Prompt == "" {
		s.writeFollowUpError(w, http.StatusBadRequest, string(errors.EPromptRequired), "prompt is required", "", req.ClientRequestID)
		return
	}
	if len(req.Prompt) > MaxPromptSize {
		s.writeFollowUpError(w, http.StatusBadRequest, string(errors.EPromptTooLarge),
			fmt.Sprintf("prompt exceeds maximum size of %d bytes (got %d)", MaxPromptSize, len(req.Prompt)),
			"reduce prompt size or split into smaller chunks", req.ClientRequestID)
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		code := errors.GetCode(resolveErr)
		if code == "" {
			code = errors.EInvocationNotFound
		}
		s.writeFollowUpError(w, http.StatusNotFound, string(code), resolveErr.Error(), "use 'agency agent ls --repo <repo>' to list invocations", req.ClientRequestID)
		return
	}

	if record.Meta == nil {
		s.writeFollowUpError(w, http.StatusInternalServerError, string(errors.EInvocationBroken),
			"invocation exists but meta.json is unreadable", "", req.ClientRequestID)
		return
	}
	if record.Meta.Mode != store.RunnerModeHeadless {
		s.writeFollowUpError(w, http.StatusBadRequest, string(errors.EInvocationInvalidMode),
			"follow-up prompt is only supported for headless invocations", "", req.ClientRequestID)
		return
	}
	if record.Meta.Status != store.InvocationStatusRunning {
		s.writeFollowUpError(w, http.StatusConflict, string(errors.EInvocationNotRunning),
			"invocation is not running", "start or restart the invocation before sending follow-up prompts", req.ClientRequestID)
		return
	}

	eventsPath := s.Store.InvocationEventsPath(record.RepoID, record.InvocationID)
	timelineEntryID, alreadyApplied, err := s.appendFollowUpPromptEvent(eventsPath, record.InvocationID, req.ClientRequestID, req.Prompt)
	if err != nil {
		s.writeFollowUpError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to append follow-up prompt event: "+err.Error(), "", req.ClientRequestID)
		return
	}

	s.writeFollowUpSuccess(w, record.InvocationID, timelineEntryID, req.ClientRequestID, alreadyApplied)
}

func (s *Server) writeFollowUpError(w http.ResponseWriter, status int, code, message, hint, clientRequestID string) {
	resp := ControlPlaneFollowUpPromptResponse{
		OK:              false,
		ErrorCode:       code,
		Message:         message,
		Hint:            hint,
		APIVersion:      APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: clientRequestID,
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) writeFollowUpSuccess(w http.ResponseWriter, invocationID, timelineEntryID, clientRequestID string, alreadyApplied bool) {
	resp := ControlPlaneFollowUpPromptResponse{
		OK:              true,
		InvocationID:    invocationID,
		TimelineEntry:   timelineEntryID,
		AlreadyApplied:  alreadyApplied,
		APIVersion:      APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: clientRequestID,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) appendFollowUpPromptEvent(eventsPath, invocationID, clientRequestID, prompt string) (string, bool, error) {
	writer := s.InvocationEvents
	if writer == nil {
		writer = invocationevents.NewWriter(s.Clock)
	}

	result, err := writer.Append(
		eventsPath,
		invocationID,
		followUpPromptEventKind,
		map[string]any{
			"text":              prompt,
			"client_request_id": clientRequestID,
		},
		invocationevents.AppendOptions{
			IdempotencyDataKey:   "client_request_id",
			IdempotencyDataValue: clientRequestID,
		},
	)
	if err != nil {
		return "", false, err
	}
	return followUpTimelineEntryID(result.Seq), result.AlreadyApplied, nil
}

func followUpTimelineEntryID(seq uint64) string {
	return "inv_event:" + strconv.FormatUint(seq, 10) + ":" + followUpPromptEventKind
}
