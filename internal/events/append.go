// Package events provides per-run event logging for agency.
// Events are stored in append-only JSONL files.
package events

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Event represents a single event in events.jsonl.
// This is the public contract for the events file format.
type Event struct {
	SchemaVersion string         `json:"schema_version"`
	Timestamp     string         `json:"timestamp"` // RFC3339
	RepoID        string         `json:"repo_id"`
	RunID         string         `json:"run_id"`
	Event         string         `json:"event"` // "cmd_start", "cmd_end", "script_start", "script_end"
	Data          map[string]any `json:"data,omitempty"`
}

// AppendEvent appends a single event to the events.jsonl file.
// The file is created lazily if it doesn't exist.
// Each event is written as a single JSON line followed by newline.
//
// Best-effort: errors are returned but callers should typically ignore them
// and continue with the main operation.
func AppendEvent(path string, e Event) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Open file for appending, create if not exists
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Marshal event to compact JSON (no indentation)
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}

	// Write JSON line with newline
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// CmdStartData returns the data map for a cmd_start event.
func CmdStartData(cmd string, args []string) map[string]any {
	return map[string]any{
		"cmd":  cmd,
		"args": args,
	}
}

// CmdEndData returns the data map for a cmd_end event.
// errorCode should be nil or an E_* string.
func CmdEndData(cmd string, exitCode int, durationMs int64, errorCode *string) map[string]any {
	data := map[string]any{
		"cmd":         cmd,
		"exit_code":   exitCode,
		"duration_ms": durationMs,
	}
	if errorCode != nil {
		data["error_code"] = *errorCode
	}
	return data
}

// CaptureResultData returns extra data fields for capture results.
// Used to augment cmd_end data when --capture is used.
func CaptureResultData(captureOk bool, captureStage string, captureError string) map[string]any {
	data := map[string]any{
		"capture_ok": captureOk,
	}
	if !captureOk {
		data["capture_stage"] = captureStage
		if captureError != "" {
			data["capture_error"] = captureError
		}
	}
	return data
}

// StopData returns the data map for a stop event.
func StopData(sessionName string, keys []string) map[string]any {
	return map[string]any{
		"session_name": sessionName,
		"keys":         keys,
	}
}

// KillSessionData returns the data map for a kill_session event.
func KillSessionData(sessionName string) map[string]any {
	return map[string]any{
		"session_name": sessionName,
	}
}

// ResumeData returns the data map for a resume_* event (resume_attach, resume_create, resume_restart).
func ResumeData(sessionName, runner string, detached, restart bool) map[string]any {
	return map[string]any{
		"session_name": sessionName,
		"runner":       runner,
		"detached":     detached,
		"restart":      restart,
	}
}

// ResumeFailedData returns the data map for a resume_failed event.
func ResumeFailedData(sessionName, reason string) map[string]any {
	return map[string]any{
		"session_name": sessionName,
		"reason":       reason,
	}
}
