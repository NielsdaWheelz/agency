package stream

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedClock() time.Time {
	return time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
}

type errorAfterPayloadReader struct {
	payload []byte
	offset  int
	err     error
}

func (r *errorAfterPayloadReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.payload) {
		return 0, r.err
	}
	n := copy(p, r.payload[r.offset:])
	r.offset += n
	if r.offset >= len(r.payload) {
		return n, r.err
	}
	return n, nil
}

func TestParser_StreamAndParse_CodexCommandOutputAcrossStartAndEndPreserved(t *testing.T) {
	t.Parallel()

	parser := NewParser("test-inv-codex-output-preserved", "codex", fixedClock)

	rawFile, err := os.CreateTemp("", "raw-codex-output-preserved-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-codex-output-preserved-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	input := strings.Join([]string{
		`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"sh -lc probe","aggregated_output":"sleep-start\n","exit_code":null,"status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"sh -lc probe","aggregated_output":"sleep-end\n","exit_code":0,"status":"completed"}}`,
	}, "\n") + "\n"

	err = parser.StreamAndParse(strings.NewReader(input), rawFile, streamFile)
	require.NoError(t, err)

	rawData, err := os.ReadFile(rawFile.Name())
	require.NoError(t, err)
	assert.Contains(t, string(rawData), "sleep-start", "raw capture must keep early command output")
	assert.Contains(t, string(rawData), "sleep-end", "raw capture must keep final command output")

	streamData, err := os.ReadFile(streamFile.Name())
	require.NoError(t, err)
	assert.Contains(t, string(streamData), "sleep-start", "normalized stream must keep early command output")
	assert.Contains(t, string(streamData), "sleep-end", "normalized stream must keep final command output")
}

func TestParser_StreamAndParse_MalformedMidStream(t *testing.T) {
	t.Parallel()
	// Read fixture
	fixturePath := filepath.Join("testdata", "malformed_mid_stream.jsonl")
	fixtureData, err := os.ReadFile(fixturePath)
	require.NoError(t, err, "Failed to read fixture")

	// Create parser
	parser := NewParser("test-inv-3", "claude-code", fixedClock)

	// Create temp files
	rawFile, err := os.CreateTemp("", "raw-*.jsonl")
	require.NoError(t, err, "Failed to create temp raw file")
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-*.jsonl")
	require.NoError(t, err, "Failed to create temp stream file")
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	// Parse (should not fail even with malformed line)
	reader := bytes.NewReader(fixtureData)
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.NoError(t, err, "StreamAndParse failed")

	// Verify raw.jsonl contains verbatim data (including malformed line)
	_, _ = rawFile.Seek(0, 0)
	rawData, _ := os.ReadFile(rawFile.Name())
	assert.Equal(t, fixtureData, rawData, "raw.jsonl content doesn't match fixture")

	// Verify stream.jsonl has parse_error event
	_, _ = streamFile.Seek(0, 0)
	streamData, _ := os.ReadFile(streamFile.Name())
	assert.Contains(t, string(streamData), `"kind":"parse_error"`, "stream.jsonl should contain parse_error event")

	// Verify parsing continued after error (should still have final event)
	assert.Contains(t, string(streamData), `"kind":"final"`, "stream.jsonl should contain final event after malformed line")
}

func TestParser_StreamAndParse_NoTrailingNewline(t *testing.T) {
	t.Parallel()
	// Read fixture
	fixturePath := filepath.Join("testdata", "no_trailing_newline.jsonl")
	fixtureData, err := os.ReadFile(fixturePath)
	require.NoError(t, err, "Failed to read fixture")

	// Verify fixture doesn't end with newline
	if fixtureData[len(fixtureData)-1] == '\n' {
		t.Skip("Fixture has trailing newline, test invalid")
	}

	// Create parser
	parser := NewParser("test-inv-4", "claude-code", fixedClock)

	// Create temp files
	rawFile, err := os.CreateTemp("", "raw-*.jsonl")
	require.NoError(t, err, "Failed to create temp raw file")
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-*.jsonl")
	require.NoError(t, err, "Failed to create temp stream file")
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	// Parse
	reader := bytes.NewReader(fixtureData)
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.NoError(t, err, "StreamAndParse failed")

	// Verify raw.jsonl contains all data (including final line without newline)
	_, _ = rawFile.Seek(0, 0)
	rawData, _ := os.ReadFile(rawFile.Name())
	assert.Equal(t, fixtureData, rawData, "raw.jsonl content doesn't match fixture")

	// Verify final line was parsed (should have final event)
	_, _ = streamFile.Seek(0, 0)
	streamData, _ := os.ReadFile(streamFile.Name())
	assert.Contains(t, string(streamData), `"kind":"final"`, "stream.jsonl should contain final event from line without trailing newline")
}

