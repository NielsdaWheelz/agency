package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// TranscriptEntry is a subset of daemon.TimelineEntryDTO containing the
// fields needed for rendering. Defined here to avoid an import cycle
// (daemon already imports render).
type TranscriptEntry struct {
	Kind      string
	Timestamp string // RFC3339, displayed in headers
	Data      map[string]interface{}
}

// TranscriptOpts controls transcript rendering behavior.
type TranscriptOpts struct {
	NoColor bool // caller should check NO_COLOR env and set this
	// ExpandToolPayloads renders full tool input payloads for tool_use blocks.
	// Default false keeps human transcripts concise.
	ExpandToolPayloads bool
}

// ANSI escape codes for terminal styling.
const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiDim   = "\033[2m"
	ansiRed   = "\033[31m"
	ansiGreen = "\033[32m"
	ansiCyan  = "\033[36m"
)

func (o TranscriptOpts) style(code, text string) string {
	if o.NoColor {
		return text
	}
	return code + text + ansiReset
}

// WriteTranscript renders timeline entries as a human-readable transcript.
func WriteTranscript(w io.Writer, entries []TranscriptEntry, opts TranscriptOpts) error {
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "No timeline entries found.")
		return err
	}

	for _, entry := range entries {
		if err := renderEntry(w, entry, opts); err != nil {
			return err
		}
	}
	return nil
}

func renderEntry(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	switch entry.Kind {
	case "session_start":
		return renderSessionStart(w, entry, opts)
	case "prompt_seed":
		return renderPromptSeed(w, entry, opts)
	case "message":
		role := entryString(entry.Data, "role")
		if role == "assistant" {
			return renderAssistantMessage(w, entry, opts)
		}
		return renderUserMessage(w, entry, opts)
	case "tool_use":
		return renderToolUse(w, entry, opts)
	case "followup_prompt":
		return renderFollowupPrompt(w, entry, opts)
	case "final":
		return renderFinal(w, entry, opts)
	case "usage":
		return renderUsage(w, entry, opts)
	case "error":
		return renderError(w, entry, opts)
	case "parse_error":
		return renderParseError(w, entry, opts)
	case "unknown":
		return renderUnknown(w, entry, opts)
	case "checkpoint_event", "invocation_event":
		return renderEvent(w, entry, opts)
	case "raw_log_coverage":
		return nil // omit
	default:
		return nil
	}
}

func renderSessionStart(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	model := entryString(entry.Data, "model")
	cwd := entryString(entry.Data, "cwd")
	var parts []string
	if entry.Timestamp != "" {
		parts = append(parts, entry.Timestamp)
	}
	if model != "" {
		parts = append(parts, "model="+model)
	}
	if cwd != "" {
		parts = append(parts, "cwd="+cwd)
	}
	line := "Session started"
	if len(parts) > 0 {
		line += " (" + strings.Join(parts, ", ") + ")"
	}
	_, err := fmt.Fprintln(w, opts.style(ansiDim, line))
	return err
}

func renderPromptSeed(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	_, err := fmt.Fprintln(w, opts.style(ansiDim, "── Prompt ──"))
	if err != nil {
		return err
	}
	text := entryString(entry.Data, "text")
	if text != "" {
		_, err = fmt.Fprintln(w, indentText(text, "  "))
	}
	return err
}

