package stream

import "strings"

const (
	actionFamilyCommandExecution = "command_execution"
	actionFamilyFileRead         = "file_read"
	actionFamilyFileChange       = "file_change"
	actionFamilySearch           = "search"
	actionFamilyWebAction        = "web_action"

	messageFamilyAssistant  = "assistant"
	messageFamilyPrompt     = "prompt"
	messageFamilyToolResult = "tool_result"
)

const unparsedEventPreviewBytes = 4096

func actionFamilyForToolName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "shell", "command", "command_execution":
		return actionFamilyCommandExecution
	case "read":
		return actionFamilyFileRead
	case "edit", "write", "multiedit", "notebookedit", "filechange", "file_change":
		return actionFamilyFileChange
	case "glob", "grep", "search":
		return actionFamilySearch
	case "websearch", "webfetch", "browser":
		return actionFamilyWebAction
	default:
		return "tool_activity"
	}
}

func newUnknownRunnerEvent(rawType, reason string, line []byte) *normalizedEvent {
	event := &normalizedEvent{
		Kind: eventKindUnknown,
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
