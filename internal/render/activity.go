package render

import (
	"fmt"
	"strings"
)

// NormalizeActivityKind maps surface-specific kind tokens to one shared
// human-readable activity language.
func NormalizeActivityKind(kind string) string {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch normalized {
	case "":
		return "activity"
	case "prompt-seed":
		return "prompt"
	case "followup":
		return "follow-up"
	}
	return normalized
}

// ActivitySummaryText returns summary text with a stable fallback per kind.
func ActivitySummaryText(kind, summary string) string {
	if trimmed := strings.TrimSpace(summary); trimmed != "" {
		return trimmed
	}
	switch NormalizeActivityKind(kind) {
	case "prompt":
		return "prompt"
	case "follow-up":
		return "follow-up prompt"
	case "assistant":
		return "assistant turn"
	default:
		return "activity"
	}
}

// FormatActivityLabel returns canonical "[kind] summary" presentation.
func FormatActivityLabel(kind, summary string) string {
	return fmt.Sprintf("[%s] %s", NormalizeActivityKind(kind), ActivitySummaryText(kind, summary))
}

// FormatTurnExtras returns concise turn metadata suffix, e.g.
// "(tools=2, checkpoint=4)".
func FormatTurnExtras(toolCount int, checkpointID int, restorable bool) string {
	parts := make([]string, 0, 2)
	if toolCount > 0 {
		parts = append(parts, fmt.Sprintf("tools=%d", toolCount))
	}
	if restorable && checkpointID > 0 {
		parts = append(parts, fmt.Sprintf("checkpoint=%d", checkpointID))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// FormatToolCallSummary renders tool calls in one stable style.
func FormatToolCallSummary(name, command string, hasExit bool, exitCode int) string {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		trimmedName = "tool"
	}
	parts := []string{"▶", trimmedName}
	if trimmedCommand := strings.TrimSpace(command); trimmedCommand != "" {
		parts = append(parts, trimmedCommand)
	}
	result := strings.Join(parts, " ")
	if hasExit {
		result += fmt.Sprintf(" (exit=%d)", exitCode)
	}
	return result
}