func renderAssistantMessage(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	_, err := fmt.Fprintln(w)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, opts.style(ansiBold, "Assistant"))
	if err != nil {
		return err
	}

	// Prefer content_blocks if available
	if blocks := entryContentBlocks(entry.Data); len(blocks) > 0 {
		for _, block := range blocks {
			blockType, _ := block["type"].(string)
			switch blockType {
			case "text":
				if text, ok := block["text"].(string); ok && text != "" {
					_, err = fmt.Fprintln(w, text)
					if err != nil {
						return err
					}
				}
			case "tool_use":
				name, _ := block["name"].(string)
				if name == "" {
					name = "unknown"
				}
				_, err = fmt.Fprintln(w, opts.style(ansiCyan, "▶ Tool: "+name))
				if err != nil {
					return err
				}
				if input, ok := block["input"]; ok {
					if opts.ExpandToolPayloads {
						if err := renderJSONIndented(w, input, "  "); err != nil {
							return err
						}
					} else {
						_, err = fmt.Fprintln(w, opts.style(ansiDim, "  (input hidden; use raw/json to inspect)"))
						if err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	}

	// Fallback to text field
	text := entryString(entry.Data, "text")
	if text != "" {
		_, err = fmt.Fprintln(w, text)
	}
	return err
}

func renderUserMessage(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	if !isToolResultMessage(entry.Data) {
		_, err := fmt.Fprintln(w)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, opts.style(ansiBold, "User"))
		if err != nil {
			return err
		}
		text := promptMessageText(entry.Data)
		if text != "" {
			_, err = fmt.Fprintln(w, text)
		}
		return err
	}

	// Prefer content_blocks
	if blocks := entryContentBlocks(entry.Data); len(blocks) > 0 {
		_, err := fmt.Fprintln(w, opts.style(ansiDim, "Tool Result"))
		if err != nil {
			return err
		}
		for _, block := range blocks {
			if content, ok := block["content"].(string); ok && content != "" {
				_, err = fmt.Fprintln(w, indentText(content, "  "))
				if err != nil {
					return err
				}
			}
			if text, ok := block["text"].(string); ok && text != "" {
				_, err = fmt.Fprintln(w, indentText(text, "  "))
				if err != nil {
					return err
				}
			}
		}
		return nil
	}

	// Fallback
	text := entryString(entry.Data, "text")
	if text != "" {
		_, err := fmt.Fprintln(w, opts.style(ansiDim, "Tool Result"))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, indentText(text, "  "))
		return err
	}
	return nil
}

func renderToolUse(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	name := entryString(entry.Data, "name")
	command := entryString(entry.Data, "command")

	label := "▶"
	if name != "" {
		label += " " + name
	}
	if command != "" {
		label += " " + command
	}

	// Color exit code
	if exitCode, ok := entryFloat(entry.Data, "exit_code"); ok {
		ec := int(exitCode)
		if ec == 0 {
			label += " " + opts.style(ansiGreen, fmt.Sprintf("(exit=%d)", ec))
		} else {
			label += " " + opts.style(ansiRed, fmt.Sprintf("(exit=%d)", ec))
		}
	}

	_, err := fmt.Fprintln(w, label)
	return err
}

func renderFollowupPrompt(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	_, err := fmt.Fprintln(w)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, opts.style(ansiBold, "User"))
	if err != nil {
		return err
	}
	text := entryString(entry.Data, "text")
	if text != "" {
		_, err = fmt.Fprintln(w, text)
	}
	return err
}

func renderFinal(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	var parts []string

	if durationMS, ok := entryFloat(entry.Data, "duration_ms"); ok {
		secs := durationMS / 1000.0
		if secs >= 60 {
			parts = append(parts, fmt.Sprintf("%.1fm", secs/60.0))
		} else {
			parts = append(parts, fmt.Sprintf("%.1fs", secs))
		}
	}

	if costUSD, ok := entryFloat(entry.Data, "cost_usd"); ok {
		parts = append(parts, fmt.Sprintf("$%.4f", costUSD))
	}

	if usage, ok := entry.Data["usage"]; ok {
		if usageMap, ok := usage.(map[string]interface{}); ok {
			if v, ok := entryFloat(usageMap, "input_tokens"); ok {
				parts = append(parts, fmt.Sprintf("in=%d", int64(v)))
			}
			if v, ok := entryFloat(usageMap, "output_tokens"); ok {
				parts = append(parts, fmt.Sprintf("out=%d", int64(v)))
			}
		}
	}

	line := "Done"
	if len(parts) > 0 {
		line += " (" + strings.Join(parts, ", ") + ")"
	}

	_, err := fmt.Fprintln(w, opts.style(ansiDim, line))
	return err
}

