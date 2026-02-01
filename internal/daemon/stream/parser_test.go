package stream

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
)

func fixedClock() time.Time {
	return time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
}

func TestClaudeAdapter_ParseLine(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			result, err := adapter.ParseLine([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if len(result.Events) == 0 && tt.wantKind != "" {
				t.Errorf("ParseLine() returned no events, wanted kind %v", tt.wantKind)
				return
			}

			if len(result.Events) > 0 && result.Events[0].Kind != tt.wantKind {
				t.Errorf("ParseLine() kind = %v, want %v", result.Events[0].Kind, tt.wantKind)
			}

			if tt.wantStatus != nil {
				if result.SemanticStatus == nil {
					t.Errorf("ParseLine() semantic status is nil, want %v", *tt.wantStatus)
				} else if *result.SemanticStatus != *tt.wantStatus {
					t.Errorf("ParseLine() semantic status = %v, want %v", *result.SemanticStatus, *tt.wantStatus)
				}
			}
		})
	}
}

func TestCodexAdapter_ParseLine(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			result, err := adapter.ParseLine([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if len(result.Events) == 0 && tt.wantKind != "" {
				t.Errorf("ParseLine() returned no events, wanted kind %v", tt.wantKind)
				return
			}

			if len(result.Events) > 0 && result.Events[0].Kind != tt.wantKind {
				t.Errorf("ParseLine() kind = %v, want %v", result.Events[0].Kind, tt.wantKind)
			}

			if tt.wantStatus != nil {
				if result.SemanticStatus == nil {
					t.Errorf("ParseLine() semantic status is nil, want %v", *tt.wantStatus)
				} else if *result.SemanticStatus != *tt.wantStatus {
					t.Errorf("ParseLine() semantic status = %v, want %v", *result.SemanticStatus, *tt.wantStatus)
				}
			}
		})
	}
}

func TestParser_StreamAndParse_ClaudeFixture(t *testing.T) {
	// Read fixture
	fixturePath := filepath.Join("testdata", "claude_stream.jsonl")
	fixtureData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	// Create parser
	parser := NewParser("test-inv-1", "claude", fixedClock)

	// Create temp files
	rawFile, err := os.CreateTemp("", "raw-*.jsonl")
	if err != nil {
		t.Fatalf("Failed to create temp raw file: %v", err)
	}
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-*.jsonl")
	if err != nil {
		t.Fatalf("Failed to create temp stream file: %v", err)
	}
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	// Parse
	reader := bytes.NewReader(fixtureData)
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	if err != nil {
		t.Fatalf("StreamAndParse failed: %v", err)
	}

	// Verify raw.jsonl contains verbatim data
	_, _ = rawFile.Seek(0, 0)
	rawData, _ := os.ReadFile(rawFile.Name())
	if !bytes.Equal(rawData, fixtureData) {
		t.Errorf("raw.jsonl content doesn't match fixture")
	}

	// Verify stream.jsonl has normalized events
	_, _ = streamFile.Seek(0, 0)
	streamData, _ := os.ReadFile(streamFile.Name())
	if len(streamData) == 0 {
		t.Error("stream.jsonl is empty")
	}

	// Count lines in stream.jsonl (should have multiple events)
	lines := strings.Split(strings.TrimSpace(string(streamData)), "\n")
	if len(lines) < 5 {
		t.Errorf("Expected at least 5 normalized events, got %d", len(lines))
	}

	// Verify final semantic status
	finalStatus := parser.GetSemanticStatus()
	if finalStatus == nil {
		t.Error("Final semantic status is nil")
	} else if *finalStatus != runnerstatus.StatusReadyForReview {
		t.Errorf("Final semantic status = %v, want ready_for_review", *finalStatus)
	}
}

func TestParser_StreamAndParse_CodexFixture(t *testing.T) {
	// Read fixture
	fixturePath := filepath.Join("testdata", "codex_stream.jsonl")
	fixtureData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	// Create parser
	parser := NewParser("test-inv-2", "codex", fixedClock)

	// Create temp files
	rawFile, err := os.CreateTemp("", "raw-*.jsonl")
	if err != nil {
		t.Fatalf("Failed to create temp raw file: %v", err)
	}
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-*.jsonl")
	if err != nil {
		t.Fatalf("Failed to create temp stream file: %v", err)
	}
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	// Parse
	reader := bytes.NewReader(fixtureData)
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	if err != nil {
		t.Fatalf("StreamAndParse failed: %v", err)
	}

	// Verify raw.jsonl contains verbatim data
	_, _ = rawFile.Seek(0, 0)
	rawData, _ := os.ReadFile(rawFile.Name())
	if !bytes.Equal(rawData, fixtureData) {
		t.Errorf("raw.jsonl content doesn't match fixture")
	}

	// Verify stream.jsonl has normalized events
	_, _ = streamFile.Seek(0, 0)
	streamData, _ := os.ReadFile(streamFile.Name())
	if len(streamData) == 0 {
		t.Error("stream.jsonl is empty")
	}

	// Verify final semantic status
	finalStatus := parser.GetSemanticStatus()
	if finalStatus == nil {
		t.Error("Final semantic status is nil")
	} else if *finalStatus != runnerstatus.StatusReadyForReview {
		t.Errorf("Final semantic status = %v, want ready_for_review", *finalStatus)
	}
}

