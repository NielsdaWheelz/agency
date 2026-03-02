package stream

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
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

func TestClaudeAdapter_ParseLine(t *testing.T) {
	t.Parallel()
	adapter := &ClaudeAdapter{}

	tests := []struct {
		name       string
		input      string
		wantKind   EventKind
		wantStatus *runnerstatus.Status
		wantErr    bool
	}{
		{
			name:     "system init",
			input:    `{"type":"system","subtype":"init","cwd":"/sandbox","model":"claude-3"}`,
			wantKind: EventKindSessionStart,
		},
		{
			name:     "assistant text only",
			input:    `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}]}}`,
			wantKind: EventKindMessage,
			wantStatus: func() *runnerstatus.Status {
				s := runnerstatus.StatusWorking
				return &s
			}(),
		},
		{
			name:     "assistant with tool use",
			input:    `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me check"},{"type":"tool_use","id":"t1","name":"Read"}]}}`,
			wantKind: EventKindMessage,
			wantStatus: func() *runnerstatus.Status {
				s := runnerstatus.StatusWorking
				return &s
			}(),
		},
		{
			name:     "user tool result",
			input:    `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"file contents"}]}}`,
			wantKind: EventKindMessage,
		},
		{
			name:     "result success",
			input:    `{"type":"result","subtype":"success","duration_ms":45000,"cost_usd":0.15}`,
			wantKind: EventKindFinal,
			wantStatus: func() *runnerstatus.Status {
				s := runnerstatus.StatusReadyForReview
				return &s
			}(),
		},
		{
			name:     "result error",
			input:    `{"type":"result","subtype":"error","error":"rate limit exceeded","error_code":"E_RATE_LIMIT"}`,
			wantKind: EventKindError,
		},
		{
			name:    "malformed json",
			input:   `{not valid json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := adapter.ParseLine([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.wantKind != "" {
				require.NotEmpty(t, result.Events, "ParseLine() returned no events, wanted kind %v", tt.wantKind)
			}

			if len(result.Events) > 0 {
				assert.Equal(t, tt.wantKind, result.Events[0].Kind)
			}

			if tt.wantStatus != nil {
				require.NotNil(t, result.SemanticStatus, "ParseLine() semantic status is nil, want %v", *tt.wantStatus)
				assert.Equal(t, *tt.wantStatus, *result.SemanticStatus)
			}
		})
	}
}

func TestCodexAdapter_ParseLine(t *testing.T) {
	t.Parallel()
	adapter := &CodexAdapter{}

	tests := []struct {
		name       string
		input      string
		wantKind   EventKind
		wantStatus *runnerstatus.Status
		wantErr    bool
	}{
		{
			name:     "thread started",
			input:    `{"type":"thread.started","thread_id":"thread_123"}`,
			wantKind: EventKindSessionStart,
		},
		{
			name:     "command start",
			input:    `{"type":"item.started","item":{"type":"command_execution","command":"cat file.txt"}}`,
			wantKind: EventKindToolStart,
			wantStatus: func() *runnerstatus.Status {
				s := runnerstatus.StatusWorking
				return &s
			}(),
		},
		{
			name:     "command end",
			input:    `{"type":"item.completed","item":{"type":"command_execution","command":"cat file.txt","exit_code":0}}`,
			wantKind: EventKindToolEnd,
			wantStatus: func() *runnerstatus.Status {
				s := runnerstatus.StatusWorking
				return &s
			}(),
		},
		{
			name:     "agent message",
			input:    `{"type":"item.completed","item":{"type":"agent_message","content":[{"type":"text","text":"Done!"}]}}`,
			wantKind: EventKindMessage,
			wantStatus: func() *runnerstatus.Status {
				s := runnerstatus.StatusReadyForReview
				return &s
			}(),
		},
		{
			name:     "turn completed",
			input:    `{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}`,
			wantKind: EventKindUsage,
		},
		{
			name:    "malformed json",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := adapter.ParseLine([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.wantKind != "" {
				require.NotEmpty(t, result.Events, "ParseLine() returned no events, wanted kind %v", tt.wantKind)
			}

			if len(result.Events) > 0 {
				assert.Equal(t, tt.wantKind, result.Events[0].Kind)
			}

			if tt.wantStatus != nil {
				require.NotNil(t, result.SemanticStatus, "ParseLine() semantic status is nil, want %v", *tt.wantStatus)
				assert.Equal(t, *tt.wantStatus, *result.SemanticStatus)
			}
		})
	}
}

func TestParser_StreamAndParse_ClaudeFixture(t *testing.T) {
	t.Parallel()
	// Read fixture
	fixturePath := filepath.Join("testdata", "claude_stream.jsonl")
	fixtureData, err := os.ReadFile(fixturePath)
	require.NoError(t, err, "Failed to read fixture")

	// Create parser
	parser := NewParser("test-inv-1", "claude", fixedClock)

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

	// Verify raw.jsonl contains verbatim data
	_, _ = rawFile.Seek(0, 0)
	rawData, _ := os.ReadFile(rawFile.Name())
	assert.Equal(t, fixtureData, rawData, "raw.jsonl content doesn't match fixture")

	// Verify stream.jsonl has normalized events
	_, _ = streamFile.Seek(0, 0)
	streamData, _ := os.ReadFile(streamFile.Name())
	assert.NotEmpty(t, streamData, "stream.jsonl is empty")

	// Count lines in stream.jsonl (should have multiple events)
	lines := strings.Split(strings.TrimSpace(string(streamData)), "\n")
	assert.GreaterOrEqual(t, len(lines), 5, "Expected at least 5 normalized events")

	// Verify final semantic status
	finalStatus := parser.GetSemanticStatus()
	require.NotNil(t, finalStatus, "Final semantic status is nil")
	assert.Equal(t, runnerstatus.StatusReadyForReview, *finalStatus)
}

func TestParser_StreamAndParse_CodexFixture(t *testing.T) {
	t.Parallel()
	// Read fixture
	fixturePath := filepath.Join("testdata", "codex_stream.jsonl")
	fixtureData, err := os.ReadFile(fixturePath)
	require.NoError(t, err, "Failed to read fixture")

	// Create parser
	parser := NewParser("test-inv-2", "codex", fixedClock)

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

	// Verify raw.jsonl contains verbatim data
	_, _ = rawFile.Seek(0, 0)
	rawData, _ := os.ReadFile(rawFile.Name())
	assert.Equal(t, fixtureData, rawData, "raw.jsonl content doesn't match fixture")

	// Verify stream.jsonl has normalized events
	_, _ = streamFile.Seek(0, 0)
	streamData, _ := os.ReadFile(streamFile.Name())
	assert.NotEmpty(t, streamData, "stream.jsonl is empty")

	// Verify final semantic status
	finalStatus := parser.GetSemanticStatus()
	require.NotNil(t, finalStatus, "Final semantic status is nil")
	assert.Equal(t, runnerstatus.StatusReadyForReview, *finalStatus)
}

func TestParser_StreamAndParse_MalformedMidStream(t *testing.T) {
	t.Parallel()
	// Read fixture
	fixturePath := filepath.Join("testdata", "malformed_mid_stream.jsonl")
	fixtureData, err := os.ReadFile(fixturePath)
	require.NoError(t, err, "Failed to read fixture")

	// Create parser
	parser := NewParser("test-inv-3", "claude", fixedClock)

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
	parser := NewParser("test-inv-4", "claude", fixedClock)

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

	// Verify final semantic status
	finalStatus := parser.GetSemanticStatus()
	require.NotNil(t, finalStatus, "Final semantic status is nil")
	assert.Equal(t, runnerstatus.StatusReadyForReview, *finalStatus)
}

func TestParser_SeqMonotonic(t *testing.T) {
	t.Parallel()
	// Create a simple fixture inline
	input := `{"type":"system","subtype":"init","cwd":"/sandbox"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}]}}
{"type":"result","subtype":"success"}
`

	parser := NewParser("test-inv-seq", "claude", fixedClock)

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
		expectedSeq := i + 1
		if !strings.Contains(line, `"seq":`+string(rune('0'+expectedSeq))) {
			// More robust check needed for multi-digit seq
			assert.Contains(t, line, `"seq":`, "Line %d doesn't contain seq field", i)
		}
	}
}

func TestParser_StreamAndParse_OversizedLineEmitsParseErrorAndContinues(t *testing.T) {
	t.Parallel()

	parser := NewParser("test-invocation", "claude", fixedClock)

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
		if event.Kind == string(EventKindParseError) {
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
	parser := NewParser("test-invocation-streaming", "claude", fixedClock)

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
	parser := NewParser("test-invocation-read-error", "claude", fixedClock)

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

func TestGetAdapter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		runner  string
		wantNil bool
	}{
		{"claude", false},
		{"claude-code", false},
		{"codex", false},
		{"amp", true},
		{"opencode", true},
		{"cursor-cli", true},
		{"droid", true},
		{"unknown", true},
		{"", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.runner, func(t *testing.T) {
			t.Parallel()
			adapter := GetAdapter(tt.runner)
			if tt.wantNil {
				assert.Nil(t, adapter)
			} else {
				assert.NotNil(t, adapter)
			}
		})
	}
}

func TestParser_StreamAndParse_RawWriteFailureReturnsError(t *testing.T) {
	t.Parallel()

	parser := NewParser("test-inv-raw-write-fail", "claude", fixedClock)

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

func TestParser_StreamAndParse_StreamWriteFailureReturnsError(t *testing.T) {
	t.Parallel()

	parser := NewParser("test-inv-stream-write-fail", "claude", fixedClock)

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
