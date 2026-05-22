package daemon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/jsonl"
)

const (
	timelineSourceRankPrompt    = 10
	timelineSourceRankStream    = 20
	timelineSourceRankEvent     = 30
	timelineSourceRankRawMarker = 40
	maxTimelineLineBytes        = stream.MaxLineSize
	expectedTimelineSchema      = "1.0"
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

type timelineJSONLEnvelope struct {
	SchemaVersion string                 `json:"schema_version"`
	Seq           uint64                 `json:"seq"`
	Timestamp     string                 `json:"timestamp"`
	Kind          string                 `json:"kind"`
	Data          map[string]interface{} `json:"data"`
}

// handleGetInvocationTimeline handles GET /invocations/{ref}/timeline.
func (s *Server) handleGetInvocationTimeline(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)

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

	entries, err := s.collectTimelineEntries(record)
	if err != nil {
		s.writeInvocationTimelineReadError(w, requestID, err)
		return
	}
	if params.Order == "desc" {
		slices.Reverse(entries)
	}
	page, nextCursor := paginateTimeline(entries, params.Cursor, params.Limit)
	s.writeAPIResponse(w, requestID, InvocationTimelineData{
		Entries:    page,
		NextCursor: nextCursor,
	})
}

func (s *Server) writeInvocationTimelineReadError(w http.ResponseWriter, requestID string, err error) {
	code := errors.GetCode(err)
	if code == "" {
		code = errors.EInternal
	}
	message := "failed to read invocation timeline"
	if err != nil {
		message = err.Error()
	}
	s.writeAPIError(w, http.StatusInternalServerError, requestID, string(code), message, "", nil)
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

func (s *Server) collectTimelineEntries(record *resolvedInvocation) ([]timelineSortableEntry, error) {
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
	streamEntries, err := readStreamTimelineEntries(s.readableInvocationLogPath(record.RepoID, record.InvocationID, InvocationLogKindStream), baseTimestamp)
	if err != nil {
		return nil, err
	}
	result = append(result, streamEntries...)

	// Invocation events (checkpoint lifecycle, landing events, etc).
	invocationEventEntries, err := readInvocationEventTimelineEntries(s.store.InvocationEventsPath(record.RepoID, record.InvocationID), baseTimestamp)
	if err != nil {
		return nil, err
	}
	result = append(result, invocationEventEntries...)

	// Raw-log coverage marker.
	if info, err := os.Stat(s.readableInvocationLogPath(record.RepoID, record.InvocationID, InvocationLogKindRaw)); err == nil && info.Size() > 0 {
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
	return result, nil
}

func readStreamTimelineEntries(path, defaultTimestamp string) ([]timelineSortableEntry, error) {
	return readTimelineJSONL(path, defaultTimestamp, "stream", timelineSourceRankStream, buildStreamTimelineEntry)
}

func readInvocationEventTimelineEntries(path, defaultTimestamp string) ([]timelineSortableEntry, error) {
	return readTimelineJSONL(path, defaultTimestamp, "invocation_event", timelineSourceRankEvent, buildInvocationEventTimelineEntry)
}

func readTimelineJSONL(
	path string,
	defaultTimestamp string,
	source string,
	sourceRank int,
	build func(lineNumber int, event timelineJSONLEnvelope, defaultTimestamp string) timelineSortableEntry,
) ([]timelineSortableEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.Wrap(errors.EStoreCorrupt, fmt.Sprintf("open %s timeline file", source), err)
	}
	defer func() { _ = f.Close() }()

	entries := make([]timelineSortableEntry, 0)
	visitErr := jsonl.Visit(f, maxTimelineLineBytes, jsonl.VisitOptions{}, func(line jsonl.Line) error {
		if line.Oversized {
			entries = append(entries, newTimelineReadFailureEntry(
				source,
				sourceRank,
				line.Number,
				defaultTimestamp,
				"line_too_large",
				nil,
			))
			return nil
		}

		var event timelineJSONLEnvelope
		if err := json.Unmarshal(line.Bytes, &event); err != nil {
			entries = append(entries, newTimelineReadFailureEntry(
				source,
				sourceRank,
				line.Number,
				defaultTimestamp,
				"json_unmarshal_failed",
				err,
			))
			return nil
		}
		entry := build(line.Number, event, defaultTimestamp)
		entries = append(entries, entry)
		return nil
	})
	if visitErr != nil {
		return nil, errors.Wrap(errors.EStoreCorrupt, fmt.Sprintf("read %s timeline file", source), visitErr)
	}

	return entries, nil
}

func buildStreamTimelineEntry(lineNumber int, event timelineJSONLEnvelope, defaultTimestamp string) timelineSortableEntry {
	if event.SchemaVersion != expectedTimelineSchema {
		return newSchemaMismatchTimelineEntry(
			"stream",
			timelineSourceRankStream,
			lineNumber,
			event.Seq,
			event.Timestamp,
			defaultTimestamp,
			event.SchemaVersion,
			event.Kind,
		)
	}
	normalizedKind := strings.TrimSpace(event.Kind)
	if normalizedKind == "" {
		return newMissingEventKindTimelineEntry(
			"stream",
			timelineSourceRankStream,
			lineNumber,
			event.Seq,
			event.Timestamp,
			defaultTimestamp,
		)
	}
	kind := normalizedKind
	switch normalizedKind {
	case "message":
		kind = "message"
	case "tool_start":
		kind = "tool_use"
		if event.Data == nil {
			event.Data = map[string]interface{}{}
		}
		event.Data["in_progress"] = true
	case "tool_end":
		kind = "tool_use"
	}
	entryID := "stream:" + strconv.FormatUint(event.Seq, 10)
	return newTimelineEntry(
		TimelineEntryDTO{
			EntryID:   entryID,
			Kind:      kind,
			Source:    "stream",
			Timestamp: nonEmpty(event.Timestamp, defaultTimestamp),
			Seq:       event.Seq,
			Data:      event.Data,
		},
		timelineSourceRankStream,
	)
}

func buildInvocationEventTimelineEntry(lineNumber int, event timelineJSONLEnvelope, defaultTimestamp string) timelineSortableEntry {
	if event.SchemaVersion != expectedTimelineSchema {
		return newSchemaMismatchTimelineEntry(
			"invocation_event",
			timelineSourceRankEvent,
			lineNumber,
			event.Seq,
			event.Timestamp,
			defaultTimestamp,
			event.SchemaVersion,
			event.Kind,
		)
	}

	if event.Seq == 0 {
		return newInvalidEventSeqTimelineEntry(
			"invocation_event",
			timelineSourceRankEvent,
			lineNumber,
			event.Timestamp,
			defaultTimestamp,
			event.Kind,
		)
	}

	kind := strings.TrimSpace(event.Kind)
	if kind == "" {
		return newMissingEventKindTimelineEntry(
			"invocation_event",
			timelineSourceRankEvent,
			lineNumber,
			event.Seq,
			event.Timestamp,
			defaultTimestamp,
		)
	}
	entryKind := "invocation_event"
	if strings.HasPrefix(kind, "agency.checkpoint_") {
		entryKind = "checkpoint_event"
	} else if kind == followUpPromptEventKind {
		entryKind = "followup_prompt"
	}

	payload := event.Data
	if payload == nil {
		payload = map[string]interface{}{}
	}
	payload["event_kind"] = kind

	return newTimelineEntry(
		TimelineEntryDTO{
			EntryID:   "inv_event:" + strconv.FormatUint(event.Seq, 10) + ":" + kind,
			Kind:      entryKind,
			Source:    "invocation_event",
			Timestamp: nonEmpty(event.Timestamp, defaultTimestamp),
			Seq:       event.Seq,
			Data:      payload,
		},
		timelineSourceRankEvent,
	)
}

func newInvalidEventSeqTimelineEntry(
	source string,
	sourceRank int,
	lineNumber int,
	timestamp string,
	defaultTimestamp string,
	eventKind string,
) timelineSortableEntry {
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" {
		normalizedSource = "timeline"
	}
	if lineNumber < 1 {
		lineNumber = 1
	}

	return newTimelineEntry(
		TimelineEntryDTO{
			EntryID:   fmt.Sprintf("%s:invalid_event_seq:line:%d", normalizedSource, lineNumber),
			Kind:      "parse_error",
			Source:    normalizedSource,
			Timestamp: nonEmpty(timestamp, defaultTimestamp),
			Data: map[string]interface{}{
				"reason":     "invalid_event_seq",
				"line":       lineNumber,
				"event_kind": strings.TrimSpace(eventKind),
			},
		},
		sourceRank,
	)
}

