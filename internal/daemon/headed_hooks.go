package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/stream"
	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const headedTranscriptStateSchema = "agency.headed_transcripts.v1"

type headedTranscriptState struct {
	SchemaVersion string           `json:"schema_version"`
	Offsets       map[string]int64 `json:"offsets"`
}

type HeadedHookIngestData struct {
	InvocationID    string   `json:"invocation_id"`
	HookBytes       int      `json:"hook_bytes"`
	TranscriptPaths []string `json:"transcript_paths,omitempty"`
	ImportedBytes   int64    `json:"imported_bytes"`
}

func (s *Server) handleHeadedHook(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)

	record, resolveErr := s.resolveInvocationRef(invocationRef, r.URL.Query().Get("repo_id"))
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agent ls' to list invocations", errors.EInvocationIDAmbiguous)
		return
	}

	meta, err := s.store.ReadInvocationMeta(record.RepoID, record.InvocationID)
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to read invocation meta: "+err.Error(), "", nil)
		return
	}
	if meta.Mode != store.RunnerModeHeaded {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "headed hook ingestion requires a headed invocation", "", nil)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, stream.MaxLineSize))
	if err != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "failed to read hook payload: "+err.Error(), "", nil)
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "invalid hook payload JSON: "+err.Error(), "", nil)
		return
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.EInvalidRequest), "invalid hook payload JSON: "+err.Error(), "", nil)
		return
	}

	s.headedHookMu.Lock()
	defer s.headedHookMu.Unlock()

	hooksPath, err := s.prepareWritableInvocationLogPath(record.RepoID, record.InvocationID, InvocationLogKindHooks)
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), err.Error(), "", nil)
		return
	}
	hooksFile, err := os.OpenFile(hooksPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to append headed hook log: "+err.Error(), "", nil)
		return
	}
	if _, err := hooksFile.Write(compact.Bytes()); err != nil {
		_ = hooksFile.Close()
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to append headed hook log: "+err.Error(), "", nil)
		return
	}
	if _, err := hooksFile.Write([]byte("\n")); err != nil {
		_ = hooksFile.Close()
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to append headed hook log: "+err.Error(), "", nil)
		return
	}
	if err := hooksFile.Close(); err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, string(errors.EInternal), "failed to append headed hook log: "+err.Error(), "", nil)
		return
	}

	runner := strings.TrimSpace(r.URL.Query().Get("runner"))
	if runner == "" {
		runner = meta.Runner
	}

	transcriptPaths := transcriptPathsFromHookPayload(payload)
	var imported int64
	for _, transcriptPath := range transcriptPaths {
		n, err := s.importHeadedTranscript(record.RepoID, record.InvocationID, runner, transcriptPath)
		if err != nil {
			s.recordInvocationWarning(record.RepoID, record.InvocationID, "headed_transcript_import_failed", err.Error(), map[string]any{
				"transcript_path": transcriptPath,
			})
			continue
		}
		imported += n
	}
	if value, ok := payload["hook_event_name"].(string); ok && strings.TrimSpace(value) == "Stop" {
		n, err := s.importHeadedSyntheticStop(record.RepoID, record.InvocationID, runner, payload)
		if err != nil {
			s.recordInvocationWarning(record.RepoID, record.InvocationID, "headed_stop_import_failed", err.Error(), nil)
		}
		imported += n
	}

	s.writeAPIResponse(w, requestID, HeadedHookIngestData{
		InvocationID:    record.InvocationID,
		HookBytes:       compact.Len(),
		TranscriptPaths: transcriptPaths,
		ImportedBytes:   imported,
	})
}

func transcriptPathsFromHookPayload(payload map[string]any) []string {
	seen := map[string]bool{}
	var paths []string
	var walk func(any)
	walk = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "transcript_path" {
					if path, ok := child.(string); ok {
						path = strings.TrimSpace(path)
						if path != "" && !seen[path] {
							seen[path] = true
							paths = append(paths, path)
						}
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(payload)
	return paths
}

func hookString(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func (s *Server) importHeadedTranscript(repoID, invocationID, runner, transcriptPath string) (int64, error) {
	if transcriptPath == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			transcriptPath = home
		}
	}
	if transcriptTail, ok := strings.CutPrefix(transcriptPath, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			transcriptPath = filepath.Join(home, transcriptTail)
		}
	}
	if !filepath.IsAbs(transcriptPath) {
		return 0, fmt.Errorf("transcript path must be absolute: %s", transcriptPath)
	}

	state, err := s.readHeadedTranscriptState(repoID, invocationID)
	if err != nil {
		return 0, err
	}

	src, err := os.Open(transcriptPath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = src.Close() }()

	info, err := src.Stat()
	if err != nil {
		return 0, err
	}
	offset := state.Offsets[transcriptPath]
	if offset > info.Size() {
		offset = 0
	}
	if _, err := src.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}

	imported, err := s.parseHeadedReader(repoID, invocationID, runner, src)
	if err != nil {
		return 0, err
	}

	newOffset, err := src.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	state.Offsets[transcriptPath] = newOffset
	if err := s.writeHeadedTranscriptState(repoID, invocationID, state); err != nil {
		return 0, err
	}
	return imported, nil
}

