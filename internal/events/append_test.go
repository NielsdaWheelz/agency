package events

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendEvent(t *testing.T) {
	t.Run("creates file lazily", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl")

		event := Event{
			SchemaVersion: "1.0",
			Timestamp:     "2026-01-10T12:00:00Z",
			RepoID:        "abc123",
			RunID:         "20260110-a3f2",
			Event:         "cmd_start",
			Data:          map[string]any{"cmd": "show", "args": []any{"--capture"}},
		}

		err := AppendEvent(path, event)
		if err != nil {
			t.Fatalf("AppendEvent() error = %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Fatal("expected events.jsonl to be created")
		}

		// Verify content
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}

		// Should be a single line ending with newline
		if !strings.HasSuffix(string(content), "\n") {
			t.Error("expected line to end with newline")
		}

		// Parse and verify JSON
		var parsed Event
		if err := json.Unmarshal(content, &parsed); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		if parsed.SchemaVersion != "1.0" {
			t.Errorf("SchemaVersion = %q, want %q", parsed.SchemaVersion, "1.0")
		}
		if parsed.Event != "cmd_start" {
			t.Errorf("Event = %q, want %q", parsed.Event, "cmd_start")
		}
	})

	t.Run("appends multiple events", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl")

		event1 := Event{
			SchemaVersion: "1.0",
			Timestamp:     "2026-01-10T12:00:00Z",
			RepoID:        "abc123",
			RunID:         "20260110-a3f2",
			Event:         "cmd_start",
			Data:          map[string]any{"cmd": "show"},
		}

		event2 := Event{
			SchemaVersion: "1.0",
			Timestamp:     "2026-01-10T12:00:01Z",
			RepoID:        "abc123",
			RunID:         "20260110-a3f2",
			Event:         "cmd_end",
			Data:          map[string]any{"cmd": "show", "exit_code": 0, "duration_ms": 123},
		}

		err := AppendEvent(path, event1)
		if err != nil {
			t.Fatalf("AppendEvent(event1) error = %v", err)
		}

		err = AppendEvent(path, event2)
		if err != nil {
			t.Fatalf("AppendEvent(event2) error = %v", err)
		}

		// Verify content
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}

		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines, got %d", len(lines))
		}

		// Parse both lines
		var e1, e2 Event
		if err := json.Unmarshal([]byte(lines[0]), &e1); err != nil {
			t.Fatalf("failed to parse line 1: %v", err)
		}
		if err := json.Unmarshal([]byte(lines[1]), &e2); err != nil {
			t.Fatalf("failed to parse line 2: %v", err)
		}

		if e1.Event != "cmd_start" {
			t.Errorf("event1.Event = %q, want %q", e1.Event, "cmd_start")
		}
		if e2.Event != "cmd_end" {
			t.Errorf("event2.Event = %q, want %q", e2.Event, "cmd_end")
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nested", "dir", "events.jsonl")

		event := Event{
			SchemaVersion: "1.0",
			Timestamp:     "2026-01-10T12:00:00Z",
			RepoID:        "abc123",
			RunID:         "20260110-a3f2",
			Event:         "cmd_start",
		}

		err := AppendEvent(path, event)
		if err != nil {
			t.Fatalf("AppendEvent() error = %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Fatal("expected events.jsonl to be created with parent dirs")
		}
	})
}

func TestCmdStartData(t *testing.T) {
	data := CmdStartData("show", []string{"abc123", "--capture"})

	if data["cmd"] != "show" {
		t.Errorf("cmd = %v, want %q", data["cmd"], "show")
	}

	args, ok := data["args"].([]string)
	if !ok {
		t.Fatalf("args is not []string: %T", data["args"])
	}
	if len(args) != 2 || args[0] != "abc123" || args[1] != "--capture" {
		t.Errorf("args = %v, want [abc123, --capture]", args)
	}
}

func TestCmdEndData(t *testing.T) {
	t.Run("without error code", func(t *testing.T) {
		data := CmdEndData("show", 0, 123, nil)

		if data["cmd"] != "show" {
			t.Errorf("cmd = %v, want %q", data["cmd"], "show")
		}
		if data["exit_code"] != 0 {
			t.Errorf("exit_code = %v, want %d", data["exit_code"], 0)
		}
		if data["duration_ms"] != int64(123) {
			t.Errorf("duration_ms = %v, want %d", data["duration_ms"], 123)
		}
		if _, ok := data["error_code"]; ok {
			t.Error("error_code should not be present")
		}
	})

	t.Run("with error code", func(t *testing.T) {
		errCode := "E_RUN_NOT_FOUND"
		data := CmdEndData("show", 1, 50, &errCode)

		if data["error_code"] != "E_RUN_NOT_FOUND" {
			t.Errorf("error_code = %v, want %q", data["error_code"], "E_RUN_NOT_FOUND")
		}
	})
}

func TestCaptureResultData(t *testing.T) {
	t.Run("capture ok", func(t *testing.T) {
		data := CaptureResultData(true, "", "")

		if data["capture_ok"] != true {
			t.Errorf("capture_ok = %v, want %v", data["capture_ok"], true)
		}
		if _, ok := data["capture_stage"]; ok {
			t.Error("capture_stage should not be present on success")
		}
	})

	t.Run("capture failed", func(t *testing.T) {
		data := CaptureResultData(false, "has_session", "tmux session does not exist")

		if data["capture_ok"] != false {
			t.Errorf("capture_ok = %v, want %v", data["capture_ok"], false)
		}
		if data["capture_stage"] != "has_session" {
			t.Errorf("capture_stage = %v, want %q", data["capture_stage"], "has_session")
		}
		if data["capture_error"] != "tmux session does not exist" {
			t.Errorf("capture_error = %v, want %q", data["capture_error"], "tmux session does not exist")
		}
	})
}

func TestEventJSON(t *testing.T) {
	event := Event{
		SchemaVersion: "1.0",
		Timestamp:     "2026-01-10T12:00:00Z",
		RepoID:        "abc123",
		RunID:         "20260110-a3f2",
		Event:         "cmd_start",
		Data:          map[string]any{"cmd": "show", "args": []any{"abc", "--capture"}},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify compact JSON (no indentation)
	if strings.Contains(string(data), "\n") {
		t.Error("JSON should be compact (no newlines)")
	}

	// Verify required fields present
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	requiredFields := []string{"schema_version", "timestamp", "repo_id", "run_id", "event"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}
}
