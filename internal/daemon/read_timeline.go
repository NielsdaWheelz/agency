package daemon

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

const (
	timelineSourceRankPrompt    = 10
	timelineSourceRankStream    = 20
	timelineSourceRankEvent     = 30
	timelineSourceRankRawMarker = 40
	maxTimelineLineBytes        = 4 * 1024 * 1024
)

type timelineSortKey struct {
	Timestamp  string
	SourceRank int
	Seq        uint64
	EntryID    string
}

type timelineSortableEntry struct {
	dto TimelineEntryDTO
	key timelineSortKey
}

// handleGetInvocationTimeline handles GET /invocations/{ref}/timeline.
func (s *Server) handleGetInvocationTimeline(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)
	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	repoID := r.URL.Query().Get("repo_id")
	params, invalidLimit, invalidOrder := parseGetTimelineParams(r)
	if invalidLimit != "" {
		s.writeAPIError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidArgument),
			fmt.Sprintf("invalid value for parameter 'limit': %q", invalidLimit),
			"provide limit in [1, 500]",
			nil,
		)
		return
	}
	if invalidOrder != "" {
		s.writeAPIError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidArgument),
			fmt.Sprintf("invalid value for parameter 'order': %q", invalidOrder),
			"use 'asc' or 'desc'",
			nil,
		)
		return
	}
	if params.Order == "desc" && params.Cursor != "" {
		s.writeAPIError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidArgument),
			"cursor pagination is not supported with order=desc",
			"omit cursor when using order=desc",
			nil,
		)
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationRef, repoID)
	if resolveErr != nil {
		code := errors.GetCode(resolveErr)
		s.writeAPIError(w, http.StatusNotFound, requestID, string(code), resolveErr.Error(), "use 'agent ls' to list invocations", nil)
		return
	}

	entries := s.collectTimelineEntries(record)
	if params.Order == "desc" {
		reverseTimelineEntries(entries)
	}
	page, nextCursor := paginateTimeline(entries, params.Cursor, params.Limit)
	s.writeAPIResponse(w, requestID, InvocationTimelineData{
		Entries:    page,
		NextCursor: nextCursor,
	})
}

func parseGetTimelineParams(r *http.Request) (GetTimelineParams, string, string) {
	params := GetTimelineParams{
		Limit: 100,
		Order: "asc",
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		parsed, err := strconv.Atoi(limit)
		if err != nil || parsed < 1 || parsed > 500 {
			return params, limit, ""
		}
		params.Limit = parsed
	}
	if order := r.URL.Query().Get("order"); order != "" {
		if order != "asc" && order != "desc" {
			return params, "", order
		}
		params.Order = order
	}
	params.Cursor = r.URL.Query().Get("cursor")
	return params, "", ""
}

func (s *Server) collectTimelineEntries(record *resolvedInvocation) []timelineSortableEntry {
	result := make([]timelineSortableEntry, 0)
	baseTimestamp := timelineBaseTimestamp(record)

	// Prompt seed context entry (if prompt content is available).
	if promptPath := record.Meta.PromptPath; promptPath != "" {
		if prompt, err := os.ReadFile(promptPath); err == nil && len(strings.TrimSpace(string(prompt))) > 0 {
			result = append(result, newTimelineEntry(
				TimelineEntryDTO{
					EntryID:   "prompt_seed",
					Kind:      "prompt_seed",
					Source:    "prompt",
					Timestamp: baseTimestamp,
					Data: map[string]interface{}{
						"text": string(prompt),
					},
				},
				timelineSourceRankPrompt,
			))
		}
	}

	// Stream entries (message/tool activity and other normalized events).
	result = append(result, readStreamTimelineEntries(s.Store.SandboxStreamLogPath(record.RepoID, record.InvocationID), baseTimestamp)...)

	// Invocation events (checkpoint lifecycle, landing events, etc).
	result = append(result, readInvocationEventTimelineEntries(s.Store.InvocationEventsPath(record.RepoID, record.InvocationID), baseTimestamp)...)

	// Raw-log coverage marker.
	if info, err := os.Stat(s.Store.SandboxRawLogPath(record.RepoID, record.InvocationID)); err == nil && info.Size() > 0 {
		result = append(result, newTimelineEntry(
			TimelineEntryDTO{
				EntryID:   "raw:" + strconv.FormatInt(info.Size(), 10),
				Kind:      "raw_log_coverage",
				Source:    "raw_log",
				Timestamp: baseTimestamp,
				Data: map[string]interface{}{
					"bytes": info.Size(),
				},
			},
			timelineSourceRankRawMarker,
		))
	}

	sort.SliceStable(result, func(i, j int) bool {
		return compareTimelineKeys(result[i].key, result[j].key) < 0
	})
	return result
}