func TestParser_SeqMonotonic(t *testing.T) {
	t.Parallel()
	// Create a simple fixture inline
	input := `{"type":"system","subtype":"init","cwd":"/sandbox"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}]}}
{"type":"result","subtype":"success"}
`

	parser := NewParser("test-inv-seq", "claude-code", fixedClock)

	rawFile, _ := os.CreateTemp("", "raw-*.jsonl")
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, _ := os.CreateTemp("", "stream-*.jsonl")
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	reader := bytes.NewReader([]byte(input))
	_ = parser.StreamAndParse(reader, rawFile, streamFile)

	// Read stream.jsonl and verify seq values
	_, _ = streamFile.Seek(0, 0)
	streamData, _ := os.ReadFile(streamFile.Name())
	lines := strings.Split(strings.TrimSpace(string(streamData)), "\n")

	require.GreaterOrEqual(t, len(lines), 3, "Expected at least 3 events")

	// Verify seq is monotonically increasing starting at 1
	for i, line := range lines {
		var event struct {
			Seq uint64 `json:"seq"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		assert.Equal(t, uint64(i+1), event.Seq)
	}
}

func TestParser_StreamAndParse_OversizedLineEmitsParseErrorAndContinues(t *testing.T) {
	t.Parallel()

	parser := NewParser("test-invocation", "claude-code", fixedClock)

	rawFile, err := os.CreateTemp("", "raw-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	oversizedLine := strings.Repeat("x", MaxLineSize+1) + "\n"
	validLine := `{"type":"system","subtype":"init","cwd":"/sandbox"}` + "\n"
	reader := bytes.NewReader([]byte(oversizedLine + validLine))

	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.NoError(t, err)

	streamData, err := os.ReadFile(streamFile.Name())
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(streamData)), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "expected parse_error + valid parsed event")

	foundLineTooLarge := false
	foundNonParseEvent := false
	for _, line := range lines {
		var event struct {
			Kind string                 `json:"kind"`
			Data map[string]interface{} `json:"data"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Kind == string(eventKindParseError) {
			if reason, ok := event.Data["reason"].(string); ok && reason == "line_too_large" {
				foundLineTooLarge = true
			}
			continue
		}
		foundNonParseEvent = true
	}

	assert.True(t, foundLineTooLarge, "expected parse_error reason=line_too_large")
	assert.True(t, foundNonParseEvent, "expected parser to continue and emit subsequent valid events")
}

func TestParser_StreamAndParse_OversizedLinePersistsRawBeforeTerminator(t *testing.T) {
	parser := NewParser("test-invocation-streaming", "claude-code", fixedClock)

	rawFile, err := os.CreateTemp("", "raw-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	inputReader, inputWriter := io.Pipe()
	done := make(chan error, 1)
	finished := false
	go func() {
		done <- parser.StreamAndParse(inputReader, rawFile, streamFile)
	}()
	t.Cleanup(func() {
		_ = inputWriter.Close()
		if finished {
			return
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})

	oversizedWithoutTerminator := bytes.Repeat([]byte("x"), MaxLineSize+4096)
	_, err = inputWriter.Write(oversizedWithoutTerminator)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		info, statErr := rawFile.Stat()
		if statErr != nil {
			return false
		}
		return info.Size() > 0
	}, 2*time.Second, 20*time.Millisecond, "expected raw log to receive bytes before newline terminator arrives")

	validLine := `{"type":"system","subtype":"init","cwd":"/sandbox"}`
	_, err = inputWriter.Write([]byte("\n" + validLine + "\n"))
	require.NoError(t, err)
	require.NoError(t, inputWriter.Close())

	select {
	case err = <-done:
		finished = true
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("parser did not terminate after input close")
	}

	rawInfo, err := os.Stat(rawFile.Name())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, rawInfo.Size(), int64(len(oversizedWithoutTerminator)+len(validLine)+2))

	streamData, err := os.ReadFile(streamFile.Name())
	require.NoError(t, err)
	assert.Contains(t, string(streamData), `"kind":"parse_error"`)
	assert.Contains(t, string(streamData), `"reason":"line_too_large"`)
	assert.Contains(t, string(streamData), `"kind":"session_start"`)
}

func TestParser_StreamAndParse_OversizedLineWithReaderErrorStillEmitsParseError(t *testing.T) {
	parser := NewParser("test-invocation-read-error", "claude-code", fixedClock)

	rawFile, err := os.CreateTemp("", "raw-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	oversizedPayload := bytes.Repeat([]byte("x"), MaxLineSize+1024)
	reader := &errorAfterPayloadReader{
		payload: oversizedPayload,
		err:     io.ErrUnexpectedEOF,
	}

	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.Error(t, err)
	assert.True(t, stderrors.Is(err, io.ErrUnexpectedEOF), "expected reader error to surface")

	rawData, err := os.ReadFile(rawFile.Name())
	require.NoError(t, err)
	assert.Equal(t, oversizedPayload, rawData, "raw log should preserve oversized bytes even on reader error")

	streamData, err := os.ReadFile(streamFile.Name())
	require.NoError(t, err)
	assert.Contains(t, string(streamData), `"kind":"parse_error"`)
	assert.Contains(t, string(streamData), `"reason":"line_too_large"`)
}

func TestParser_StreamAndParse_RawWriteFailureReturnsError(t *testing.T) {
	t.Parallel()

	parser := NewParser("test-inv-raw-write-fail", "claude-code", fixedClock)

	rawFile, err := os.CreateTemp("", "raw-ro-*.jsonl")
	require.NoError(t, err)
	rawPath := rawFile.Name()
	require.NoError(t, rawFile.Close())
	rawFile, err = os.Open(rawPath) // read-only descriptor
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawPath) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-ok-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	reader := bytes.NewReader([]byte(`{"type":"system","subtype":"init","cwd":"/sandbox"}` + "\n"))
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.Error(t, err)
	assert.True(t, stderrors.Is(err, ErrRawLogWriteFailed), "expected raw-log write failure classification")
}

