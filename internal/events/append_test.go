package events

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendEvent(t *testing.T) {
	t.Parallel()
	t.Run("creates file lazily", func(t *testing.T) {
		t.Parallel()
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
		require.NoError(t, err, "AppendEvent()")

		// Verify file exists
		_, err = os.Stat(path)
		require.False(t, os.IsNotExist(err), "expected events.jsonl to be created")

		// Verify content
		content, err := os.ReadFile(path)
		require.NoError(t, err, "failed to read file")

		// Should be a single line ending with newline
		assert.True(t, strings.HasSuffix(string(content), "\n"), "expected line to end with newline")

		// Parse and verify JSON
		var parsed Event
		err = json.Unmarshal(content, &parsed)
		require.NoError(t, err, "failed to parse JSON")

		assert.Equal(t, "1.0", parsed.SchemaVersion)
		assert.Equal(t, "cmd_start", parsed.Event)
	})

	t.Run("appends multiple events", func(t *testing.T) {
		t.Parallel()
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
		require.NoError(t, err, "AppendEvent(event1)")

		err = AppendEvent(path, event2)
		require.NoError(t, err, "AppendEvent(event2)")

		// Verify content
		content, err := os.ReadFile(path)
		require.NoError(t, err, "failed to read file")

		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		require.Len(t, lines, 2)

		// Parse both lines
		var e1, e2 Event
		err = json.Unmarshal([]byte(lines[0]), &e1)
		require.NoError(t, err, "failed to parse line 1")
		err = json.Unmarshal([]byte(lines[1]), &e2)
		require.NoError(t, err, "failed to parse line 2")

		assert.Equal(t, "cmd_start", e1.Event)
		assert.Equal(t, "cmd_end", e2.Event)
	})

	t.Run("creates parent directories", func(t *testing.T) {
		t.Parallel()
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
		require.NoError(t, err, "AppendEvent()")

		// Verify file exists
		_, err = os.Stat(path)
		require.False(t, os.IsNotExist(err), "expected events.jsonl to be created with parent dirs")
	})
}

func TestCmdStartData(t *testing.T) {
	t.Parallel()
	data := CmdStartData("show", []string{"abc123", "--capture"})

	assert.Equal(t, "show", data["cmd"])

	args, ok := data["args"].([]string)
	require.True(t, ok, "args is not []string: %T", data["args"])
	assert.Equal(t, []string{"abc123", "--capture"}, args)
}

func TestCmdEndData(t *testing.T) {
	t.Parallel()
	t.Run("without error code", func(t *testing.T) {
		t.Parallel()
		data := CmdEndData("show", 0, 123, nil)

		assert.Equal(t, "show", data["cmd"])
		assert.Equal(t, 0, data["exit_code"])
		assert.Equal(t, int64(123), data["duration_ms"])
		_, ok := data["error_code"]
		assert.False(t, ok, "error_code should not be present")
	})

	t.Run("with error code", func(t *testing.T) {
		t.Parallel()
		errCode := "E_RUN_NOT_FOUND"
		data := CmdEndData("show", 1, 50, &errCode)

		assert.Equal(t, "E_RUN_NOT_FOUND", data["error_code"])
	})
}

func TestCaptureResultData(t *testing.T) {
	t.Parallel()
	t.Run("capture ok", func(t *testing.T) {
		t.Parallel()
		data := CaptureResultData(true, "", "")

		assert.Equal(t, true, data["capture_ok"])
		_, ok := data["capture_stage"]
		assert.False(t, ok, "capture_stage should not be present on success")
	})

	t.Run("capture failed", func(t *testing.T) {
		t.Parallel()
		data := CaptureResultData(false, "has_session", "tmux session does not exist")

		assert.Equal(t, false, data["capture_ok"])
		assert.Equal(t, "has_session", data["capture_stage"])
		assert.Equal(t, "tmux session does not exist", data["capture_error"])
	})
}

func TestEventJSON(t *testing.T) {
	t.Parallel()
	event := Event{
		SchemaVersion: "1.0",
		Timestamp:     "2026-01-10T12:00:00Z",
		RepoID:        "abc123",
		RunID:         "20260110-a3f2",
		Event:         "cmd_start",
		Data:          map[string]any{"cmd": "show", "args": []any{"abc", "--capture"}},
	}

	data, err := json.Marshal(event)
	require.NoError(t, err, "failed to marshal")

	// Verify compact JSON (no indentation)
	assert.False(t, strings.Contains(string(data), "\n"), "JSON should be compact (no newlines)")

	// Verify required fields present
	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err, "failed to unmarshal")

	requiredFields := []string{"schema_version", "timestamp", "repo_id", "run_id", "event"}
	for _, field := range requiredFields {
		_, ok := parsed[field]
		assert.True(t, ok, "missing required field: %s", field)
	}
}
