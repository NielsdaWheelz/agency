package relay

import (
	"encoding/json"
	"fmt"

	"github.com/NielsdaWheelz/agency/internal/runners"
)

// FormatStdinMessage builds the JSONL byte payload for a user follow-up message,
// formatted per the runner's stdin streaming protocol.
//
// Supported runners and their formats:
//   - claude-code: {"type":"user","message":{"role":"user","content":[{"type":"text","text":"..."}]}}
//   - amp:         {"type":"user","message":{"role":"user","content":[{"type":"text","text":"..."}]}}
//   - droid:       {"type":"user","message":{"role":"user","content":[{"type":"text","text":"..."}]}}
func FormatStdinMessage(runner, prompt string) ([]byte, error) {
	switch runner {
	case runners.RunnerClaudeCode, runners.RunnerAmp, runners.RunnerDroid:
		return formatClaudeStyleMessage(prompt)
	default:
		return nil, fmt.Errorf("runner %q does not support stdin message format", runner)
	}
}

// claudeStyleMessage is the JSONL format shared by Claude Code, Amp, and Droid.
// All three use the same {"type":"user","message":{...}} envelope with content blocks.
type claudeStyleMessage struct {
	Type    string             `json:"type"`
	Message claudeStylePayload `json:"message"`
}

type claudeStylePayload struct {
	Role    string               `json:"role"`
	Content []claudeContentBlock `json:"content"`
}

type claudeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func formatClaudeStyleMessage(prompt string) ([]byte, error) {
	msg := claudeStyleMessage{
		Type: "user",
		Message: claudeStylePayload{
			Role: "user",
			Content: []claudeContentBlock{
				{Type: "text", Text: prompt},
			},
		},
	}
	return json.Marshal(msg)
}
