package render

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteTranscript_Empty(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := WriteTranscript(&buf, nil, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Equal(t, "No timeline entries found.\n", buf.String())
}

func TestWriteTranscript_PromptSeed(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "prompt_seed", Data: map[string]interface{}{"text": "Fix the bug"}},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "── Prompt ──")
	assert.Contains(t, buf.String(), "  Fix the bug")
}

func TestWriteTranscript_AssistantWithContentBlocks(t *testing.T) {
	t.Parallel()
	payloadMarker := "S8_PR06_TOOL_INPUT_PAYLOAD_SHOULD_BE_HIDDEN"
	entries := []TranscriptEntry{
		{
			Kind: "message",
			Data: map[string]interface{}{
				"role": "assistant",
				"text": "Let me check",
				"content_blocks": []map[string]interface{}{
					{"type": "text", "text": "Let me check"},
					{"type": "tool_use", "name": "Read", "id": "t1", "input": map[string]interface{}{"path": "/foo", "payload": payloadMarker}},
				},
			},
		},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Assistant")
	assert.Contains(t, out, "Let me check")
	assert.Contains(t, out, "▶ Tool: Read")
	assert.NotContains(t, out, payloadMarker, "default transcript rendering should not dump full tool input payloads")
	assert.Contains(t, out, "input hidden", "default transcript rendering should make explicit that tool payloads are summarized")
}

func TestWriteTranscript_AssistantWithContentBlocks_ExpandedToolPayloads(t *testing.T) {
	t.Parallel()
	payloadMarker := "S8_PR06_TOOL_INPUT_PAYLOAD_SHOULD_BE_VISIBLE_WHEN_EXPANDED"
	entries := []TranscriptEntry{
		{
			Kind: "message",
			Data: map[string]interface{}{
				"role": "assistant",
				"content_blocks": []map[string]interface{}{
					{"type": "tool_use", "name": "Read", "id": "t1", "input": map[string]interface{}{"path": "/foo", "payload": payloadMarker}},
				},
			},
		},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true, ExpandToolPayloads: true})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, payloadMarker)
	assert.NotContains(t, out, "input hidden")
}

func TestWriteTranscript_AssistantWithoutContentBlocks(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{
			Kind: "message",
			Data: map[string]interface{}{
				"role": "assistant",
				"text": "Hello world",
			},
		},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Assistant")
	assert.Contains(t, out, "Hello world")
}

func TestWriteTranscript_ToolUseEntry(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{
			Kind: "tool_use",
			Data: map[string]interface{}{
				"name":      "Bash",
				"command":   "ls -la",
				"exit_code": float64(0),
			},
		},
		{
			Kind: "tool_use",
			Data: map[string]interface{}{
				"name":      "Bash",
				"command":   "false",
				"exit_code": float64(1),
			},
		},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "▶ Bash ls -la (exit=0)")
	assert.Contains(t, out, "▶ Bash false (exit=1)")
}

func TestWriteTranscript_FinalEntry(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{
			Kind: "final",
			Data: map[string]interface{}{
				"duration_ms": float64(45000),
				"cost_usd":    float64(0.15),
			},
		},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Done")
	assert.Contains(t, out, "45.0s")
	assert.Contains(t, out, "$0.1500")
}

func TestWriteTranscript_FinalEntryWithUsage(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{
			Kind: "final",
			Data: map[string]interface{}{
				"duration_ms": float64(5000),
				"cost_usd":    float64(0.05),
				"usage": map[string]interface{}{
					"input_tokens":  float64(1234),
					"output_tokens": float64(567),
				},
			},
		},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "in=1234")
	assert.Contains(t, out, "out=567")
}

func TestWriteTranscript_UsageEntry(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{
			Kind: "usage",
			Data: map[string]interface{}{
				"input_tokens":  float64(42),
				"output_tokens": float64(7),
			},
		},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Usage (in=42, out=7)")
}

func TestWriteTranscript_ErrorEntry(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{
			Kind: "error",
			Data: map[string]interface{}{"message": "rate limit exceeded"},
		},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Error: rate limit exceeded")
}

func TestWriteTranscript_ErrorEntryEmptyMessage(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "error", Data: map[string]interface{}{}},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Error: unknown error")
}

func TestWriteTranscript_NoColor(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "error", Data: map[string]interface{}{"message": "fail"}},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.NotContains(t, buf.String(), "\033[")
}

func TestWriteTranscript_WithColor(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "error", Data: map[string]interface{}{"message": "fail"}},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: false})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "\033[")
}