func TestParser_CheckpointNotify_MutatingTool(t *testing.T) {
	t.Parallel()

	var notifications []CheckpointNotification
	notifyFn := func(n CheckpointNotification) {
		notifications = append(notifications, n)
	}

	parser := NewParser("test-inv-notify", "claude-code", fixedClock)
	parser.SetCheckpointNotify(notifyFn)

	rawFile, err := os.CreateTemp("", "raw-notify-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-notify-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	// Simulate a Claude assistant message with tool_use blocks for Edit
	assistantMsg := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"editing file"},{"type":"tool_use","id":"t1","name":"Edit","input":{}}]}}` + "\n"
	// Followed by a user message (tool result)
	userMsg := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}` + "\n"

	reader := strings.NewReader(assistantMsg + userMsg)
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.NoError(t, err)

	// Should have received at least one notification for the Edit tool
	require.GreaterOrEqual(t, len(notifications), 1, "expected at least one checkpoint notification")
	found := false
	for _, n := range notifications {
		if n.ToolName == "Edit" {
			found = true
			assert.Greater(t, n.Seq, uint64(0), "seq should be > 0")
		}
	}
	assert.True(t, found, "expected notification for Edit tool")
}

func TestParser_CheckpointNotify_NonMutatingTool_NoNotification(t *testing.T) {
	t.Parallel()

	var notifications []CheckpointNotification
	notifyFn := func(n CheckpointNotification) {
		notifications = append(notifications, n)
	}

	parser := NewParser("test-inv-no-notify", "claude-code", fixedClock)
	parser.SetCheckpointNotify(notifyFn)

	rawFile, err := os.CreateTemp("", "raw-nonotify-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-nonotify-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	// Simulate a Claude assistant message with tool_use for Read (non-mutating)
	assistantMsg := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{}}]}}` + "\n"
	userMsg := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"file data"}]}}` + "\n"

	reader := strings.NewReader(assistantMsg + userMsg)
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.NoError(t, err)

	// No notifications for non-mutating tools
	assert.Empty(t, notifications, "non-mutating tool should not trigger checkpoint notification")
}

func TestParser_CheckpointNotify_NilNotifyFn(t *testing.T) {
	t.Parallel()

	// Parser without checkpoint notify should not panic
	parser := NewParser("test-inv-nil-notify", "claude-code", fixedClock)
	// No SetCheckpointNotify call

	rawFile, err := os.CreateTemp("", "raw-nilnotify-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-nilnotify-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	assistantMsg := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Edit","input":{}}]}}` + "\n"
	reader := strings.NewReader(assistantMsg)
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.NoError(t, err)
	// No panic = success
}

func TestParser_CheckpointNotify_CodexMutatingCommand_TriggersNotification(t *testing.T) {
	t.Parallel()

	var notifications []CheckpointNotification
	parser := NewParser("test-inv-codex-checkpoint", "codex", fixedClock)
	parser.SetCheckpointNotify(func(n CheckpointNotification) {
		notifications = append(notifications, n)
	})

	rawFile, err := os.CreateTemp("", "raw-codex-checkpoint-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-codex-checkpoint-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	input := strings.Join([]string{
		`{"type":"item.started","item":{"type":"command_execution","command":"printf 'hello' > test-codex.txt"}}`,
		`{"type":"item.completed","item":{"type":"command_execution","command":"printf 'hello' > test-codex.txt","exit_code":0}}`,
		`{"type":"item.completed","item":{"type":"agent_message","content":[{"type":"text","text":"done"}]}}`,
	}, "\n") + "\n"

	err = parser.StreamAndParse(strings.NewReader(input), rawFile, streamFile)
	require.NoError(t, err)

	require.NotEmpty(t, notifications, "mutating codex command should emit checkpoint notification")
	assert.Equal(t, "Bash", notifications[0].ToolName)
	assert.Greater(t, notifications[0].Seq, uint64(0))
}

func TestParser_CheckpointNotify_CodexFileChange_TriggersNotification(t *testing.T) {
	t.Parallel()

	var notifications []CheckpointNotification
	parser := NewParser("test-inv-codex-filechange-checkpoint", "codex", fixedClock)
	parser.SetCheckpointNotify(func(n CheckpointNotification) {
		notifications = append(notifications, n)
	})

	rawFile, err := os.CreateTemp("", "raw-codex-filechange-checkpoint-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-codex-filechange-checkpoint-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	input := strings.Join([]string{
		`{"type":"item.completed","item":{"type":"file_change","changes":[{"path":"README.md","kind":"update"}]}}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
	}, "\n") + "\n"

	err = parser.StreamAndParse(strings.NewReader(input), rawFile, streamFile)
	require.NoError(t, err)

	require.NotEmpty(t, notifications, "codex file_change should emit checkpoint notification")
	assert.Equal(t, "FileChange", notifications[0].ToolName)
	assert.Greater(t, notifications[0].Seq, uint64(0))
}

