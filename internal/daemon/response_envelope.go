package daemon

// ResponseEnvelope is the shared header fields embedded into every typed
// mutation/control-plane response. Embedding (no JSON tag) keeps the wire
// format flat. Error fields stay zero on success and vice versa.
type ResponseEnvelope struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	Message      string `json:"message,omitempty"`
	Hint         string `json:"hint,omitempty"`
}

// NewErrorEnvelope returns the envelope used by every typed error response.
// requestID may be empty for endpoints that do not propagate it.
func NewErrorEnvelope(requestID, code, message, hint string) ResponseEnvelope {
	return ResponseEnvelope{
		OK:           false,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		RequestID:    requestID,
		ErrorCode:    code,
		Message:      message,
		Hint:         hint,
	}
}

// NewSuccessEnvelope returns the envelope for a typed success response.
func NewSuccessEnvelope(requestID string) ResponseEnvelope {
	return ResponseEnvelope{
		OK:           true,
		APIVersion:   APIVersion,
		BuildVersion: daemonBuildVersion(),
		RequestID:    requestID,
	}
}
