package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/daemon/relay"
	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/jsonl"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const followUpPromptEventKind = "agency.followup_prompt"

// handleControlPlaneFollowUp handles POST /invocations/{ref}/followup.
func (s *Server) handleControlPlaneFollowUp(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := prepareRequestID(w, r)

	// Enforce explicit repo scoping for mutating operations.
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeFollowUpError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "repo_id query parameter is required", "", "")
		return
	}

	var req ControlPlaneFollowUpRequest
	if err := decodeStrictJSON(r.Body, &req); err != nil {
		s.writeFollowUpError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), strictJSONDecodeErrorMessage(err), "", "")
		return
	}

	if req.ClientRequestID == "" {
		s.writeFollowUpError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "client_request_id is required", "provide a stable request identity for idempotent retries", "")
		return
	}
	if req.Prompt == "" {
		s.writeFollowUpError(w, http.StatusBadRequest, requestID, string(errors.EPromptRequired), "prompt is required", "", req.ClientRequestID)
		return
	}
	if len(req.Prompt) > MaxPromptSize {
		s.writeFollowUpError(w, http.StatusBadRequest, requestID, string(errors.EPromptTooLarge),
			fmt.Sprintf("prompt exceeds maximum size of %d bytes (got %d)", MaxPromptSize, len(req.Prompt)),
			"reduce prompt size or split into smaller chunks", req.ClientRequestID)
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		code := errors.CodeOr(resolveErr, errors.EInvocationNotFound)
		s.writeFollowUpError(w, http.StatusNotFound, requestID, string(code), resolveErr.Error(), "use 'agency agent ls --repo <repo>' to list invocations", req.ClientRequestID)
		return
	}

	if record.Meta == nil {
		s.writeFollowUpError(w, http.StatusInternalServerError, requestID, string(errors.EInvocationBroken),
			"invocation exists but meta.json is unreadable", "", req.ClientRequestID)
		return
	}
	if record.Meta.Mode != store.RunnerModeHeadless {
		s.writeFollowUpError(w, http.StatusBadRequest, requestID, string(errors.EInvocationInvalidMode),
			"follow-up prompt is only supported for headless invocations", "", req.ClientRequestID)
		return
	}
	if record.Meta.Status != store.InvocationStatusRunning {
		s.writeFollowUpError(w, http.StatusConflict, requestID, string(errors.EInvocationNotRunning),
			"invocation is not running", "start a new invocation before sending follow-up prompts", req.ClientRequestID)
		return
	}

	if existing, found, err := s.findFollowUpClientRequest(record.RepoID, req.ClientRequestID); err != nil {
		s.writeFollowUpError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", "failed to check follow-up idempotency: "+err.Error(), "", req.ClientRequestID)
		return
	} else if found && (existing.InvocationID != record.InvocationID || existing.Prompt != req.Prompt) {
		s.writeFollowUpError(w, http.StatusConflict, requestID, string(errors.EIdempotencyConflict),
			"client_request_id was already used for a different follow-up request",
			"retry with the original request or choose a new client_request_id", req.ClientRequestID)
		return
	}

	if _, _, conflict := s.reserveFollowUpIdempotency(record.RepoID, req.ClientRequestID, record.InvocationID, followUpFingerprint(record.InvocationID, req.Prompt)); conflict {
		s.writeFollowUpError(w, http.StatusConflict, requestID, string(errors.EIdempotencyConflict),
			"client_request_id was already used for a different follow-up request",
			"retry with the original request or choose a new client_request_id", req.ClientRequestID)
		return
	}

	eventsPath := s.Store.InvocationEventsPath(record.RepoID, record.InvocationID)
	timelineEntryID, alreadyApplied, err := s.appendFollowUpPromptEvent(eventsPath, record.InvocationID, req.ClientRequestID, req.Prompt)
	if err != nil {
		s.writeFollowUpError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", "failed to append follow-up prompt event: "+err.Error(), "", req.ClientRequestID)
		return
	}

	// Deliver via follow-up relay (audit event is already persisted above).
	deliveryMode := s.deliverFollowUp(record.InvocationID, req.Prompt)

	s.writeFollowUpSuccessWithDelivery(w, record.InvocationID, timelineEntryID, req.ClientRequestID, requestID, alreadyApplied, deliveryMode)
}