func TestParser_CheckpointNotify_CursorMutatingToolCall_TriggersNotification(t *testing.T) {
	t.Parallel()

	var notifications []CheckpointNotification
	parser := NewParser("test-inv-cursor-checkpoint", "cursor", fixedClock)
	parser.SetCheckpointNotify(func(n CheckpointNotification) {
		notifications = append(notifications, n)
	})

	rawFile, err := os.CreateTemp("", "raw-cursor-checkpoint-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-cursor-checkpoint-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	input := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"writing file"}]}}`,
		`{"type":"tool_call","subtype":"completed","tool_call":{"bashToolCall":{"command":"printf 'hello' > test-cursor.txt","exitCode":0}}}`,
		`{"type":"result","subtype":"success"}`,
	}, "\n") + "\n"

	err = parser.StreamAndParse(strings.NewReader(input), rawFile, streamFile)
	require.NoError(t, err)

	require.NotEmpty(t, notifications, "mutating cursor tool call should emit checkpoint notification")
	assert.Equal(t, "Bash", notifications[0].ToolName)
	assert.Greater(t, notifications[0].Seq, uint64(0))
}

func TestParser_SessionStartNotify_ExtractsResumeSessionID(t *testing.T) {
	t.Parallel()

	var notifications []SessionStartNotification
	parser := NewParser("test-inv-session-notify", "codex", fixedClock)
	parser.SetSessionStartNotify(func(n SessionStartNotification) {
		notifications = append(notifications, n)
	})

	rawFile, err := os.CreateTemp("", "raw-sessionnotify-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-sessionnotify-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	// Codex emits thread.started with thread_id. Parser should map this to
	// a session notification that daemon resume logic can consume.
	reader := strings.NewReader(`{"type":"thread.started","thread_id":"thread_123"}` + "\n")
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.NoError(t, err)

	require.Len(t, notifications, 1)
	assert.Equal(t, "thread_123", notifications[0].SessionID)
	assert.Greater(t, notifications[0].Seq, uint64(0))
}

func TestParser_SessionStartNotify_ExtractsCursorSessionID(t *testing.T) {
	t.Parallel()

	var notifications []SessionStartNotification
	parser := NewParser("test-inv-session-notify-cursor", "cursor", fixedClock)
	parser.SetSessionStartNotify(func(n SessionStartNotification) {
		notifications = append(notifications, n)
	})

	rawFile, err := os.CreateTemp("", "raw-sessionnotify-cursor-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-sessionnotify-cursor-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	reader := strings.NewReader(`{"type":"system","subtype":"init","cwd":"/sandbox","session_id":"sess_123"}` + "\n")
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.NoError(t, err)

	require.Len(t, notifications, 1)
	assert.Equal(t, "sess_123", notifications[0].SessionID)
	assert.Greater(t, notifications[0].Seq, uint64(0))
}

func TestParser_StreamAndParse_StreamWriteFailureReturnsError(t *testing.T) {
	t.Parallel()

	parser := NewParser("test-inv-stream-write-fail", "claude-code", fixedClock)

	rawFile, err := os.CreateTemp("", "raw-ok-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-ro-*.jsonl")
	require.NoError(t, err)
	streamPath := streamFile.Name()
	require.NoError(t, streamFile.Close())
	streamFile, err = os.Open(streamPath) // read-only descriptor
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamPath) }()
	defer func() { _ = streamFile.Close() }()

	reader := bytes.NewReader([]byte(`{"type":"system","subtype":"init","cwd":"/sandbox"}` + "\n"))
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.Error(t, err)
	assert.True(t, stderrors.Is(err, ErrNormalizedStreamWriteFailed), "expected normalized-stream write failure classification")
}

func TestParser_StreamAndParse_WritesParseErrorForOversizedNormalizedEvent(t *testing.T) {
	t.Parallel()

	parser := NewParser("test-inv-oversized-normalized", "claude-code", fixedClock)

	rawFile, err := os.CreateTemp("", "raw-oversized-normalized-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-oversized-normalized-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	payload, err := json.Marshal(map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []map[string]string{
				{
					"type": "text",
					"text": strings.Repeat("x", MaxLineSize/2+8192),
				},
			},
		},
	})
	require.NoError(t, err)
	require.Less(t, len(payload), MaxLineSize, "raw input must stay parseable")

	reader := bytes.NewReader(append(payload, '\n'))
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	require.NoError(t, err)

	streamData, err := os.ReadFile(streamFile.Name())
	require.NoError(t, err)
	trimmed := strings.TrimSpace(string(streamData))
	require.NotEmpty(t, trimmed)

	lines := strings.Split(trimmed, "\n")
	require.Len(t, lines, 1)
	assert.LessOrEqual(t, len(lines[0]), MaxLineSize, "overflow parse_error event must remain under MaxLineSize")

	var event struct {
		Kind string                 `json:"kind"`
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &event))
	assert.Equal(t, string(eventKindParseError), event.Kind)
	assert.Equal(t, "normalized_event_too_large", dataStringValue(event.Data, "reason"))
	assert.Equal(t, string(eventKindMessage), dataStringValue(event.Data, "event_kind"))
}

