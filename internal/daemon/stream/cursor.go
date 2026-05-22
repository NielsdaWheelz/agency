package stream

import (
	"encoding/json"
	"strings"
)

// cursorAdapter parses Cursor agent stream-json output.
// Cursor shares Claude-style message/result events, but tool activity is emitted
// via dedicated `tool_call` events instead of assistant content blocks.
type cursorAdapter struct{}

type cursorRawEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	ToolCall map[string]interface{} `json:"tool_call,omitempty"`
}

// ParseLine parses a single JSONL line from Cursor output.
func (a *cursorAdapter) ParseLine(line []byte) (*parseResult, error) {
	var raw cursorRawEvent
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, err
	}

	if raw.Type == "tool_call" {
		events := a.parseToolCall(&raw, line)
		return &parseResult{
			events: events,
		}, nil
	}

	// Cursor emits the same system/assistant/user/result event envelope handled by claudeAdapter.
	claude := &claudeAdapter{}
	return claude.ParseLine(line)
}

func (a *cursorAdapter) parseToolCall(raw *cursorRawEvent, line []byte) []*normalizedEvent {
	switch raw.Subtype {
	case "started", "completed":
	default:
		unknown := newUnknownRunnerEvent(raw.Type, "unsupported_tool_call_subtype", line)
		if strings.TrimSpace(raw.Subtype) != "" {
			unknown.Data["subtype"] = raw.Subtype
		}
		return []*normalizedEvent{unknown}
	}

	kind := eventKindToolStart
	if raw.Subtype == "completed" {
		kind = eventKindToolEnd
	}

	name, command, exitCode, actionFamily := parseCursorToolCall(raw.ToolCall)
	if strings.TrimSpace(name) == "" {
		return []*normalizedEvent{
			newUnknownRunnerEvent(raw.Type, "unrecognized_tool_structure", line),
		}
	}

	event := &normalizedEvent{
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
	if kind == eventKindToolEnd && exitCode != nil {
		event.Data["exit_code"] = *exitCode
	}
	return []*normalizedEvent{event}
}

var cursorToolCallKeyOrder = []struct {
	key       string
	canonical string
	family    string
}{
	{key: "readToolCall", canonical: "Read", family: actionFamilyFileRead},
	{key: "globToolCall", canonical: "Glob", family: actionFamilySearch},
	{key: "grepToolCall", canonical: "Grep", family: actionFamilySearch},
	{key: "bashToolCall", canonical: "Bash", family: actionFamilyCommandExecution},
	{key: "shellToolCall", canonical: "Bash", family: actionFamilyCommandExecution},
	{key: "editToolCall", canonical: "Edit", family: actionFamilyFileChange},
	{key: "writeToolCall", canonical: "Write", family: actionFamilyFileChange},
	{key: "multiEditToolCall", canonical: "MultiEdit", family: actionFamilyFileChange},
	{key: "notebookEditToolCall", canonical: "NotebookEdit", family: actionFamilyFileChange},
	{key: "webSearchToolCall", canonical: "WebSearch", family: actionFamilyWebAction},
	{key: "webFetchToolCall", canonical: "WebFetch", family: actionFamilyWebAction},
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