func renderUsage(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	var parts []string
	if in, ok := entryFloat(entry.Data, "input_tokens"); ok {
		parts = append(parts, fmt.Sprintf("in=%d", int64(in)))
	}
	if out, ok := entryFloat(entry.Data, "output_tokens"); ok {
		parts = append(parts, fmt.Sprintf("out=%d", int64(out)))
	}
	if len(parts) == 0 {
		return nil
	}
	_, err := fmt.Fprintln(w, opts.style(ansiDim, "Usage ("+strings.Join(parts, ", ")+")"))
	return err
}

func renderError(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	msg := entryString(entry.Data, "message")
	if msg == "" {
		msg = "unknown error"
	}
	_, err := fmt.Fprintln(w, opts.style(ansiRed, "Error: "+msg))
	return err
}

func renderParseError(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	reason := strings.TrimSpace(entryString(entry.Data, "reason"))
	if reason == "" {
		reason = "unclassified"
	}
	line := "Parse diagnostic: " + reason
	if count, ok := entryFloat(entry.Data, "parse_error_count"); ok && int(count) > 0 {
		line += fmt.Sprintf(" (count=%d)", int(count))
	}
	_, err := fmt.Fprintln(w, opts.style(ansiDim, line))
	return err
}

func renderUnknown(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	eventType := strings.TrimSpace(entryString(entry.Data, "runner_event_type"))
	reason := strings.TrimSpace(entryString(entry.Data, "reason"))
	line := "Unknown runner event"
	if eventType != "" {
		line += ": " + eventType
	}
	if reason != "" {
		line += " (" + reason + ")"
	}
	_, err := fmt.Fprintln(w, opts.style(ansiDim, line))
	return err
}

func renderEvent(w io.Writer, entry TranscriptEntry, opts TranscriptOpts) error {
	kind := entryString(entry.Data, "event_kind")
	if kind == "" {
		kind = entry.Kind
	}
	_, err := fmt.Fprintln(w, opts.style(ansiDim, "["+kind+"]"))
	return err
}

// Helper functions

func entryString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func entryFloat(data map[string]interface{}, key string) (float64, bool) {
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
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func entryContentBlocks(data map[string]interface{}) []map[string]interface{} {
	if data == nil {
		return nil
	}
	v, ok := data["content_blocks"]
	if !ok {
		return nil
	}
	if blocks, ok := v.([]map[string]interface{}); ok {
		return blocks
	}
	// After JSON round-trip, may be []interface{}
	if arr, ok := v.([]interface{}); ok {
		result := make([]map[string]interface{}, 0, len(arr))
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				result = append(result, m)
			}
		}
		return result
	}
	return nil
}

func indentText(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func renderJSONIndented(w io.Writer, v interface{}, prefix string) error {
	data, err := json.Marshal(v)
	if err != nil {
		_, err = fmt.Fprintf(w, "%s%v\n", prefix, v)
		return err
	}

	var buf bytes.Buffer
	// json.Indent's prefix applies to lines 2+; we prepend prefix to line 1.
	if json.Indent(&buf, data, prefix, "  ") == nil {
		_, err = fmt.Fprintln(w, prefix+buf.String())
	} else {
		_, err = fmt.Fprintln(w, prefix+string(data))
	}
	return err
}

func isToolResultMessage(data map[string]interface{}) bool {
	family := strings.ToLower(strings.TrimSpace(entryString(data, "message_family")))
	switch family {
	case "prompt":
		return false
	case "tool_result":
		return true
	}

	for _, block := range entryContentBlocks(data) {
		if strings.EqualFold(strings.TrimSpace(entryString(block, "type")), "tool_result") {
			return true
		}
	}
	return false
}

func promptMessageText(data map[string]interface{}) string {
	if text := strings.TrimSpace(entryString(data, "text")); text != "" {
		return text
	}
	blocks := entryContentBlocks(data)
	if len(blocks) == 0 {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		blockType := strings.TrimSpace(entryString(block, "type"))
		if blockType != "" && blockType != "text" {
			continue
		}
		if text := strings.TrimSpace(entryString(block, "text")); text != "" {
			parts = append(parts, text)
			continue
		}
		if content := strings.TrimSpace(entryString(block, "content")); content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}
