package stream

import (
	"encoding/json"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
)

// CursorAdapter parses Cursor agent stream-json output.
// Cursor shares Claude-style message/result events, but tool activity is emitted
// via dedicated `tool_call` events instead of assistant content blocks.
type CursorAdapter struct{}

// Name returns the runner name.
func (a *CursorAdapter) Name() string {
	return "cursor"
}

type cursorRawEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	ToolCall map[string]interface{} `json:"tool_call,omitempty"`
}

// ParseLine parses a single JSONL line from Cursor output.
func (a *CursorAdapter) ParseLine(line []byte) (*ParseResult, error) {
	var raw cursorRawEvent
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, err
	}

	if raw.Type == "tool_call" {
		events, status := a.parseToolCall(&raw)
		return &ParseResult{
			Events:         events,
			SemanticStatus: status,
		}, nil
	}

	// Cursor emits Claude-compatible system/assistant/user/result events.
	claude := &ClaudeAdapter{}
	return claude.ParseLine(line)
}

func (a *CursorAdapter) parseToolCall(raw *cursorRawEvent) ([]*NormalizedEvent, *runnerstatus.Status) {
	switch raw.Subtype {
	case "started", "completed":
	default:
		return []*NormalizedEvent{}, nil
	}

	kind := EventKindToolStart
	if raw.Subtype == "completed" {
		kind = EventKindToolEnd
	}

	name, command, exitCode := parseCursorToolCall(raw.ToolCall)

	event := &NormalizedEvent{
		Kind: kind,
		Data: make(map[string]interface{}),
	}
	if name != "" {
		event.Data["name"] = name
	}
	if command != "" {
		event.Data["command"] = command
	}
	if kind == EventKindToolEnd && exitCode != nil {
		event.Data["exit_code"] = *exitCode
	}

	status := runnerstatus.StatusWorking
	return []*NormalizedEvent{event}, &status
}

var cursorToolCallKeyOrder = []struct {
	key       string
	canonical string
}{
	{key: "readToolCall", canonical: "Read"},
	{key: "globToolCall", canonical: "Glob"},
	{key: "grepToolCall", canonical: "Grep"},
	{key: "bashToolCall", canonical: "Bash"},
	{key: "editToolCall", canonical: "Edit"},
	{key: "writeToolCall", canonical: "Write"},
	{key: "multiEditToolCall", canonical: "MultiEdit"},
	{key: "notebookEditToolCall", canonical: "NotebookEdit"},
}

func parseCursorToolCall(toolCall map[string]interface{}) (string, string, *int) {
	if toolCall == nil {
		return "", "", nil
	}

	for _, candidate := range cursorToolCallKeyOrder {
		nested, ok := toolCall[candidate.key]
		if !ok {
			continue
		}
		nestedMap, _ := nested.(map[string]interface{})
		command := cursorToolCommand(nestedMap)
		if command == "" {
			command = cursorToolCommand(toolCall)
		}
		exitCode := cursorToolExitCode(nestedMap)
		if exitCode == nil {
			exitCode = cursorToolExitCode(toolCall)
		}
		return candidate.canonical, command, exitCode
	}

	name := canonicalCursorToolName(cursorString(toolCall, "name"))
	command := cursorToolCommand(toolCall)
	exitCode := cursorToolExitCode(toolCall)
	return name, command, exitCode
}

func canonicalCursorToolName(name string) string {
	n := strings.TrimSpace(name)
	switch strings.ToLower(n) {
	case "read":
		return "Read"
	case "glob":
		return "Glob"
	case "grep":
		return "Grep"
	case "bash":
		return "Bash"
	case "edit":
		return "Edit"
	case "write":
		return "Write"
	case "multiedit":
		return "MultiEdit"
	case "notebookedit":
		return "NotebookEdit"
	default:
		return n
	}
}

func cursorToolCommand(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	if command := cursorString(data, "command"); command != "" {
		return command
	}
	if path := cursorString(data, "path"); path != "" {
		return path
	}
	if path := cursorString(data, "target_file"); path != "" {
		return path
	}
	if path := cursorString(data, "targetFile"); path != "" {
		return path
	}
	return ""
}

func cursorToolExitCode(data map[string]interface{}) *int {
	if data == nil {
		return nil
	}
	if v, ok := data["exit_code"]; ok {
		if i, ok := anyToInt(v); ok {
			return &i
		}
	}
	if v, ok := data["exitCode"]; ok {
		if i, ok := anyToInt(v); ok {
			return &i
		}
	}
	return nil
}

func cursorString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	raw, ok := data[key]
	if !ok {
		return ""
	}
	v, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

func anyToInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}
