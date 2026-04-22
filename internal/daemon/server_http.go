package daemon

import (
	"encoding/json"
	"net/http"
)

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := prepareRequestID(w, r)
		next.ServeHTTP(w, r.WithContext(withRequestIDContext(r.Context(), requestID)))
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func setRequestIDHeader(w http.ResponseWriter, requestID string) {
	normalized := normalizeRequestID(requestID)
	if normalized == "" {
		return
	}
	w.Header().Set("X-Request-ID", normalized)
}

func requestIDForResponse(w http.ResponseWriter) string {
	if w == nil {
		return newRequestID()
	}
	if requestID := normalizeRequestID(w.Header().Get("X-Request-ID")); requestID != "" {
		return requestID
	}
	requestID := newRequestID()
	setRequestIDHeader(w, requestID)
	return requestID
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message, hint string) {
	s.writeErrorWithRequestID(w, status, requestIDForResponse(w), code, message, hint)
}

func (s *Server) writeErrorWithRequestID(w http.ResponseWriter, status int, requestID, code, message, hint string) {
	requestID = resolveOrGenerateRequestID(requestID)
	setRequestIDHeader(w, requestID)
	resp := ErrorResponse{
		OK:        false,
		RequestID: requestID,
		ErrorCode: code,
		Message:   message,
		Hint:      hint,
	}
	s.writeJSON(w, status, resp)
}