func (s *Server) importHeadedSyntheticStop(repoID, invocationID, runner string, payload map[string]any) (int64, error) {
	sessionID := hookString(payload, "session_id")
	switch runner {
	case "claude-code":
		raw := map[string]any{
			"type":    "result",
			"subtype": "success",
		}
		if sessionID != "" {
			raw["session_id"] = sessionID
		}
		line, _ := json.Marshal(raw)
		return s.parseHeadedReader(repoID, invocationID, runner, bytes.NewReader(append(line, '\n')))
	case "codex":
		raw := map[string]any{
			"type": "item.completed",
			"item": map[string]any{
				"type": "agent_message",
				"text": hookString(payload, "last_assistant_message"),
			},
		}
		line, _ := json.Marshal(raw)
		return s.parseHeadedReader(repoID, invocationID, runner, bytes.NewReader(append(line, '\n')))
	default:
		return 0, nil
	}
}

func (s *Server) parseHeadedReader(repoID, invocationID, runner string, reader io.Reader) (int64, error) {
	rawPath, err := s.prepareWritableInvocationLogPath(repoID, invocationID, InvocationLogKindRaw)
	if err != nil {
		return 0, err
	}
	streamPath, err := s.prepareWritableInvocationLogPath(repoID, invocationID, InvocationLogKindStream)
	if err != nil {
		return 0, err
	}

	rawFile, err := os.OpenFile(rawPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rawFile.Close() }()
	streamFile, err := os.OpenFile(streamPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() { _ = streamFile.Close() }()

	s.mu.RLock()
	proc := s.processes[invocationID]
	s.mu.RUnlock()
	var parser *stream.Parser
	if proc != nil && proc.parser != nil {
		parser = proc.parser
	} else {
		parser = stream.NewParser(invocationID, runner, s.clock)
		parser.SetInitialSeq(loadMaxStreamSeq(streamPath))
	}
	if proc != nil {
		proc.parserMu.Lock()
		defer proc.parserMu.Unlock()
	}

	startInfo, _ := rawFile.Stat()
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	endInfo, _ := rawFile.Stat()
	if proc != nil {
		s.flushLastOutputAt(proc)
	}
	if startInfo == nil || endInfo == nil {
		return 0, err
	}
	return endInfo.Size() - startInfo.Size(), err
}

func (s *Server) readHeadedTranscriptState(repoID, invocationID string) (headedTranscriptState, error) {
	state := headedTranscriptState{
		SchemaVersion: headedTranscriptStateSchema,
		Offsets:       map[string]int64{},
	}
	data, err := os.ReadFile(s.headedTranscriptStatePath(repoID, invocationID))
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if state.SchemaVersion != headedTranscriptStateSchema {
		return state, fmt.Errorf("unsupported headed transcript state schema: %s", state.SchemaVersion)
	}
	if state.Offsets == nil {
		state.Offsets = map[string]int64{}
	}
	return state, nil
}

func (s *Server) writeHeadedTranscriptState(repoID, invocationID string, state headedTranscriptState) error {
	if _, err := s.store.EnsureInvocationLogsDir(repoID, invocationID); err != nil {
		return err
	}
	state.SchemaVersion = headedTranscriptStateSchema
	if state.Offsets == nil {
		state.Offsets = map[string]int64{}
	}
	return agencyfs.WriteJSONAtomic(s.fsys, s.headedTranscriptStatePath(repoID, invocationID), state, 0o600)
}

func (s *Server) headedTranscriptStatePath(repoID, invocationID string) string {
	return filepath.Join(s.store.InvocationLogsDir(repoID, invocationID), "headed_transcripts.json")
}
