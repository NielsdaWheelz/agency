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
func AppendEvent(path string, e Event) (err error) {
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
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

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

// MergeConfirmPromptedData returns the data map for a merge_confirm_prompted event.
func MergeConfirmPromptedData() map[string]any {
	return map[string]any{}
}

// MergeConfirmedData returns the data map for a merge_confirmed event.
func MergeConfirmedData() map[string]any {
	return map[string]any{}
}

// GHMergeStartedData returns the data map for a gh_merge_started event.
func GHMergeStartedData(prNumber int, prURL, strategy string) map[string]any {
	return map[string]any{
		"pr_number": prNumber,
		"pr_url":    prURL,
		"strategy":  strategy,
	}
}

// GHMergeFinishedData returns the data map for a gh_merge_finished event.
func GHMergeFinishedData(ok bool, prNumber int, prURL string) map[string]any {
	return map[string]any{
		"ok":        ok,
		"pr_number": prNumber,
		"pr_url":    prURL,
	}
}

// MergeAlreadyMergedData returns the data map for a merge_already_merged event.
func MergeAlreadyMergedData(prNumber int, prURL string) map[string]any {
	return map[string]any{
		"pr_number": prNumber,
		"pr_url":    prURL,
	}
}

// MergeFinishedData returns the data map for a merge_finished event.
func MergeFinishedData(ok bool, errorCode string) map[string]any {
	data := map[string]any{
		"ok": ok,
	}
	if errorCode != "" {
		data["error_code"] = errorCode
	}
	return data
}

// VerifyStartedData returns the data map for a verify_started event.
func VerifyStartedData(timeoutMS int64, logPath string, verifyJSONPath string) map[string]any {
	data := map[string]any{
		"timeout_ms": timeoutMS,
		"log_path":   logPath,
	}
	if verifyJSONPath != "" {
		data["verify_json_path"] = verifyJSONPath
	}
	return data
}

// VerifyFinishedData returns the data map for a verify_finished event.
func VerifyFinishedData(ok bool, exitCode *int, timedOut, cancelled bool, durationMS int64, verifyJSONPath, logPath, verifyRecordPath string) map[string]any {
	data := map[string]any{
		"ok":                 ok,
		"timed_out":          timedOut,
		"cancelled":          cancelled,
		"duration_ms":        durationMS,
		"log_path":           logPath,
		"verify_record_path": verifyRecordPath,
	}
	if exitCode != nil {
		data["exit_code"] = *exitCode
	}
	if verifyJSONPath != "" {
		data["verify_json_path"] = verifyJSONPath
	}
	return data
}

// CleanStartedData returns the data map for a clean_started event.
func CleanStartedData(runID string) map[string]any {
	return map[string]any{
		"run_id": runID,
	}
}

// CleanFinishedData returns the data map for a clean_finished event.
func CleanFinishedData(ok bool) map[string]any {
	return map[string]any{
		"ok": ok,
	}
}

// ArchiveStartedData returns the data map for an archive_started event.
func ArchiveStartedData(runID string) map[string]any {
	return map[string]any{
		"run_id": runID,
	}
}

// ArchiveFinishedData returns the data map for an archive_finished event.
func ArchiveFinishedData(ok bool) map[string]any {
	return map[string]any{
		"ok": ok,
	}
}

// ArchiveFailedData returns the data map for an archive_failed event.
// Includes details about which sub-steps succeeded or failed.
// Reason strings are bounded to 512 bytes max.
func ArchiveFailedData(scriptOK, tmuxOK, deleteOK bool, scriptReason, tmuxReason, deleteReason string) map[string]any {
	const maxReasonLen = 512

	truncate := func(s string) string {
		if len(s) > maxReasonLen {
			return s[:maxReasonLen]
		}
		return s
	}

	data := map[string]any{
		"script_ok": scriptOK,
		"tmux_ok":   tmuxOK,
		"delete_ok": deleteOK,
	}

	if scriptReason != "" {
		data["script_reason"] = truncate(scriptReason)
	}
	if tmuxReason != "" {
		data["tmux_reason"] = truncate(tmuxReason)
	}
	if deleteReason != "" {
		data["delete_reason"] = truncate(deleteReason)
	}

	return data
}