func newTimelineReadFailureEntry(
	source string,
	sourceRank int,
	lineNumber int,
	defaultTimestamp string,
	reason string,
	readErr error,
) timelineSortableEntry {
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" {
		normalizedSource = "timeline"
	}
	if lineNumber < 1 {
		lineNumber = 1
	}
	data := map[string]interface{}{
		"reason": reason,
		"line":   lineNumber,
	}
	if readErr != nil {
		if trimmedErr := strings.TrimSpace(readErr.Error()); trimmedErr != "" {
			data["error"] = trimmedErr
		}
	}
	return newTimelineEntry(
		TimelineEntryDTO{
			EntryID:   fmt.Sprintf("%s:parse_error:line:%d", normalizedSource, lineNumber),
			Kind:      "parse_error",
			Source:    normalizedSource,
			Timestamp: defaultTimestamp,
			Data:      data,
		},
		sourceRank,
	)
}

func newSchemaMismatchTimelineEntry(
	source string,
	sourceRank int,
	lineNumber int,
	seq uint64,
	timestamp string,
	defaultTimestamp string,
	actualSchema string,
	eventKind string,
) timelineSortableEntry {
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" {
		normalizedSource = "timeline"
	}
	entryID := fmt.Sprintf("%s:schema_mismatch:line:%d", normalizedSource, lineNumber)
	if seq > 0 {
		entryID = fmt.Sprintf("%s:schema_mismatch:%d", normalizedSource, seq)
	}
	trimmedSchema := strings.TrimSpace(actualSchema)
	if trimmedSchema == "" {
		trimmedSchema = "<empty>"
	}
	return newTimelineEntry(
		TimelineEntryDTO{
			EntryID:   entryID,
			Kind:      "parse_error",
			Source:    normalizedSource,
			Timestamp: nonEmpty(timestamp, defaultTimestamp),
			Seq:       seq,
			Data: map[string]interface{}{
				"reason":                  "unsupported_schema_version",
				"expected_schema_version": expectedTimelineSchema,
				"actual_schema_version":   trimmedSchema,
				"event_kind":              strings.TrimSpace(eventKind),
			},
		},
		sourceRank,
	)
}

func newMissingEventKindTimelineEntry(
	source string,
	sourceRank int,
	lineNumber int,
	seq uint64,
	timestamp string,
	defaultTimestamp string,
) timelineSortableEntry {
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" {
		normalizedSource = "timeline"
	}
	if lineNumber < 1 {
		lineNumber = 1
	}
	entryID := fmt.Sprintf("%s:missing_event_kind:line:%d", normalizedSource, lineNumber)
	if seq > 0 {
		entryID = fmt.Sprintf("%s:missing_event_kind:%d", normalizedSource, seq)
	}

	data := map[string]interface{}{
		"reason": "missing_event_kind",
		"line":   lineNumber,
	}

	return newTimelineEntry(
		TimelineEntryDTO{
			EntryID:   entryID,
			Kind:      "parse_error",
			Source:    normalizedSource,
			Timestamp: nonEmpty(timestamp, defaultTimestamp),
			Seq:       seq,
			Data:      data,
		},
		sourceRank,
	)
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
			var c timelineCursor
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
		c := timelineCursor{
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

func nonEmpty(v, defaultValue string) string {
	if v != "" {
		return v
	}
	return defaultValue
}
