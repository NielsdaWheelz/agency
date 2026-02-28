package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

const (
	followUpPromptEventKind   = "agency.followup_prompt"
	maxFollowUpEventLineBytes = 4 * 1024 * 1024
)

type invocationEventLine struct {
	SchemaVersion string         `json:"schema_version"`
	Seq           uint64         `json:"seq"`
	Timestamp     string         `json:"timestamp"`
	InvocationID  string         `json:"invocation_id"`
	Kind          string         `json:"kind"`
	Data          map[string]any `json:"data,omitempty"`
}

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
	s.followUpMu.Lock()
	defer s.followUpMu.Unlock()

	if existingSeq, duplicate, err := findFollowUpPromptSeq(eventsPath, clientRequestID); err != nil {
		return "", false, err
	} else if duplicate {
		return followUpTimelineEntryID(existingSeq), true, nil
	}

	maxSeq, err := loadMaxInvocationEventSeq(eventsPath)
	if err != nil {
		return "", false, err
	}
	seq := maxSeq + 1

	event := invocationEventLine{
		SchemaVersion: "1.0",
		Seq:           seq,
		Timestamp:     s.Clock().UTC().Format(time.RFC3339),
		InvocationID:  invocationID,
		Kind:          followUpPromptEventKind,
		Data: map[string]any{
			"text":              prompt,
			"client_request_id": clientRequestID,
		},
	}

	if err := appendInvocationEventLine(eventsPath, event); err != nil {
		return "", false, err
	}
	return followUpTimelineEntryID(seq), false, nil
}

func appendInvocationEventLine(eventsPath string, event invocationEventLine) error {
	dir := filepath.Dir(eventsPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func loadMaxInvocationEventSeq(eventsPath string) (uint64, error) {
	f, err := os.Open(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer func() { _ = f.Close() }()

	var maxSeq uint64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxFollowUpEventLineBytes)
	for scanner.Scan() {
		var line struct {
			Seq uint64 `json:"seq"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Seq > maxSeq {
			maxSeq = line.Seq
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return maxSeq, nil
}

func findFollowUpPromptSeq(eventsPath, clientRequestID string) (uint64, bool, error) {
	f, err := os.Open(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxFollowUpEventLineBytes)
	for scanner.Scan() {
		var line invocationEventLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Kind != followUpPromptEventKind {
			continue
		}
		if reqID, ok := line.Data["client_request_id"].(string); ok && reqID == clientRequestID {
			return line.Seq, true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, false, err
	}
	return 0, false, nil
}

func followUpTimelineEntryID(seq uint64) string {
	return "inv_event:" + strconv.FormatUint(seq, 10) + ":" + followUpPromptEventKind
}
