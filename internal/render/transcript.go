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

// transcriptWriter wraps an io.Writer and captures the first error from any
// Println call; subsequent calls are no-ops. Callers drain the error via Err.
type transcriptWriter struct {
	w   io.Writer
	err error
}

func (x *transcriptWriter) Println(args ...any) {
	if x.err != nil {
		return
	}
	_, x.err = fmt.Fprintln(x.w, args...)
}

func (x *transcriptWriter) Err() error {
	return x.err
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
		_, err := fmt.Fprintln(w, opts.style(ansiDim, payload.UnrecognizedEventSummary(entry.Kind)))
		return err
	}
}

func renderSessionStart(w io.Writer, entry TranscriptEntry, payload timelinePayload, opts TranscriptOpts) error {
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

func renderPromptSeed(w io.Writer, payload timelinePayload, opts TranscriptOpts) error {
	tw := &transcriptWriter{w: w}
	tw.Println(opts.style(ansiDim, "── Prompt ──"))
	if text := payload.PromptLikeSummary(); text != "" {
		tw.Println(indentText(text, "  "))
	}
	return tw.Err()
}

func renderAssistantMessage(w io.Writer, payload timelinePayload, opts TranscriptOpts) error {
	tw := &transcriptWriter{w: w}
	tw.Println()
	tw.Println(opts.style(ansiBold, "Assistant"))

	if len(payload.blocks) > 0 {
		for _, block := range payload.blocks {
			switch block.Type {
			case "text":
				if text := block.Text; text != "" {
					tw.Println(text)
				}
			case "tool_use":
				name := block.Name
				if name == "" {
					name = "unknown"
				}
				tw.Println(opts.style(ansiCyan, "▶ Tool: "+name))
				if block.Input != nil {
					tw.Println(opts.style(ansiDim, "  (input hidden; use raw/json to inspect)"))
				}
			}
		}
		return tw.Err()
	}

	if text := payload.Text; text != "" {
		tw.Println(text)
	}
	return tw.Err()
}

func renderUserMessage(w io.Writer, payload timelinePayload, opts TranscriptOpts) error {
	tw := &transcriptWriter{w: w}
	if !payload.isToolResultMessage() {
		tw.Println()
		tw.Println(opts.style(ansiBold, "User"))
		if text := payload.promptMessageText(); text != "" {
			tw.Println(text)
		}
		return tw.Err()
	}

	if len(payload.blocks) > 0 {
		tw.Println(opts.style(ansiDim, "Tool Result"))
		for _, block := range payload.blocks {
			if content := block.Content; content != "" {
				tw.Println(indentText(content, "  "))
			}
			if text := block.Text; text != "" {
				tw.Println(indentText(text, "  "))
			}
		}
		return tw.Err()
	}

	if text := payload.Text; text != "" {
		tw.Println(opts.style(ansiDim, "Tool Result"))
		tw.Println(indentText(text, "  "))
	}
	return tw.Err()
}

func renderToolUse(w io.Writer, payload timelinePayload, opts TranscriptOpts) error {
	label := "▶"
	if payload.Name != "" {
		label += " " + payload.Name
	}
	if payload.Command != "" {
		label += " " + payload.Command
	}

	if payload.HasExitCode {
		exitStyle := ansiRed
		if payload.ExitCode == 0 {
			exitStyle = ansiGreen
		}
		label += " " + opts.style(exitStyle, fmt.Sprintf("(exit=%d)", payload.ExitCode))
	}

	_, err := fmt.Fprintln(w, label)
	return err
}

func renderFollowupPrompt(w io.Writer, payload timelinePayload, opts TranscriptOpts) error {
	tw := &transcriptWriter{w: w}
	tw.Println()
	tw.Println(opts.style(ansiBold, "User"))
	if text := payload.PromptLikeSummary(); text != "" {
		tw.Println(text)
	}
	return tw.Err()
}

func renderFinal(w io.Writer, payload timelinePayload, opts TranscriptOpts) error {
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

	if payload.usage.HasInputTokens {
		parts = append(parts, fmt.Sprintf("in=%d", payload.usage.InputTokens))
	}
	if payload.usage.HasOutputTokens {
		parts = append(parts, fmt.Sprintf("out=%d", payload.usage.OutputTokens))
	}

	line := "Done"
	if len(parts) > 0 {
		line += " (" + strings.Join(parts, ", ") + ")"
	}

	_, err := fmt.Fprintln(w, opts.style(ansiDim, line))
	return err
}

func renderUsage(w io.Writer, payload timelinePayload, opts TranscriptOpts) error {
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

func renderError(w io.Writer, payload timelinePayload, opts TranscriptOpts) error {
	msg := payload.Message
	if msg == "" {
		msg = "unknown error"
	}
	_, err := fmt.Fprintln(w, opts.style(ansiRed, "Error: "+msg))
	return err
}

func renderParseError(w io.Writer, payload timelinePayload, opts TranscriptOpts) error {
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

func renderUnknown(w io.Writer, payload timelinePayload, opts TranscriptOpts) error {
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

func renderEvent(w io.Writer, entryKind string, payload timelinePayload, opts TranscriptOpts) error {
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