func TestParser_StreamAndParse_CodexSuccess_CanonicalFamilies(t *testing.T) {
	t.Parallel()

	events := parseRunnerContractFixtureEvents(t, "codex", "codex_success.jsonl")
	require.NotEmpty(t, events)

	sawAssistantMessageText := false
	sawCommandExecution := false
	sawFileChange := false
	sawUsage := false

	for _, ev := range events {
		switch ev.Kind {
		case string(eventKindMessage):
			if dataStringValue(ev.Data, "role") != "assistant" {
				continue
			}
			if strings.TrimSpace(dataStringValue(ev.Data, "text")) != "" {
				sawAssistantMessageText = true
			}
		case string(eventKindToolStart), string(eventKindToolEnd):
			switch dataStringValue(ev.Data, "action_family") {
			case "command_execution":
				sawCommandExecution = true
				assert.NotEmpty(t, strings.TrimSpace(dataStringValue(ev.Data, "command")))
			case "file_change":
				sawFileChange = true
			}
		case string(eventKindUsage):
			sawUsage = true
		}
	}

	assert.True(t, sawAssistantMessageText, "codex agent_message.text should map to message text")
	assert.True(t, sawCommandExecution, "codex command_execution should map to command_execution family")
	assert.True(t, sawFileChange, "codex file_change should map to file_change family")
	assert.True(t, sawUsage, "codex turn.completed should map to usage")
}

func TestParser_StreamAndParse_CursorSuccess_ParsesPromptAndNestedToolResults(t *testing.T) {
	t.Parallel()

	events := parseRunnerContractFixtureEvents(t, "cursor", "cursor_success.jsonl")
	require.NotEmpty(t, events)

	sawPromptMessage := false
	sawFileRead := false
	sawFileChange := false
	sawCommandExecution := false

	for _, ev := range events {
		switch ev.Kind {
		case string(eventKindMessage):
			if dataStringValue(ev.Data, "role") == "user" &&
				dataStringValue(ev.Data, "message_family") == "prompt" &&
				strings.TrimSpace(dataStringValue(ev.Data, "text")) != "" {
				sawPromptMessage = true
			}
		case string(eventKindToolEnd):
			switch dataStringValue(ev.Data, "action_family") {
			case "file_read":
				sawFileRead = true
			case "file_change":
				sawFileChange = true
			case "command_execution":
				sawCommandExecution = true
				exitCode, ok := dataFloatValue(ev.Data, "exit_code")
				require.True(t, ok, "cursor shell completion should include exit code")
				assert.Equal(t, float64(0), exitCode)
			}
		}
	}

	assert.True(t, sawPromptMessage, "cursor echoed user prompt should remain prompt, not tool_result")
	assert.True(t, sawFileRead, "cursor read tool should map to file_read family")
	assert.True(t, sawFileChange, "cursor edit tool should map to file_change family")
	assert.True(t, sawCommandExecution, "cursor shell tool should map to command_execution family")
}

func TestParser_StreamAndParse_RunnerContractCoverage_Parseable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		runner  string
		fixture string
	}{
		{name: "claude_assistant_only", runner: "claude-code", fixture: "claude_assistant_only.jsonl"},
		{name: "claude_read_search_no_edit", runner: "claude-code", fixture: "claude_read_search_no_edit.jsonl"},
		{name: "claude_command_long_output", runner: "claude-code", fixture: "claude_command_long_output.jsonl"},
		{name: "claude_single_edit", runner: "claude-code", fixture: "claude_single_edit.jsonl"},
		{name: "codex_assistant_only", runner: "codex", fixture: "codex_assistant_only.jsonl"},
		{name: "codex_read_search_no_edit", runner: "codex", fixture: "codex_read_search_no_edit.jsonl"},
		{name: "codex_command_long_output", runner: "codex", fixture: "codex_command_long_output.jsonl"},
		{name: "codex_single_edit", runner: "codex", fixture: "codex_single_edit.jsonl"},
		{name: "cursor_assistant_only", runner: "cursor", fixture: "cursor_assistant_only.jsonl"},
		{name: "cursor_read_search_no_edit", runner: "cursor", fixture: "cursor_read_search_no_edit.jsonl"},
		{name: "cursor_command_long_output", runner: "cursor", fixture: "cursor_command_long_output.jsonl"},
		{name: "cursor_single_edit", runner: "cursor", fixture: "cursor_single_edit.jsonl"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			events := parseRunnerContractFixtureEvents(t, tc.runner, tc.fixture)
			require.NotEmpty(t, events, "fixture should parse into normalized events")

			sawTextMessage := false
			sawTerminal := false
			for _, ev := range events {
				if ev.Kind == string(eventKindMessage) && strings.TrimSpace(dataStringValue(ev.Data, "text")) != "" {
					sawTextMessage = true
				}
				if ev.Kind == string(eventKindFinal) || ev.Kind == string(eventKindUsage) {
					sawTerminal = true
				}
			}

			assert.True(t, sawTextMessage, "fixture should include at least one text-bearing message")
			assert.True(t, sawTerminal, "fixture should include final/usage terminal evidence")
		})
	}
}