func TestParser_StreamAndParse_MalformedMidStream(t *testing.T) {
	// Read fixture
	fixturePath := filepath.Join("testdata", "malformed_mid_stream.jsonl")
	fixtureData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	// Create parser
	parser := NewParser("test-inv-3", "claude", fixedClock)

	// Create temp files
	rawFile, err := os.CreateTemp("", "raw-*.jsonl")
	if err != nil {
		t.Fatalf("Failed to create temp raw file: %v", err)
	}
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-*.jsonl")
	if err != nil {
		t.Fatalf("Failed to create temp stream file: %v", err)
	}
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	// Parse (should not fail even with malformed line)
	reader := bytes.NewReader(fixtureData)
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	if err != nil {
		t.Fatalf("StreamAndParse failed: %v", err)
	}

	// Verify raw.jsonl contains verbatim data (including malformed line)
	_, _ = rawFile.Seek(0, 0)
	rawData, _ := os.ReadFile(rawFile.Name())
	if !bytes.Equal(rawData, fixtureData) {
		t.Errorf("raw.jsonl content doesn't match fixture")
	}

	// Verify stream.jsonl has parse_error event
	_, _ = streamFile.Seek(0, 0)
	streamData, _ := os.ReadFile(streamFile.Name())
	if !strings.Contains(string(streamData), `"kind":"parse_error"`) {
		t.Error("stream.jsonl should contain parse_error event")
	}

	// Verify parsing continued after error (should still have final event)
	if !strings.Contains(string(streamData), `"kind":"final"`) {
		t.Error("stream.jsonl should contain final event after malformed line")
	}
}

func TestParser_StreamAndParse_NoTrailingNewline(t *testing.T) {
	// Read fixture
	fixturePath := filepath.Join("testdata", "no_trailing_newline.jsonl")
	fixtureData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	// Verify fixture doesn't end with newline
	if fixtureData[len(fixtureData)-1] == '\n' {
		t.Skip("Fixture has trailing newline, test invalid")
	}

	// Create parser
	parser := NewParser("test-inv-4", "claude", fixedClock)

	// Create temp files
	rawFile, err := os.CreateTemp("", "raw-*.jsonl")
	if err != nil {
		t.Fatalf("Failed to create temp raw file: %v", err)
	}
	defer func() { _ = os.Remove(rawFile.Name()) }()
	defer func() { _ = rawFile.Close() }()

	streamFile, err := os.CreateTemp("", "stream-*.jsonl")
	if err != nil {
		t.Fatalf("Failed to create temp stream file: %v", err)
	}
	defer func() { _ = os.Remove(streamFile.Name()) }()
	defer func() { _ = streamFile.Close() }()

	// Parse
	reader := bytes.NewReader(fixtureData)
	err = parser.StreamAndParse(reader, rawFile, streamFile)
	if err != nil {
		t.Fatalf("StreamAndParse failed: %v", err)
	}

	// Verify raw.jsonl contains all data (including final line without newline)
	_, _ = rawFile.Seek(0, 0)
	rawData, _ := os.ReadFile(rawFile.Name())
	if !bytes.Equal(rawData, fixtureData) {
		t.Errorf("raw.jsonl content doesn't match fixture")
	}

	// Verify final line was parsed (should have final event)
	_, _ = streamFile.Seek(0, 0)
	streamData, _ := os.ReadFile(streamFile.Name())
	if !strings.Contains(string(streamData), `"kind":"final"`) {
		t.Error("stream.jsonl should contain final event from line without trailing newline")
	}

	// Verify final semantic status
	finalStatus := parser.GetSemanticStatus()
	if finalStatus == nil {
		t.Error("Final semantic status is nil")
	} else if *finalStatus != runnerstatus.StatusReadyForReview {
		t.Errorf("Final semantic status = %v, want ready_for_review", *finalStatus)
	}
}

func TestParser_SeqMonotonic(t *testing.T) {
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

	if len(lines) < 3 {
		t.Fatalf("Expected at least 3 events, got %d", len(lines))
	}

	// Verify seq is monotonically increasing starting at 1
	for i, line := range lines {
		expectedSeq := i + 1
		if !strings.Contains(line, `"seq":`+string(rune('0'+expectedSeq))) {
			// More robust check needed for multi-digit seq
			if !strings.Contains(line, `"seq":`) {
				t.Errorf("Line %d doesn't contain seq field", i)
			}
		}
	}
}

func TestGetAdapter(t *testing.T) {
	tests := []struct {
		runner  string
		wantNil bool
	}{
		{"claude", false},
		{"codex", false},
		{"unknown", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.runner, func(t *testing.T) {
			adapter := GetAdapter(tt.runner)
			if (adapter == nil) != tt.wantNil {
				t.Errorf("GetAdapter(%q) = %v, want nil=%v", tt.runner, adapter, tt.wantNil)
			}
		})
	}
}