func TestWriteTranscript_MixedConversation(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "session_start", Timestamp: "2026-02-01T12:00:00Z", Data: map[string]interface{}{"model": "claude-3", "cwd": "/sandbox"}},
		{Kind: "prompt_seed", Data: map[string]interface{}{"text": "Fix the bug"}},
		{
			Kind: "message",
			Data: map[string]interface{}{
				"role": "assistant",
				"text": "I'll look at the code",
				"content_blocks": []map[string]interface{}{
					{"type": "text", "text": "I'll look at the code"},
					{"type": "tool_use", "name": "Read", "id": "t1"},
				},
			},
		},
		{
			Kind: "message",
			Data: map[string]interface{}{
				"role": "user",
				"content_blocks": []map[string]interface{}{
					{"type": "tool_result", "content": "file contents here"},
				},
			},
		},
		{Kind: "tool_use", Data: map[string]interface{}{"name": "Bash", "command": "go test", "exit_code": float64(0)}},
		{Kind: "followup_prompt", Data: map[string]interface{}{"text": "looks good, continue"}},
		{Kind: "checkpoint_event", Data: map[string]interface{}{"event_kind": "checkpoint_created"}},
		{Kind: "final", Data: map[string]interface{}{"duration_ms": float64(120000), "cost_usd": float64(0.50)}},
	}

	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Session started (2026-02-01T12:00:00Z, model=claude-3, cwd=/sandbox)")
	assert.Contains(t, out, "── Prompt ──")
	assert.Contains(t, out, "Fix the bug")
	assert.Contains(t, out, "Assistant")
	assert.Contains(t, out, "I'll look at the code")
	assert.Contains(t, out, "▶ Tool: Read")
	assert.Contains(t, out, "Tool Result")
	assert.Contains(t, out, "file contents here")
	assert.Contains(t, out, "▶ Bash go test (exit=0)")
	assert.Contains(t, out, "User")
	assert.Contains(t, out, "looks good, continue")
	assert.Contains(t, out, "[checkpoint_created]")
	assert.Contains(t, out, "Done (2.0m, $0.5000)")
}

func TestWriteTranscript_SessionStartMinimal(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "session_start", Data: map[string]interface{}{}},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Equal(t, "Session started\n", buf.String())
}

func TestWriteTranscript_SessionStartWithTimestamp(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "session_start", Timestamp: "2026-01-15T10:30:00Z", Data: map[string]interface{}{"model": "claude-3"}},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "2026-01-15T10:30:00Z")
	assert.Contains(t, buf.String(), "model=claude-3")
}

func TestWriteTranscript_UserMessageWithTextAndNoFamilyRendersAsUser(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{
			Kind: "message",
			Data: map[string]interface{}{
				"role": "user",
				"text": "some tool output",
			},
		},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "User")
	assert.NotContains(t, buf.String(), "Tool Result")
	assert.Contains(t, buf.String(), "some tool output")
}

func TestWriteTranscript_UserPromptMessageRendersAsUser(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{
			Kind: "message",
			Data: map[string]interface{}{
				"role":           "user",
				"message_family": "prompt",
				"text":           "please continue from checkpoint two",
			},
		},
	}

	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "User")
	assert.Contains(t, out, "please continue from checkpoint two")
	assert.NotContains(t, out, "Tool Result")
}

func TestWriteTranscript_RawLogCoverageOmitted(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "raw_log_coverage", Data: map[string]interface{}{"bytes": float64(1024)}},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestWriteTranscript_NilData(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "error", Data: nil},
		{Kind: "message", Data: nil},
		{Kind: "final", Data: nil},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Error: unknown error")
	assert.Contains(t, buf.String(), "Done")
}

func TestWriteTranscript_UnknownKindSilentlySkipped(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "some_future_kind", Data: map[string]interface{}{"foo": "bar"}},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestWriteTranscript_ParseErrorAndUnknownDiagnostics(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{
			Kind: "parse_error",
			Data: map[string]interface{}{
				"parse_error_count": float64(2),
				"reason":            "line_too_large",
			},
		},
		{
			Kind: "unknown",
			Data: map[string]interface{}{
				"runner_event_type": "rate_limit_event",
				"reason":            "unsupported_event_type",
			},
		},
	}

	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "line_too_large")
	assert.Contains(t, out, "rate_limit_event")
}

func TestWriteTranscript_ContentBlocksAfterJSONRoundTrip(t *testing.T) {
	t.Parallel()
	// After JSON unmarshal, content_blocks becomes []interface{} not []map[string]interface{}
	entries := []TranscriptEntry{
		{
			Kind: "message",
			Data: map[string]interface{}{
				"role": "assistant",
				"content_blocks": []interface{}{
					map[string]interface{}{"type": "text", "text": "hello"},
					map[string]interface{}{"type": "tool_use", "name": "Bash"},
				},
			},
		},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "hello")
	assert.Contains(t, out, "▶ Tool: Bash")
}

func TestWriteTranscript_FollowupPrompt(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "followup_prompt", Data: map[string]interface{}{"text": "keep going"}},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "User")
	assert.Contains(t, out, "keep going")
}

func TestWriteTranscript_InvocationEvent(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{Kind: "invocation_event", Data: map[string]interface{}{"event_kind": "invocation_started"}},
	}
	var buf bytes.Buffer
	err := WriteTranscript(&buf, entries, TranscriptOpts{NoColor: true})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "[invocation_started]")
}