func TestParser_StreamAndParse_CursorToolFamilyCoverage_IncludesSearchAndWeb(t *testing.T) {
	t.Parallel()

	events := parseRunnerContractFixtureEvents(t, "cursor", "cursor_tool_family_coverage.jsonl")
	require.NotEmpty(t, events)

	sawGlob := false
	sawGrep := false
	sawWebSearch := false
	sawWebFetch := false

	for _, ev := range events {
		if ev.Kind != string(eventKindToolEnd) {
			continue
		}
		name := dataStringValue(ev.Data, "name")
		family := dataStringValue(ev.Data, "action_family")
		switch {
		case name == "Glob" && family == actionFamilySearch:
			sawGlob = true
		case name == "Grep" && family == actionFamilySearch:
			sawGrep = true
		case name == "WebSearch" && family == actionFamilyWebAction:
			sawWebSearch = true
		case name == "WebFetch" && family == actionFamilyWebAction:
			sawWebFetch = true
		}
	}

	assert.True(t, sawGlob, "cursor glob tool calls must normalize to search family")
	assert.True(t, sawGrep, "cursor grep tool calls must normalize to search family")
	assert.True(t, sawWebSearch, "cursor webSearch tool calls must normalize to web_action family")
	assert.True(t, sawWebFetch, "cursor webFetch tool calls must normalize to web_action family")
}

func TestParser_StreamAndParse_CursorFailure_ExtractsFailureExitCode(t *testing.T) {
	t.Parallel()

	events := parseRunnerContractFixtureEvents(t, "cursor", "cursor_failure.jsonl")
	require.NotEmpty(t, events)

	sawFailedCommand := false
	for _, ev := range events {
		if ev.Kind != string(eventKindToolEnd) {
			continue
		}
		if dataStringValue(ev.Data, "action_family") != "command_execution" {
			continue
		}
		exitCode, ok := dataFloatValue(ev.Data, "exit_code")
		require.True(t, ok, "cursor failed shell result should expose exit_code")
		if int(exitCode) == 7 {
			sawFailedCommand = true
		}
	}

	assert.True(t, sawFailedCommand, "cursor nested failure payload should preserve non-zero exit code")
}

func TestParser_StreamAndParse_CodexFailure_PreservesFailedCommandExitCode(t *testing.T) {
	t.Parallel()

	events := parseRunnerContractFixtureEvents(t, "codex", "codex_failure.jsonl")
	require.NotEmpty(t, events)

	sawFailedCommand := false
	for _, ev := range events {
		if ev.Kind != string(eventKindToolEnd) {
			continue
		}
		if dataStringValue(ev.Data, "action_family") != "command_execution" {
			continue
		}
		exitCode, ok := dataFloatValue(ev.Data, "exit_code")
		require.True(t, ok, "codex failed command result should expose exit_code")
		if int(exitCode) == 7 {
			sawFailedCommand = true
		}
	}

	assert.True(t, sawFailedCommand, "codex failure fixture should preserve non-zero command exit code")
}

func TestParser_StreamAndParse_ClaudeFailure_PreservesToolFailureContext(t *testing.T) {
	t.Parallel()

	events := parseRunnerContractFixtureEvents(t, "claude-code", "claude_failure.jsonl")
	require.NotEmpty(t, events)

	sawToolResultMessage := false
	sawToolResultExitCode := false
	sawUnknownDiagnostic := false

	for _, ev := range events {
		switch ev.Kind {
		case string(eventKindMessage):
			if dataStringValue(ev.Data, "role") == "user" &&
				dataStringValue(ev.Data, "message_family") == "tool_result" &&
				strings.Contains(strings.ToLower(dataStringValue(ev.Data, "text")), "fixture failure") {
				sawToolResultMessage = true
			}
			if dataStringValue(ev.Data, "role") == "user" &&
				dataStringValue(ev.Data, "message_family") == "tool_result" &&
				strings.Contains(strings.ToLower(dataStringValue(ev.Data, "text")), "exit code 7") {
				sawToolResultExitCode = true
			}
		case string(eventKindUnknown):
			if dataStringValue(ev.Data, "runner_event_type") == "rate_limit_event" {
				sawUnknownDiagnostic = true
			}
		}
	}

	assert.True(t, sawToolResultMessage, "claude tool_result message should preserve failure text")
	assert.True(t, sawToolResultExitCode, "claude tool_result message should preserve failure exit code text")
	assert.True(t, sawUnknownDiagnostic, "unknown runner event shape should emit explicit diagnostic")
}

func TestParser_StreamAndParse_ClaudeSuccess_UnknownEventDiagnosticEmitted(t *testing.T) {
	t.Parallel()

	events := parseRunnerContractFixtureEvents(t, "claude-code", "claude_success.jsonl")
	require.NotEmpty(t, events)

	sawUnknownDiagnostic := false
	sawFinal := false
	sawContentBlocks := false

	for _, ev := range events {
		switch ev.Kind {
		case "unknown":
			if dataStringValue(ev.Data, "runner_event_type") == "rate_limit_event" {
				sawUnknownDiagnostic = true
			}
		case string(eventKindMessage):
			if _, ok := ev.Data["content_blocks"]; ok {
				sawContentBlocks = true
			}
		case string(eventKindFinal):
			sawFinal = true
		}
	}

	assert.True(t, sawUnknownDiagnostic, "unknown runner event shape should emit explicit diagnostic event")
	assert.True(t, sawContentBlocks, "claude content_blocks should survive StreamAndParse serialization")
	assert.True(t, sawFinal, "parsing should continue after unknown runner event")
}

