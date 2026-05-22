package daemon

import "encoding/json"

type rawAPIResponse struct {
	OK        bool            `json:"ok"`
	RequestID string          `json:"request_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	ErrorCode string          `json:"error_code,omitempty"`
	Message   string          `json:"message,omitempty"`
	Hint      string          `json:"hint,omitempty"`
	Details   json.RawMessage `json:"details,omitempty"`
}