// deliverFollowUp sends the prompt to the runner via its follow-up relay.
// Returns the delivery mode string for the API response.
// Delivery failure is non-fatal — the audit event is already persisted.
func (s *Server) deliverFollowUp(invocationID, prompt string) string {
	s.mu.Lock()
	proc, ok := s.processes[invocationID]
	s.mu.Unlock()

	if !ok || proc.Relay == nil {
		return "audit_only"
	}

	switch proc.Relay.Mode() {
	case relay.ModeStdin:
		proc.IncrementExpectedTurns()
		if err := proc.Relay.Send(context.Background(), prompt); err != nil {
			// Delivery failure is best-effort; the audit event is durable.
			proc.DecrementExpectedTurns()
			if proc.SuccessfulCompletionObserved() {
				s.scheduleStdinCompletionFinalize(proc)
			}
			return "audit_only"
		}
		return "delivered"
	case relay.ModeResume:
		proc.IncrementExpectedTurns()
		if err := proc.Relay.Send(context.Background(), prompt); err != nil {
			proc.DecrementExpectedTurns()
			return "audit_only"
		}
		return "queued"
	default:
		if err := proc.Relay.Send(context.Background(), prompt); err != nil {
			// Delivery failure is best-effort; the audit event is durable.
			return "audit_only"
		}
		return "audit_only"
	}
}

func (s *Server) appendFollowUpPromptEvent(eventsPath, invocationID, clientRequestID, prompt string) (string, bool, error) {
	writer := s.InvocationEvents
	if writer == nil {
		writer = eventlog.NewWriter("invocation_id", s.Clock)
	}

	result, err := writer.Append(
		eventsPath,
		invocationID,
		followUpPromptEventKind,
		map[string]any{
			"text":              prompt,
			"client_request_id": clientRequestID,
		},
		eventlog.AppendOptions{
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

type followUpClientRequest struct {
	InvocationID string
	Prompt       string
}

func (s *Server) findFollowUpClientRequest(repoID, clientRequestID string) (followUpClientRequest, bool, error) {
	records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return followUpClientRequest{}, false, err
	}
	for _, record := range records {
		existing, found, err := s.findFollowUpClientRequestInFile(s.Store.InvocationEventsPath(repoID, record.InvocationID), clientRequestID)
		if err != nil {
			return followUpClientRequest{}, false, err
		}
		if found {
			return existing, true, nil
		}
	}
	return followUpClientRequest{}, false, nil
}

func (s *Server) findFollowUpClientRequestInFile(eventsPath, clientRequestID string) (followUpClientRequest, bool, error) {
	f, err := os.Open(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return followUpClientRequest{}, false, nil
		}
		return followUpClientRequest{}, false, err
	}
	defer func() { _ = f.Close() }()

	var existing followUpClientRequest
	found := false
	err = jsonl.Visit(f, stream.MaxLineSize, jsonl.VisitOptions{OversizedPrefixBytes: 0}, func(scanned jsonl.Line) error {
		if found || scanned.Oversized {
			return nil
		}
		var event struct {
			InvocationID string         `json:"invocation_id"`
			Kind         string         `json:"kind"`
			Data         map[string]any `json:"data"`
		}
		if err := json.Unmarshal(scanned.Bytes, &event); err != nil || event.Kind != followUpPromptEventKind {
			return nil
		}
		if event.Data == nil || event.Data["client_request_id"] != clientRequestID {
			return nil
		}
		prompt, _ := event.Data["text"].(string)
		existing = followUpClientRequest{InvocationID: event.InvocationID, Prompt: prompt}
		found = true
		return nil
	})
	if err != nil {
		return followUpClientRequest{}, false, err
	}
	return existing, found, nil
}