func readStreamTimelineEntries(path, fallbackTimestamp string) []timelineSortableEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	entries := make([]timelineSortableEntry, 0)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxTimelineLineBytes)
	for scanner.Scan() {
		var event struct {
			Seq       uint64                 `json:"seq"`
			Timestamp string                 `json:"timestamp"`
			Kind      string                 `json:"kind"`
			Data      map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Kind == "" {
			continue
		}
		kind := event.Kind
		switch event.Kind {
		case "message":
			kind = "message"
		case "tool_start", "tool_end":
			kind = "tool_use"
		}
		entryID := "stream:" + strconv.FormatUint(event.Seq, 10)
		entries = append(entries, newTimelineEntry(
			TimelineEntryDTO{
				EntryID:   entryID,
				Kind:      kind,
				Source:    "stream",
				Timestamp: nonEmpty(event.Timestamp, fallbackTimestamp),
				Seq:       event.Seq,
				Data:      event.Data,
			},
			timelineSourceRankStream,
		))
	}

	return entries
}

func readInvocationEventTimelineEntries(path, fallbackTimestamp string) []timelineSortableEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	entries := make([]timelineSortableEntry, 0)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxTimelineLineBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		var event struct {
			Seq       uint64                 `json:"seq"`
			Timestamp string                 `json:"timestamp"`
			Kind      string                 `json:"kind"`
			Event     string                 `json:"event"`
			Data      map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		kind := nonEmpty(event.Kind, event.Event)
		if kind == "" {
			continue
		}
		entryKind := "invocation_event"
		if strings.HasPrefix(kind, "agency.checkpoint_") {
			entryKind = "checkpoint_event"
		} else if kind == followUpPromptEventKind {
			entryKind = "followup_prompt"
		}

		entryID := "inv_event:" + strconv.Itoa(lineNumber) + ":" + kind
		if event.Seq > 0 {
			entryID = "inv_event:" + strconv.FormatUint(event.Seq, 10) + ":" + kind
		}

		payload := event.Data
		if payload == nil {
			payload = map[string]interface{}{}
		}
		payload["event_kind"] = kind

		entries = append(entries, newTimelineEntry(
			TimelineEntryDTO{
				EntryID:   entryID,
				Kind:      entryKind,
				Source:    "invocation_event",
				Timestamp: nonEmpty(event.Timestamp, fallbackTimestamp),
				Seq:       event.Seq,
				Data:      payload,
			},
			timelineSourceRankEvent,
		))
	}

	return entries
}

func timelineBaseTimestamp(record *resolvedInvocation) string {
	if record.Meta == nil {
		return ""
	}
	if record.Meta.StartedAt != "" {
		return record.Meta.StartedAt
	}
	if record.Meta.LastOutputAt != "" {
		return record.Meta.LastOutputAt
	}
	return record.Meta.FinishedAt
}

func newTimelineEntry(dto TimelineEntryDTO, sourceRank int) timelineSortableEntry {
	return timelineSortableEntry{
		dto: dto,
		key: timelineSortKey{
			Timestamp:  dto.Timestamp,
			SourceRank: sourceRank,
			Seq:        dto.Seq,
			EntryID:    dto.EntryID,
		},
	}
}

func compareTimelineKeys(a, b timelineSortKey) int {
	if a.Timestamp < b.Timestamp {
		return -1
	}
	if a.Timestamp > b.Timestamp {
		return 1
	}
	if a.SourceRank < b.SourceRank {
		return -1
	}
	if a.SourceRank > b.SourceRank {
		return 1
	}
	if a.Seq < b.Seq {
		return -1
	}
	if a.Seq > b.Seq {
		return 1
	}
	if a.EntryID < b.EntryID {
		return -1
	}
	if a.EntryID > b.EntryID {
		return 1
	}
	return 0
}

func paginateTimeline(all []timelineSortableEntry, cursor string, limit int) ([]TimelineEntryDTO, string) {
	if len(all) == 0 {
		return []TimelineEntryDTO{}, ""
	}
	startIdx := 0
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil {
			var c TimelineCursor
			if err := json.Unmarshal(decoded, &c); err == nil {
				cursorKey := timelineSortKey(c)
				startIdx = len(all)
				for i := range all {
					if compareTimelineKeys(all[i].key, cursorKey) > 0 {
						startIdx = i
						break
					}
				}
			}
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(all) {
		endIdx = len(all)
	}
	window := all[startIdx:endIdx]
	entries := make([]TimelineEntryDTO, 0, len(window))
	for _, entry := range window {
		entries = append(entries, entry.dto)
	}

	nextCursor := ""
	if endIdx < len(all) && len(window) > 0 {
		last := window[len(window)-1]
		c := TimelineCursor{
			Timestamp:  last.key.Timestamp,
			SourceRank: last.key.SourceRank,
			Seq:        last.key.Seq,
			EntryID:    last.key.EntryID,
		}
		if data, err := json.Marshal(c); err == nil {
			nextCursor = base64.StdEncoding.EncodeToString(data)
		}
	}

	return entries, nextCursor
}

func reverseTimelineEntries(entries []timelineSortableEntry) {
	slices.Reverse(entries)
}

func nonEmpty(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
