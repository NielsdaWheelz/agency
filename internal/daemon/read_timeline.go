package daemon

import (
	"cmp"
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
			entries = append(entries, newParseErrorTimelineEntry(parseErrorEntry{
				source:           source,
				sourceRank:       sourceRank,
				lineNumber:       line.Number,
				defaultTimestamp: defaultTimestamp,
				idPrefix:         "parse_error",
				reason:           "line_too_large",
			}))
			return nil
		}

		var event timelineJSONLEnvelope
		if err := json.Unmarshal(line.Bytes, &event); err != nil {
			extras := map[string]any{}
			if trimmed := strings.TrimSpace(err.Error()); trimmed != "" {
				extras["error"] = trimmed
			}
			entries = append(entries, newParseErrorTimelineEntry(parseErrorEntry{
				source:           source,
				sourceRank:       sourceRank,
				lineNumber:       line.Number,
				defaultTimestamp: defaultTimestamp,
				idPrefix:         "parse_error",
				reason:           "json_unmarshal_failed",
				extras:           extras,
			}))
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
		return newParseErrorTimelineEntry(parseErrorEntry{
			source:           "stream",
			sourceRank:       timelineSourceRankStream,
			lineNumber:       lineNumber,
			seq:              event.Seq,
			timestamp:        event.Timestamp,
			defaultTimestamp: defaultTimestamp,
			idPrefix:         "schema_mismatch",
			reason:           "unsupported_schema_version",
			extras: map[string]any{
				"expected_schema_version": expectedTimelineSchema,
				"actual_schema_version":   cmp.Or(strings.TrimSpace(event.SchemaVersion), "<empty>"),
				"event_kind":              strings.TrimSpace(event.Kind),
			},
		})
	}
	normalizedKind := strings.TrimSpace(event.Kind)
	if normalizedKind == "" {
		return newParseErrorTimelineEntry(parseErrorEntry{
			source:           "stream",
			sourceRank:       timelineSourceRankStream,
			lineNumber:       lineNumber,
			seq:              event.Seq,
			timestamp:        event.Timestamp,
			defaultTimestamp: defaultTimestamp,
			idPrefix:         "missing_event_kind",
			reason:           "missing_event_kind",
		})
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
			Timestamp: cmp.Or(event.Timestamp, defaultTimestamp),
			Seq:       event.Seq,
			Data:      event.Data,
		},
		timelineSourceRankStream,
	)
}

func buildInvocationEventTimelineEntry(lineNumber int, event timelineJSONLEnvelope, defaultTimestamp string) timelineSortableEntry {
	if event.SchemaVersion != expectedTimelineSchema {
		return newParseErrorTimelineEntry(parseErrorEntry{
			source:           "invocation_event",
			sourceRank:       timelineSourceRankEvent,
			lineNumber:       lineNumber,
			seq:              event.Seq,
			timestamp:        event.Timestamp,
			defaultTimestamp: defaultTimestamp,
			idPrefix:         "schema_mismatch",
			reason:           "unsupported_schema_version",
			extras: map[string]any{
				"expected_schema_version": expectedTimelineSchema,
				"actual_schema_version":   cmp.Or(strings.TrimSpace(event.SchemaVersion), "<empty>"),
				"event_kind":              strings.TrimSpace(event.Kind),
			},
		})
	}

	if event.Seq == 0 {
		return newParseErrorTimelineEntry(parseErrorEntry{
			source:           "invocation_event",
			sourceRank:       timelineSourceRankEvent,
			lineNumber:       lineNumber,
			timestamp:        event.Timestamp,
			defaultTimestamp: defaultTimestamp,
			idPrefix:         "invalid_event_seq",
			reason:           "invalid_event_seq",
			extras: map[string]any{
				"event_kind": strings.TrimSpace(event.Kind),
			},
		})
	}

	kind := strings.TrimSpace(event.Kind)
	if kind == "" {
		return newParseErrorTimelineEntry(parseErrorEntry{
			source:           "invocation_event",
			sourceRank:       timelineSourceRankEvent,
			lineNumber:       lineNumber,
			seq:              event.Seq,
			timestamp:        event.Timestamp,
			defaultTimestamp: defaultTimestamp,
			idPrefix:         "missing_event_kind",
			reason:           "missing_event_kind",
		})
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
			Timestamp: cmp.Or(event.Timestamp, defaultTimestamp),
			Seq:       event.Seq,
			Data:      payload,
		},
		timelineSourceRankEvent,
	)
}

type parseErrorEntry struct {
	source           string
	sourceRank       int
	lineNumber       int
	seq              uint64
	timestamp        string
	defaultTimestamp string
	idPrefix         string
	reason           string
	extras           map[string]any
}

func newParseErrorTimelineEntry(p parseErrorEntry) timelineSortableEntry {
	source := strings.TrimSpace(p.source)
	if source == "" {
		source = "timeline"
	}
	line := p.lineNumber
	if line < 1 {
		line = 1
	}
	entryID := fmt.Sprintf("%s:%s:line:%d", source, p.idPrefix, line)
	if p.seq > 0 {
		entryID = fmt.Sprintf("%s:%s:%d", source, p.idPrefix, p.seq)
	}
	data := map[string]any{"reason": p.reason, "line": line}
	for k, v := range p.extras {
		data[k] = v
	}
	return newTimelineEntry(
		TimelineEntryDTO{
			EntryID:   entryID,
			Kind:      "parse_error",
			Source:    source,
			Timestamp: cmp.Or(p.timestamp, p.defaultTimestamp),
			Seq:       p.seq,
			Data:      data,
		},
		p.sourceRank,
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
	return cmp.Or(
		cmp.Compare(a.Timestamp, b.Timestamp),
		cmp.Compare(a.SourceRank, b.SourceRank),
		cmp.Compare(a.Seq, b.Seq),
		cmp.Compare(a.EntryID, b.EntryID),
	)
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
