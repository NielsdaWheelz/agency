package daemon

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

// handleGetInvocationLogs handles GET /invocations/{ref}/logs.
func (s *Server) handleGetInvocationLogs(w http.ResponseWriter, r *http.Request, invocationRef string) {
	requestID := getOrCreateRequestID(r)

	if r.Method != http.MethodGet {
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	record, resolveErr := s.resolveInvocationRef(invocationRef, r.URL.Query().Get("repo_id"))
	if resolveErr != nil {
		s.writeReadResolveError(w, requestID, resolveErr, "use 'agent ls' to list invocations", errors.EInvocationIDAmbiguous)
		return
	}

	params, invalid := parseGetLogsParams(r)
	if invalid != nil {
		s.writeAPIError(
			w,
			http.StatusBadRequest,
			requestID,
			string(errors.EInvalidArgument),
			fmt.Sprintf("invalid value for parameter '%s': %q", invalid.Param, invalid.Value),
			"",
			*invalid,
		)
		return
	}

	var logPath string
	switch params.Kind {
	case "stderr":
		logPath = s.readableInvocationLogPath(record.RepoID, record.InvocationID, "stderr")
	case "stream":
		logPath = s.readableInvocationLogPath(record.RepoID, record.InvocationID, "stream")
	case "hooks":
		logPath = s.readableInvocationLogPath(record.RepoID, record.InvocationID, "hooks")
	case "terminal":
		logPath = s.readableInvocationLogPath(record.RepoID, record.InvocationID, "terminal")
	default:
		logPath = s.readableInvocationLogPath(record.RepoID, record.InvocationID, "raw")
		params.Kind = "raw"
	}

	offsetData, err := s.readLogFileAtOffset(logPath, params.Offset, params.Limit)
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", err.Error(), "", nil)
		return
	}
	offsetData.Kind = params.Kind
	s.writeAPIResponse(w, requestID, offsetData)
}

// readLogFileAtOffset reads a log file at a byte offset, returning up to limit bytes.
func (s *Server) readLogFileAtOffset(logPath string, offset int64, limit int) (*InvocationLogsOffsetData, error) {
	info, err := os.Stat(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &InvocationLogsOffsetData{}, nil
		}
		return nil, err
	}

	totalBytes := info.Size()
	if limit <= 0 {
		limit = 65536
	}
	if limit > MaxLogChunk {
		limit = MaxLogChunk
	}

	if offset >= totalBytes {
		return &InvocationLogsOffsetData{
			DataB64:    "",
			NextOffset: totalBytes,
			TotalBytes: totalBytes,
		}, nil
	}

	f, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
	}

	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	buf = buf[:n]

	return &InvocationLogsOffsetData{
		DataB64:    base64.StdEncoding.EncodeToString(buf),
		NextOffset: offset + int64(n),
		TotalBytes: totalBytes,
	}, nil
}