func TestParser_StreamAndParse_ClaudeResultErrorsEmitError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{
			name:  "result error",
			input: `{"type":"result","subtype":"error","error":"rate limit exceeded","error_code":"E_RATE_LIMIT"}`,
		},
		{
			name:  "result canceled",
			input: `{"type":"result","subtype":"canceled"}`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			events := parseRunnerInputEvents(t, "claude-code", tc.input+"\n")
			require.Len(t, events, 1)
			assert.Equal(t, string(eventKindError), events[0].Kind)
		})
	}
}

func TestParser_StreamAndParse_ClaudeContentBlocksAndToolResultFallback(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me check"},{"type":"tool_use","id":"t1","name":"Read","input":{"path":"/foo"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"file contents here"}]}}`,
		`{"type":"user","tool_use_result":"command output"}`,
	}, "\n") + "\n"
	events := parseRunnerInputEvents(t, "claude-code", input)

	require.Len(t, events, 3)
	assistant := events[0]
	assert.Equal(t, string(eventKindMessage), assistant.Kind)
	assert.Equal(t, "assistant", dataStringValue(assistant.Data, "role"))
	assert.Equal(t, true, assistant.Data["has_tool_use"])
	assert.Equal(t, "Let me check", dataStringValue(assistant.Data, "text"))
	assert.Equal(t, []interface{}{"Read"}, assistant.Data["tool_names"])

	assistantBlocks := dataMapSlice(assistant.Data, "content_blocks")
	require.Len(t, assistantBlocks, 2)
	assert.Equal(t, "text", dataStringValue(assistantBlocks[0], "type"))
	assert.Equal(t, "Let me check", dataStringValue(assistantBlocks[0], "text"))
	assert.Equal(t, "tool_use", dataStringValue(assistantBlocks[1], "type"))
	assert.Equal(t, "Read", dataStringValue(assistantBlocks[1], "name"))
	assert.Equal(t, "t1", dataStringValue(assistantBlocks[1], "id"))
	assert.NotNil(t, assistantBlocks[1]["input"], "tool_use block should preserve input")

	userToolResult := events[1]
	assert.Equal(t, "user", dataStringValue(userToolResult.Data, "role"))
	assert.Equal(t, "file contents here", dataStringValue(userToolResult.Data, "text"))
	userBlocks := dataMapSlice(userToolResult.Data, "content_blocks")
	require.Len(t, userBlocks, 1)
	assert.Equal(t, "tool_result", dataStringValue(userBlocks[0], "type"))
	assert.Equal(t, "t1", dataStringValue(userBlocks[0], "tool_use_id"))
	assert.Equal(t, "file contents here", dataStringValue(userBlocks[0], "content"))

	fallback := events[2]
	assert.Equal(t, string(eventKindMessage), fallback.Kind)
	assert.Equal(t, "user", dataStringValue(fallback.Data, "role"))
	assert.Equal(t, "tool_result", dataStringValue(fallback.Data, "message_family"))
	assert.Equal(t, "command output", dataStringValue(fallback.Data, "text"))
}

func TestParser_StreamAndParse_CodexContentBlocksAndUpdatedToolID(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`{"type":"item.completed","item":{"type":"agent_message","content":[{"type":"text","text":"Done!"}]}}`,
		`{"type":"item.updated","item":{"id":"item_1","type":"command_execution","command":"sh -lc probe","aggregated_output":"sleep-start\n","status":"in_progress"}}`,
	}, "\n") + "\n"
	events := parseRunnerInputEvents(t, "codex", input)

	require.Len(t, events, 2)
	message := events[0]
	assert.Equal(t, string(eventKindMessage), message.Kind)
	assert.Equal(t, "assistant", dataStringValue(message.Data, "role"))
	assert.Equal(t, "Done!", dataStringValue(message.Data, "text"))
	messageBlocks := dataMapSlice(message.Data, "content_blocks")
	require.Len(t, messageBlocks, 1)
	assert.Equal(t, "text", dataStringValue(messageBlocks[0], "type"))
	assert.Equal(t, "Done!", dataStringValue(messageBlocks[0], "text"))

	toolStart := events[1]
	assert.Equal(t, string(eventKindToolStart), toolStart.Kind)
	assert.Equal(t, "item_1", dataStringValue(toolStart.Data, "tool_id"))
	assert.Equal(t, "sh -lc probe", dataStringValue(toolStart.Data, "command"))
}

func TestParser_StreamAndParse_CursorCompletedToolMetadataUsesStablePriority(t *testing.T) {
	t.Parallel()

	input := `{"type":"tool_call","subtype":"completed","tool_call":{"bashToolCall":{"command":"echo hi","exitCode":0},"editToolCall":{"target_file":"main.go","exitCode":0}}}` + "\n"
	events := parseRunnerInputEvents(t, "cursor", input)

	require.Len(t, events, 1)
	ev := events[0]
	assert.Equal(t, string(eventKindToolEnd), ev.Kind)
	assert.Equal(t, "Bash", dataStringValue(ev.Data, "name"))
	assert.Equal(t, "echo hi", dataStringValue(ev.Data, "command"))
	exitCode, ok := dataFloatValue(ev.Data, "exit_code")
	require.True(t, ok)
	assert.Equal(t, float64(0), exitCode)
	assert.Equal(t, actionFamilyCommandExecution, dataStringValue(ev.Data, "action_family"))
}

