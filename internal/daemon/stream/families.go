package stream

import "strings"

const (
	ActionFamilyCommandExecution = "command_execution"
	ActionFamilyFileRead         = "file_read"
	ActionFamilyFileChange       = "file_change"
	ActionFamilySearch           = "search"
	ActionFamilyWebAction        = "web_action"
	ActionFamilyToolActivity     = "tool_activity"

	MessageFamilyAssistant  = "assistant"
	MessageFamilyPrompt     = "prompt"
	MessageFamilyToolResult = "tool_result"
)

const unparsedEventPreviewBytes = 4096

func actionFamilyForToolName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "shell", "command", "command_execution":
		return ActionFamilyCommandExecution
	case "read":
		return ActionFamilyFileRead
	case "edit", "write", "multiedit", "notebookedit", "filechange", "file_change":
		return ActionFamilyFileChange
	case "glob", "grep", "search":
		return ActionFamilySearch
	case "websearch", "webfetch", "browser":
		return ActionFamilyWebAction
	default:
		return ActionFamilyToolActivity
	}
}

func newUnknownRunnerEvent(rawType, reason string, line []byte) *NormalizedEvent {
	event := &NormalizedEvent{
		Kind: EventKindUnknown,
		Data: map[string]interface{}{},
	}

	if t := strings.TrimSpace(rawType); t != "" {
		event.Data["runner_event_type"] = t
	}
	if r := strings.TrimSpace(reason); r != "" {
		event.Data["reason"] = r
	}

	if len(line) > 0 {
		preview := line
		truncated := false
		if len(preview) > unparsedEventPreviewBytes {
			preview = preview[:unparsedEventPreviewBytes]
			truncated = true
		}
		event.Data["raw_json_preview"] = string(preview)
		if truncated {
			event.Data["raw_truncated"] = true
		}
	}
	return event
}
