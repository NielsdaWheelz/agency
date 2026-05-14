package render

import (
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
	payload := DecodeTimelinePayload(entry.Data)
	switch entry.Kind {
	case "session_start":
		return renderSessionStart(w, entry, payload, opts)
	case "prompt_seed":
		return renderPromptSeed(w, payload, opts)
	case "message":
		if payload.Role == "assistant" {
			return renderAssistantMessage(w, payload, opts)
		}
		return renderUserMessage(w, payload, opts)
	case "tool_use":
		return renderToolUse(w, payload, opts)
	case "followup_prompt":
		return renderFollowupPrompt(w, payload, opts)
	case "final":
		return renderFinal(w, payload, opts)
	case "usage":
		return renderUsage(w, payload, opts)
	case "error":
		return renderError(w, payload, opts)
	case "parse_error":
		return renderParseError(w, payload, opts)
	case "unknown":
		return renderUnknown(w, payload, opts)
	case "checkpoint_event", "invocation_event":
		return renderEvent(w, entry.Kind, payload, opts)
	case "raw_log_coverage":
		return nil // omit
	default:
		return nil
	}
}

func renderSessionStart(w io.Writer, entry TranscriptEntry, payload TimelinePayload, opts TranscriptOpts) error {
	var parts []string
	if entry.Timestamp != "" {
		parts = append(parts, entry.Timestamp)
	}
	if payload.Model != "" {
		parts = append(parts, "model="+payload.Model)
	}
	if payload.CWD != "" {
		parts = append(parts, "cwd="+payload.CWD)
	}
	line := "Session started"
	if len(parts) > 0 {
		line += " (" + strings.Join(parts, ", ") + ")"
	}
	_, err := fmt.Fprintln(w, opts.style(ansiDim, line))
	return err
}

func renderPromptSeed(w io.Writer, payload TimelinePayload, opts TranscriptOpts) error {
	_, err := fmt.Fprintln(w, opts.style(ansiDim, "── Prompt ──"))
	if err != nil {
		return err
	}
	if text := payload.PromptLikeSummary(); text != "" {
		_, err = fmt.Fprintln(w, indentText(text, "  "))
	}
	return err
}

func renderAssistantMessage(w io.Writer, payload TimelinePayload, opts TranscriptOpts) error {
	_, err := fmt.Fprintln(w)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, opts.style(ansiBold, "Assistant"))
	if err != nil {
		return err
	}

	// Prefer content_blocks if available
	if len(payload.Blocks) > 0 {
		for _, block := range payload.Blocks {
			switch block.Type {
			case "text":
				if text := block.Text; text != "" {
					_, err = fmt.Fprintln(w, text)
					if err != nil {
						return err
					}
				}
			case "tool_use":
				name := block.Name
				if name == "" {
					name = "unknown"
				}
				_, err = fmt.Fprintln(w, opts.style(ansiCyan, "▶ Tool: "+name))
				if err != nil {
					return err
				}
				if input := block.Input; input != nil {
					_, err = fmt.Fprintln(w, opts.style(ansiDim, "  (input hidden; use raw/json to inspect)"))
					if err != nil {
						return err
					}
				}
			}
		}
		return nil
	}

	// Fallback to text field
	if text := payload.Text; text != "" {
		_, err = fmt.Fprintln(w, text)
	}
	return err
}

func renderUserMessage(w io.Writer, payload TimelinePayload, opts TranscriptOpts) error {
	if !payload.IsToolResultMessage() {
		_, err := fmt.Fprintln(w)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, opts.style(ansiBold, "User"))
		if err != nil {
			return err
		}
		if text := payload.PromptMessageText(); text != "" {
			_, err = fmt.Fprintln(w, text)
		}
		return err
	}

	// Prefer content_blocks
	if len(payload.Blocks) > 0 {
		_, err := fmt.Fprintln(w, opts.style(ansiDim, "Tool Result"))
		if err != nil {
			return err
		}
		for _, block := range payload.Blocks {
			if content := block.Content; content != "" {
				_, err = fmt.Fprintln(w, indentText(content, "  "))
				if err != nil {
					return err
				}
			}
			if text := block.Text; text != "" {
				_, err = fmt.Fprintln(w, indentText(text, "  "))
				if err != nil {
					return err
				}
			}
		}
		return nil
	}

	// Fallback
	if text := payload.Text; text != "" {
		_, err := fmt.Fprintln(w, opts.style(ansiDim, "Tool Result"))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, indentText(text, "  "))
		return err
	}
	return nil
}

