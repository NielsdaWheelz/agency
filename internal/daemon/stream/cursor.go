package stream

import (
	"encoding/json"
	"strings"
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
		events := a.parseToolCall(&raw, line)
		return &ParseResult{
			Events: events,
		}, nil
	}

	// Cursor emits Claude-compatible system/assistant/user/result events.
	claude := &ClaudeAdapter{}
	return claude.ParseLine(line)
}

func (a *CursorAdapter) parseToolCall(raw *cursorRawEvent, line []byte) []*NormalizedEvent {
	switch raw.Subtype {
	case "started", "completed":
	default:
		unknown := newUnknownRunnerEvent(raw.Type, "unsupported_tool_call_subtype", line)
		if strings.TrimSpace(raw.Subtype) != "" {
			unknown.Data["subtype"] = raw.Subtype
		}
		return []*NormalizedEvent{unknown}
	}

	kind := EventKindToolStart
	if raw.Subtype == "completed" {
		kind = EventKindToolEnd
	}

	name, command, exitCode, actionFamily := parseCursorToolCall(raw.ToolCall)
	if strings.TrimSpace(name) == "" {
		return []*NormalizedEvent{
			newUnknownRunnerEvent(raw.Type, "unrecognized_tool_structure", line),
		}
	}

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
	if actionFamily == "" && name != "" {
		actionFamily = actionFamilyForToolName(name)
	}
	if actionFamily != "" {
		event.Data["action_family"] = actionFamily
	}
	if kind == EventKindToolEnd && exitCode != nil {
		event.Data["exit_code"] = *exitCode
	}
	return []*NormalizedEvent{event}
}

var cursorToolCallKeyOrder = []struct {
	key       string
	canonical string
	family    string
}{
	{key: "readToolCall", canonical: "Read", family: ActionFamilyFileRead},
	{key: "globToolCall", canonical: "Glob", family: ActionFamilySearch},
	{key: "grepToolCall", canonical: "Grep", family: ActionFamilySearch},
	{key: "bashToolCall", canonical: "Bash", family: ActionFamilyCommandExecution},
	{key: "shellToolCall", canonical: "Bash", family: ActionFamilyCommandExecution},
	{key: "editToolCall", canonical: "Edit", family: ActionFamilyFileChange},
	{key: "writeToolCall", canonical: "Write", family: ActionFamilyFileChange},
	{key: "multiEditToolCall", canonical: "MultiEdit", family: ActionFamilyFileChange},
	{key: "notebookEditToolCall", canonical: "NotebookEdit", family: ActionFamilyFileChange},
	{key: "webSearchToolCall", canonical: "WebSearch", family: ActionFamilyWebAction},
	{key: "webFetchToolCall", canonical: "WebFetch", family: ActionFamilyWebAction},
}

func parseCursorToolCall(toolCall map[string]interface{}) (string, string, *int, string) {
	if toolCall == nil {
		return "", "", nil, ""
	}

	for _, candidate := range cursorToolCallKeyOrder {
		nested, ok := toolCall[candidate.key]
		if !ok {
			continue
		}
		nestedMap, _ := nested.(map[string]interface{})
		command := cursorToolCommand(cursorToolArgs(nestedMap))
		if command == "" {
			command = cursorToolCommand(nestedMap)
		}
		if command == "" {
			command = cursorToolCommand(toolCall)
		}
		exitCode := cursorToolExitCode(cursorToolResult(nestedMap))
		if exitCode == nil {
			exitCode = cursorToolExitCode(nestedMap)
		}
		if exitCode == nil {
			exitCode = cursorToolExitCode(cursorToolResult(toolCall))
		}
		if exitCode == nil {
			exitCode = cursorToolExitCode(toolCall)
		}
		return candidate.canonical, command, exitCode, candidate.family
	}

	name := canonicalCursorToolName(cursorString(toolCall, "name"))
	command := cursorToolCommand(cursorToolArgs(toolCall))
	if command == "" {
		command = cursorToolCommand(toolCall)
	}
	exitCode := cursorToolExitCode(cursorToolResult(toolCall))
	if exitCode == nil {
		exitCode = cursorToolExitCode(toolCall)
	}
	return name, command, exitCode, actionFamilyForToolName(name)
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
	case "websearch":
		return "WebSearch"
	case "webfetch":
		return "WebFetch"
	default:
		return n
	}
}

func cursorToolArgs(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}
	if args, ok := data["args"].(map[string]interface{}); ok {
		return args
	}
	return data
}

func cursorToolResult(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}
	result, ok := data["result"].(map[string]interface{})
	if !ok {
		return nil
	}
	// Prefer explicit failure payload when present.
	if failure, ok := result["failure"].(map[string]interface{}); ok {
		return failure
	}
	if success, ok := result["success"].(map[string]interface{}); ok {
		return success
	}
	return result
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