func TestParser_StreamAndParse_UnknownDiagnosticsForMalformedRunnerEvents(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		runner          string
		input           string
		wantReason      string
		wantEventType   string
		wantPreviewText string
	}{
		{
			name:            "codex item started missing item",
			runner:          "codex",
			input:           `{"type":"item.started","item":null}`,
			wantReason:      "missing_item",
			wantEventType:   "item.started",
			wantPreviewText: `"item":null`,
		},
		{
			name:            "codex item completed missing item",
			runner:          "codex",
			input:           `{"type":"item.completed","item":null}`,
			wantReason:      "missing_item",
			wantEventType:   "item.completed",
			wantPreviewText: `"item":null`,
		},
		{
			name:            "cursor unrecognized tool call",
			runner:          "cursor",
			input:           `{"type":"tool_call","subtype":"completed","tool_call":{"strangeToolCall":{"foo":"bar"}}}`,
			wantReason:      "unrecognized_tool_structure",
			wantEventType:   "tool_call",
			wantPreviewText: `"strangeToolCall"`,
		},
		{
			name:          "cursor empty tool call",
			runner:        "cursor",
			input:         `{"type":"tool_call","subtype":"completed","tool_call":{}}`,
			wantReason:    "unrecognized_tool_structure",
			wantEventType: "tool_call",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			events := parseRunnerInputEvents(t, tc.runner, tc.input+"\n")
			require.Len(t, events, 1)
			ev := events[0]
			assert.Equal(t, string(eventKindUnknown), ev.Kind)
			assert.Equal(t, tc.wantReason, dataStringValue(ev.Data, "reason"))
			assert.Equal(t, tc.wantEventType, dataStringValue(ev.Data, "runner_event_type"))
			if tc.wantPreviewText != "" {
				assert.Contains(t, dataStringValue(ev.Data, "raw_json_preview"), tc.wantPreviewText)
			}
		})
	}
}

type parsedStreamEvent struct {
	Kind string                 `json:"kind"`
	Data map[string]interface{} `json:"data"`
}

func parseRunnerInputEvents(t *testing.T, runner, input string) []parsedStreamEvent {
	t.Helper()

	parser := NewParser("inline-"+runner, runner, fixedClock)

	rawFile, err := os.CreateTemp("", "raw-runner-inline-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-runner-inline-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	err = parser.StreamAndParse(strings.NewReader(input), rawFile, streamFile)
	require.NoError(t, err)

	rawData, err := os.ReadFile(rawFile.Name())
	require.NoError(t, err)
	assert.Equal(t, []byte(input), rawData, "raw.jsonl content should match parser input")

	streamData, err := os.ReadFile(streamFile.Name())
	require.NoError(t, err)
	return parseStreamEvents(t, streamData)
}

func parseRunnerContractFixtureEvents(t *testing.T, runner, fixtureName string) []parsedStreamEvent {
	t.Helper()

	fixturePath := filepath.Join("testdata", "runner_contract_fixtures", fixtureName)
	fixtureData, err := os.ReadFile(fixturePath)
	require.NoError(t, err, "failed reading fixture: %s", fixturePath)

	parser := NewParser("fixture-"+runner, runner, fixedClock)

	rawFile, err := os.CreateTemp("", "raw-runner-contract-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-runner-contract-*.jsonl")
	require.NoError(t, err)
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	err = parser.StreamAndParse(bytes.NewReader(fixtureData), rawFile, streamFile)
	require.NoError(t, err)

	rawData, err := os.ReadFile(rawFile.Name())
	require.NoError(t, err)
	assert.Equal(t, fixtureData, rawData, "raw.jsonl content should match fixture")

	streamData, err := os.ReadFile(streamFile.Name())
	require.NoError(t, err)
	return parseStreamEvents(t, streamData)
}

func parseStreamEvents(t *testing.T, streamData []byte) []parsedStreamEvent {
	t.Helper()

	trimmed := strings.TrimSpace(string(streamData))
	if trimmed == "" {
		return nil
	}

	lines := strings.Split(trimmed, "\n")
	events := make([]parsedStreamEvent, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event parsedStreamEvent
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		events = append(events, event)
	}
	return events
}

func dataStringValue(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	v, ok := data[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func dataMapSlice(data map[string]interface{}, key string) []map[string]interface{} {
	if data == nil {
		return nil
	}
	v, ok := data[key]
	if !ok {
		return nil
	}
	switch values := v.(type) {
	case []map[string]interface{}:
		return slices.Clone(values)
	case []interface{}:
		result := make([]map[string]interface{}, 0, len(values))
		for _, value := range values {
			m, ok := value.(map[string]interface{})
			if !ok {
				return nil
			}
			result = append(result, m)
		}
		return result
	default:
		return nil
	}
}

func dataFloatValue(data map[string]interface{}, key string) (float64, bool) {
	if data == nil {
		return 0, false
	}
	v, ok := data[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
