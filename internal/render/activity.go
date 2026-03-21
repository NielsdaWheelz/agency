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

// FormatActivityWithExtras returns activity label with tool/checkpoint suffix.
func FormatActivityWithExtras(kind, summary string, toolCount int, checkpointID int, restorable bool) string {
	return FormatActivityLabel(kind, summary) + FormatTurnExtras(toolCount, checkpointID, restorable)
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

// FormatChangedPathSummary renders checkpoint changed-path previews consistently.
func FormatChangedPathSummary(paths []string, totalCount int, trimmed bool) string {
	if len(paths) == 0 {
		return ""
	}
	nonEmpty := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmedPath := strings.TrimSpace(path)
		if trimmedPath == "" {
			continue
		}
		nonEmpty = append(nonEmpty, trimmedPath)
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	joined := strings.Join(nonEmpty, ", ")
	if !trimmed {
		return joined
	}
	remaining := totalCount - len(nonEmpty)
	if remaining <= 0 {
		return joined
	}
	return fmt.Sprintf("%s, ... (+%d more)", joined, remaining)
}