func renderToolUse(w io.Writer, payload TimelinePayload, opts TranscriptOpts) error {
	label := "▶"
	if payload.Name != "" {
		label += " " + payload.Name
	}
	if payload.Command != "" {
		label += " " + payload.Command
	}

	// Color exit code
	if payload.HasExitCode {
		if payload.ExitCode == 0 {
			label += " " + opts.style(ansiGreen, fmt.Sprintf("(exit=%d)", payload.ExitCode))
		} else {
			label += " " + opts.style(ansiRed, fmt.Sprintf("(exit=%d)", payload.ExitCode))
		}
	}

	_, err := fmt.Fprintln(w, label)
	return err
}

func renderFollowupPrompt(w io.Writer, payload TimelinePayload, opts TranscriptOpts) error {
	_, err := fmt.Fprintln(w)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, opts.style(ansiBold, "User"))
	if err != nil {
		return err
	}
	if text := payload.PromptLikeSummary(); text != "" {
		_, err = fmt.Fprintln(w, text)
	}
	return err
}

func renderFinal(w io.Writer, payload TimelinePayload, opts TranscriptOpts) error {
	var parts []string

	if payload.HasDurationMS {
		secs := payload.DurationMS / 1000.0
		if secs >= 60 {
			parts = append(parts, fmt.Sprintf("%.1fm", secs/60.0))
		} else {
			parts = append(parts, fmt.Sprintf("%.1fs", secs))
		}
	}

	if payload.HasCostUSD {
		parts = append(parts, fmt.Sprintf("$%.4f", payload.CostUSD))
	}

	if payload.Usage.HasInputTokens {
		parts = append(parts, fmt.Sprintf("in=%d", payload.Usage.InputTokens))
	}
	if payload.Usage.HasOutputTokens {
		parts = append(parts, fmt.Sprintf("out=%d", payload.Usage.OutputTokens))
	}

	line := "Done"
	if len(parts) > 0 {
		line += " (" + strings.Join(parts, ", ") + ")"
	}

	_, err := fmt.Fprintln(w, opts.style(ansiDim, line))
	return err
}

func renderUsage(w io.Writer, payload TimelinePayload, opts TranscriptOpts) error {
	var parts []string
	if payload.HasInputTokens {
		parts = append(parts, fmt.Sprintf("in=%d", payload.InputTokens))
	}
	if payload.HasOutputTokens {
		parts = append(parts, fmt.Sprintf("out=%d", payload.OutputTokens))
	}
	if len(parts) == 0 {
		return nil
	}
	_, err := fmt.Fprintln(w, opts.style(ansiDim, "Usage ("+strings.Join(parts, ", ")+")"))
	return err
}

func renderError(w io.Writer, payload TimelinePayload, opts TranscriptOpts) error {
	msg := payload.Message
	if msg == "" {
		msg = "unknown error"
	}
	_, err := fmt.Fprintln(w, opts.style(ansiRed, "Error: "+msg))
	return err
}

func renderParseError(w io.Writer, payload TimelinePayload, opts TranscriptOpts) error {
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "unclassified"
	}
	line := "Parse diagnostic: " + reason
	if payload.HasParseErrorCount && payload.ParseErrorCount > 0 {
		line += fmt.Sprintf(" (count=%d)", payload.ParseErrorCount)
	}
	_, err := fmt.Fprintln(w, opts.style(ansiDim, line))
	return err
}

func renderUnknown(w io.Writer, payload TimelinePayload, opts TranscriptOpts) error {
	eventType := strings.TrimSpace(payload.RunnerEventType)
	reason := strings.TrimSpace(payload.Reason)
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

func renderEvent(w io.Writer, entryKind string, payload TimelinePayload, opts TranscriptOpts) error {
	kind := payload.EventKind
	if kind == "" {
		kind = entryKind
	}
	_, err := fmt.Fprintln(w, opts.style(ansiDim, "["+kind+"]"))
	return err
}

// Helper functions

func indentText(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
